// Package statushistory schreibt die Statushistorie der Komponenten
// (Tabelle component_status_changes).
//
// Jeder Schreibpfad von Component.Status geht durch dieses Paket: Set für reine
// Statusänderungen (auch bedingte, "WHERE status = …"), Lock/Record für Pfade,
// die in derselben Transaktion weitere Spalten speichern. Ein Eintrag entsteht
// genau dann, wenn sich der Status wirklich ändert, in derselben Transaktion
// wie die Änderung (kein Eintrag ohne Änderung, keine Änderung ohne Eintrag).
package statushistory

import (
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Retention ist die Aufbewahrungsfrist. Die Verfügbarkeit zeigt 90 Tage; ein
// Jahr lässt Platz für spätere längere Auswertungen und kostet kaum Speicher
// (eine Zeile je Statuswechsel). Betreiber-Entscheidung offen.
const Retention = 365 * 24 * time.Hour

// MergeGap ist die Länge eines Verdichtungsfensters für Webhook-Wechsel derselben
// Komponente. Das Fenster beginnt mit einem ANKER (Quelle webhook) und
// dauert MergeGap ab dessen Zeit. Alle weiteren Wechsel darin werden zu höchstens ZWEI
// Zeilen verdichtet: Der Anker trägt den SCHLIMMSTEN Status des Fensters (nie ein
// Ausfall geht verloren), ein NACHLAUF (Quelle webhook_nachlauf) trägt den Endstand,
// falls der vom schlimmsten abweicht. Es wird nie gelöscht, was einen Ausfall
// verbergen könnte; nur der Nachlauf entfällt, wenn der Endstand der schlimmste ist.
// 30 s sind länger als jedes sinnvolle Prüfintervall eines Monitorings (typisch
// >= 60 s), also fasst es nur Flattern zusammen und begrenzt die Tabelle auf höchstens
// 5.760 Zeilen je Komponente und Tag (statt 20/min je IP). Der Statuswechsel selbst ist
// davon unberührt, nur die Historie wird verdichtet.
//
// Abweichung der Verfügbarkeit: PESSIMISTISCH. Das ganze Fenster (höchstens MergeGap) zählt
// als schlimmster Status, auch wenn der Dienst zwischendurch lief; nie als besser.
// Wechsel mit normalem Abstand sind nicht betroffen. (Früher löschte der Rückwechsel
// A→B→A die Zeile ganz: wiederholt ergab das 0 Zeilen für einen echten Ausfall, Review B1.)
const MergeGap = 30 * time.Second

const groupStatus = "group"

// Set setzt den Status einer Komponente und schreibt den Eintrag, in einer
// Transaktion. from != "" macht die Änderung bedingt (nur wenn der Status noch
// from ist, wie bei der Selbstüberwachung). Gruppen werden nie angefasst. Gibt
// changed=false zurück, wenn nichts zu tun war (gleicher Status, Bedingung
// nicht erfüllt, Komponente fehlt/Gruppe).
func Set(db *gorm.DB, componentID uint, from, to, source string, at time.Time) (bool, error) {
	changed := false
	err := db.Transaction(func(tx *gorm.DB) error {
		cur, err := Lock(tx, componentID)
		if err != nil || cur == "" || cur == groupStatus || cur == to || (from != "" && cur != from) {
			return err
		}
		res := tx.Model(&models.Component{}).Where("id = ? AND status = ?", componentID, cur).Update("status", to)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		changed = true
		return Record(tx, componentID, cur, to, source, at)
	})
	return changed && err == nil, err
}

// Lock liest den aktuellen Status der (nicht gelöschten) Komponente und sperrt
// die Zeile bis zum Ende der Transaktion (Postgres: FOR UPDATE; SQLite
// ignoriert die Klausel). Fehlt die Komponente, ist der Status leer.
func Lock(tx *gorm.DB, componentID uint) (string, error) {
	var rows []models.Component
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").
		Where("id = ?", componentID).Limit(1).Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return "", err
	}
	return rows[0].Status, nil
}

// Record schreibt einen Eintrag, wenn sich der Status ändert (old != new).
// Gruppen haben keine Historie. Der Aufrufer ruft es in der Transaktion auf,
// die den Status schreibt.
func Record(tx *gorm.DB, componentID uint, oldStatus, newStatus, source string, at time.Time) error {
	if oldStatus == newStatus || newStatus == groupStatus || oldStatus == groupStatus {
		return nil
	}
	if source == models.StatusSourceWebhook {
		merged, err := mergeWebhook(tx, componentID, oldStatus, newStatus, at.UTC())
		if err != nil || merged {
			return err
		}
	}
	return tx.Create(&models.ComponentStatusChange{
		ComponentID: componentID, OldStatus: oldStatus, NewStatus: newStatus,
		Source: source, ChangedAt: at.UTC(),
	}).Error
}

// mergeWebhook verdichtet (siehe MergeGap): Liegt ein Fenster vor (neueste Zeile ist
// ein Webhook-Anker oder ein Nachlauf mit seinem Anker, und der Wechsel kommt weniger als MergeGap
// nach dem Anker), wird der Anker auf den schlimmeren Status gesetzt und der Nachlauf auf den
// Endstand gebracht (angelegt, überschrieben oder entfernt, wenn der Endstand dem Anker entspricht).
// Der Aufrufer hält die Zeilensperre (Lock). Gibt false zurück, wenn kein Fenster vorliegt
// (der Aufrufer legt dann einen neuen Anker an).
func mergeWebhook(tx *gorm.DB, componentID uint, oldStatus, newStatus string, at time.Time) (bool, error) {
	var last []models.ComponentStatusChange
	if err := tx.Where("component_id = ?", componentID).Order("changed_at DESC, id DESC").Limit(2).Find(&last).Error; err != nil {
		return false, err
	}
	if len(last) == 0 {
		return false, nil
	}
	var anchor models.ComponentStatusChange
	var tail *models.ComponentStatusChange
	switch last[0].Source {
	case models.StatusSourceWebhook:
		anchor = last[0]
	case models.StatusSourceWebhookNachlauf:
		// Ein Nachlauf gehört immer zu dem Anker davor; fehlt der, ist der Zustand unklar: kein Merge.
		if len(last) < 2 || last[1].Source != models.StatusSourceWebhook || last[1].NewStatus != last[0].OldStatus {
			return false, nil
		}
		anchor = last[1]
		tail = &last[0]
	default:
		return false, nil
	}
	aktuell := anchor.NewStatus
	if tail != nil {
		aktuell = tail.NewStatus
	}
	d := at.Sub(anchor.ChangedAt)
	if aktuell != oldStatus || d < 0 || d >= MergeGap {
		return false, nil
	}
	schlimmster := componentstatus.Worst(anchor.NewStatus, newStatus)
	if schlimmster != anchor.NewStatus {
		if err := tx.Model(&models.ComponentStatusChange{}).Where("id = ?", anchor.ID).Update("new_status", schlimmster).Error; err != nil {
			return true, err
		}
	}
	switch {
	case newStatus == schlimmster:
		// Endstand ist der schlimmste: ein Nachlauf wird nicht gebraucht.
		if tail != nil {
			return true, tx.Delete(&models.ComponentStatusChange{}, tail.ID).Error
		}
		return true, nil
	case tail != nil:
		return true, tx.Model(&models.ComponentStatusChange{}).Where("id = ?", tail.ID).
			Updates(map[string]any{"old_status": schlimmster, "new_status": newStatus, "changed_at": at}).Error
	default:
		return true, tx.Create(&models.ComponentStatusChange{
			ComponentID: componentID, OldStatus: schlimmster, NewStatus: newStatus,
			Source: models.StatusSourceWebhookNachlauf, ChangedAt: at,
		}).Error
	}
}

// Backfill trägt für jede Komponente ohne Historie ihren aktuellen Status ein
// Der Zeitpunkt ist der des Nachtrags, NICHT die Anlage: Wann ein
// Bestandsstatus wirklich begann, ist unbekannt, und ein Rückdatieren würde
// einen heutigen Ausfall über Wochen als Ausfallzeit rechnen. Idempotent
// (NOT EXISTS), Gruppen und gelöschte Komponenten bleiben außen vor.
func Backfill(db *gorm.DB, now time.Time) error {
	return db.Exec(`INSERT INTO component_status_changes (component_id, old_status, new_status, source, changed_at, created_at)
SELECT c.id, '', c.status, ?, ?, ? FROM components c
WHERE c.deleted_at IS NULL AND c.status <> ? AND NOT EXISTS (SELECT 1 FROM component_status_changes h WHERE h.component_id = c.id)`,
		models.StatusSourceBackfill, now.UTC(), now.UTC(), groupStatus).Error
}

// Prune löscht Einträge älter als die Aufbewahrungsfrist, behält aber je
// Komponente den jeweils neuesten davon: Er ist der Ausgangsstatus am Anfang
// des Auswertungsfensters. Liefert die Zahl gelöschter Zeilen.
func Prune(db *gorm.DB, now time.Time) (int64, error) {
	cutoff := now.UTC().Add(-Retention)
	// Behalten wird je Komponente der neueste alte Eintrag nach (changed_at, id).
	res := db.Exec(`DELETE FROM component_status_changes WHERE changed_at < ? AND EXISTS (
SELECT 1 FROM component_status_changes n WHERE n.component_id = component_status_changes.component_id AND n.changed_at < ?
AND (n.changed_at > component_status_changes.changed_at OR (n.changed_at = component_status_changes.changed_at AND n.id > component_status_changes.id)))`, cutoff, cutoff)
	return res.RowsAffected, res.Error
}
