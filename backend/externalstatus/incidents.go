package externalstatus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// Teil B2: Vorfälle und Wartungen der Fremdseite
// spiegeln und Abonnenten benachrichtigen.
//
// Ablauf je Abruf: Nach den Komponenten holt der Dienst beide Feeds
// (Vorfälle, Wartungen), gleicht sie in einer Transaktion mit den
// gespeicherten Meldungen ab und meldet danach die Ereignisse an den
// IncidentNotifier.
//
// Stiller Erstimport: Solange der Feed einer Quelle noch nie erfolgreich
// abgerufen wurde (IncidentsSyncedAt/MaintenancesSyncedAt = nil), löst nichts
// eine Benachrichtigung aus - die Historie der Fremdseite wäre sonst eine
// Mailflut. Ebenso bleibt still, was beim ersten Auftauchen schon beendet ist.
//
// Kein Doppelversand: Jede Meldung trägt einen Fingerabdruck ihres
// verarbeiteten Stands (NotifyKey = Status + jüngstes Update). Ein Ereignis
// entsteht nur, wenn der Abruf einen anderen Stand sieht UND das bedingte
// UPDATE "setze NotifyKey auf neu, wo er noch alt ist" genau eine Zeile trifft.
// Ein zweiter Abruf (oder eine zweite Instanz) sieht dann denselben Stand und
// schweigt. Neue Meldungen beansprucht der Insert selbst (partieller
// Unique-Index). Im ungünstigsten Fall - Absturz zwischen Beanspruchen und
// Versand - fällt eine Mail aus, statt doppelt zu kommen (wie beim
// Ausfall-Melder in outage.go).

// IncidentEventKind unterscheidet die Anlässe.
type IncidentEventKind string

// Anlässe einer Benachrichtigung.
const (
	IncidentCreated  IncidentEventKind = "created"
	IncidentUpdated  IncidentEventKind = "updated"
	IncidentResolved IncidentEventKind = "resolved"
)

// OwnComponent ist eine eigene Komponente, die über eine Kopplung von einer
// Meldung betroffen ist.
type OwnComponent struct {
	ID   uint
	Name string
}

// IncidentEvent beschreibt eine neue oder geänderte Meldung, die eine
// gekoppelte Komponente betrifft. Notify=false: nur gespiegelt (stiller
// Erstimport, bereits beendet, inaktive Quelle) - der Empfänger schreibt dann
// höchstens einen Audit-Eintrag.
type IncidentEvent struct {
	Kind   IncidentEventKind
	Notify bool
	Source models.ExternalSource
	// Incident mit Components und Updates (Updates: neueste zuerst).
	Incident models.ExternalIncident
	// Components: betroffene eigene Komponenten, nach Name sortiert.
	Components []OwnComponent
}

// IncidentNotifier verschickt die Benachrichtigungen. Der Aufruf kommt
// synchron aus dem Abfrage-Dienst; langsame Arbeit gehört in eine eigene
// Goroutine des Empfängers.
type IncidentNotifier interface {
	ExternalIncident(ctx context.Context, ev IncidentEvent)
}

// SetIncidentNotifier setzt den Empfänger der Vorfall-Ereignisse (der Handler
// entsteht nach dem Dienst, deshalb nachträglich).
func (s *Service) SetIncidentNotifier(n IncidentNotifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.incidents = n
}

var feedKinds = []string{models.ExternalIncidentKindIncident, models.ExternalIncidentKindMaintenance}

// syncIncidents spiegelt beide Feeds einer Quelle. Best effort: Ein Fehler
// wird in IncidentsError festgehalten, der Abruf der Komponenten bleibt
// unberührt, der nächste Takt versucht es erneut.
func (s *Service) syncIncidents(ctx context.Context, srcID uint, now time.Time) {
	var src models.ExternalSource
	if err := s.db.First(&src, srcID).Error; err != nil {
		return
	}
	p, ok := s.providers[src.Provider].(IncidentProvider)
	if !ok {
		return
	}
	var msgs []string
	var events []IncidentEvent
	for _, kind := range feedKinds {
		evs, err := s.syncFeed(ctx, p, src, kind, now)
		if err != nil {
			slog.Warn("Externe Statusseite: Vorfälle nicht abrufbar", "source_id", src.ID, "kind", kind, "error", err)
			msgs = append(msgs, kind+": "+truncate(err.Error(), maxErrorLen/2))
			continue
		}
		events = append(events, evs...)
	}
	errText := truncate(strings.Join(msgs, "; "), maxErrorLen)
	if errText != src.IncidentsError {
		if err := s.db.Model(&models.ExternalSource{}).Where("id = ?", src.ID).Update("incidents_error", errText).Error; err != nil {
			slog.Error("Externe Statusseite: Vorfallfehler nicht gespeichert", "source_id", src.ID, "error", err)
		}
	}
	s.mu.Lock()
	n := s.incidents
	s.mu.Unlock()
	if n == nil {
		return
	}
	for _, ev := range events {
		n.ExternalIncident(ctx, ev)
	}
}

// syncFeed ruft einen Feed ab und übernimmt ihn samt Validatoren und
// Erst-Import-Marke in einer Transaktion.
func (s *Service) syncFeed(ctx context.Context, p IncidentProvider, src models.ExternalSource, kind string, now time.Time) ([]IncidentEvent, error) {
	req := IncidentRequest{BaseURL: src.BaseURL, Kind: kind, ETag: src.IncidentsETag, LastModified: src.IncidentsLastMod}
	synced := src.IncidentsSyncedAt
	etagCol, modCol, syncCol := "IncidentsETag", "IncidentsLastMod", "IncidentsSyncedAt"
	if kind == models.ExternalIncidentKindMaintenance {
		req.ETag, req.LastModified = src.MaintenancesETag, src.MaintenancesLastMod
		synced = src.MaintenancesSyncedAt
		etagCol, modCol, syncCol = "MaintenancesETag", "MaintenancesLastMod", "MaintenancesSyncedAt"
	}
	feed, err := p.FetchIncidents(ctx, req)
	if err != nil {
		return nil, err
	}
	if feed.NotModified {
		return nil, nil
	}
	silent := synced == nil
	items := make([]Incident, 0, len(feed.Items))
	for _, it := range feed.Items {
		if !s.pastRetention(it, now) {
			items = append(items, it)
		}
	}
	var events []IncidentEvent
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		if events, txErr = applyFeed(tx, src, items, silent, now); txErr != nil {
			return txErr
		}
		upd := map[string]any{etagCol: feed.ETag, modCol: feed.LastModified}
		if silent {
			upd[syncCol] = now
		}
		return tx.Model(&models.ExternalSource{}).Where("id = ?", src.ID).Updates(upd).Error
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

// fingerprint: Stand einer Meldung für die Doppelversand-Sperre - Status und
// Prüfsumme der Update-IDs. Ein neues Update ändert ihn auch dann, wenn die
// Fremdseite es rückdatiert (display_at) oder der Status gleich bleibt.
func fingerprint(it Incident) string {
	ids := make([]string, 0, len(it.Updates))
	for _, u := range it.Updates {
		ids = append(ids, u.ExternalID)
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
	return it.Status + "|" + hex.EncodeToString(sum[:8])
}

// applyFeed gleicht die Meldungen einer Quelle mit items ab und liefert die
// Ereignisse für Meldungen, die gekoppelte Komponenten betreffen. Idempotent:
// Derselbe Feed zweimal ergibt beim zweiten Mal weder Änderungen noch Ereignisse.
func applyFeed(tx *gorm.DB, src models.ExternalSource, items []Incident, silent bool, now time.Time) ([]IncidentEvent, error) {
	if len(items) == 0 {
		return nil, nil
	}
	kind := items[0].Kind
	var existing []models.ExternalIncident
	if err := tx.Where("external_source_id = ? AND kind = ?", src.ID, kind).Find(&existing).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]*models.ExternalIncident, len(existing))
	for i := range existing {
		byID[existing[i].ExternalID] = &existing[i]
	}
	own, err := OwnComponentMap(tx, src.StatusPageID, src.ID)
	if err != nil {
		return nil, err
	}
	var events []IncidentEvent
	for _, it := range items {
		ev, err := applyIncident(tx, src, it, byID[it.ExternalID], own[src.ID], silent, now)
		if err != nil {
			return nil, err
		}
		if ev != nil {
			events = append(events, *ev)
		}
	}
	return events, nil
}

// applyIncident übernimmt eine Meldung. row = bisheriger Stand (nil = neu).
func applyIncident(tx *gorm.DB, src models.ExternalSource, it Incident, row *models.ExternalIncident, own map[string][]OwnComponent, silent bool, now time.Time) (*IncidentEvent, error) {
	fp := fingerprint(it)
	isNew := row == nil
	wasClosed, claimed := false, false
	if isNew {
		row = &models.ExternalIncident{StatusPageID: src.StatusPageID, ExternalSourceID: src.ID, ExternalID: it.ExternalID, NotifyKey: fp}
	} else {
		wasClosed = row.Closed
		if row.NotifyKey == fp && sameTime(row.ExternalUpdatedAt, it.UpdatedAt) {
			return nil, nil // nichts Neues
		}
		if row.NotifyKey != fp {
			// Bedingtes UPDATE: nur wer den alten Stand ablöst, darf melden.
			res := tx.Model(&models.ExternalIncident{}).Where("id = ? AND notify_key = ?", row.ID, row.NotifyKey).Update("notify_key", fp)
			if res.Error != nil {
				return nil, res.Error
			}
			if res.RowsAffected == 0 {
				return nil, nil
			}
			row.NotifyKey = fp
			claimed = true
		}
	}
	fillIncident(row, it, now)
	if err := tx.Save(row).Error; err != nil {
		return nil, err
	}
	if err := syncIncidentUpdates(tx, row.ID, it.Updates); err != nil {
		return nil, err
	}
	if err := syncIncidentComponents(tx, row.ID, it.Components); err != nil {
		return nil, err
	}
	if !isNew && !claimed {
		return nil, nil // nur Stammdaten geändert (Titel, Zeiten), kein neuer Stand
	}
	return buildEvent(tx, src, row, it, own, isNew, wasClosed, silent)
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Equal(*b)
}

func fillIncident(row *models.ExternalIncident, it Incident, now time.Time) {
	row.Kind = it.Kind
	row.Title = it.Title
	row.Status = it.Status
	row.RawStatus = it.RawStatus
	row.Impact = it.Impact
	row.Closed = it.Closed
	row.URL = it.URL
	row.StartedAt = it.StartedAt
	row.ExternalUpdatedAt = it.UpdatedAt
	row.ResolvedAt = it.ResolvedAt
	row.ScheduledFor = it.ScheduledAt
	row.ScheduledUntil = it.ScheduledTo
	row.LastSeenAt = now
}

// buildEvent entscheidet, ob und wie gemeldet wird. Meldungen ohne gekoppelte
// Komponente erzeugen kein Ereignis.
func buildEvent(tx *gorm.DB, src models.ExternalSource, row *models.ExternalIncident, it Incident, own map[string][]OwnComponent, isNew, wasClosed, silent bool) (*IncidentEvent, error) {
	comps := affectedOwn(own, it)
	if len(comps) == 0 {
		return nil, nil
	}
	kind := IncidentUpdated
	switch {
	case it.Closed:
		kind = IncidentResolved
	case isNew:
		kind = IncidentCreated
	}
	notify := !silent && src.Active
	if it.Closed && (isNew || wasClosed) {
		notify = false // beim ersten Auftauchen schon beendet bzw. Nachtrag zu einer beendeten Meldung
	}
	var full models.ExternalIncident
	if err := tx.Preload("Components").Preload("Updates", func(db *gorm.DB) *gorm.DB {
		return db.Order("display_at desc, id desc")
	}).First(&full, row.ID).Error; err != nil {
		return nil, err
	}
	return &IncidentEvent{Kind: kind, Notify: notify, Source: src, Incident: full, Components: comps}, nil
}

// affectedOwn liefert die eigenen Komponenten, die von der Meldung betroffen
// sind (Vereinigung über alle genannten externen Komponenten).
func affectedOwn(own map[string][]OwnComponent, it Incident) []OwnComponent {
	seen := map[uint]bool{}
	var out []OwnComponent
	for _, c := range it.Components {
		for _, o := range own[c.ExternalID] {
			if !seen[o.ID] {
				seen[o.ID] = true
				out = append(out, o)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func syncIncidentUpdates(tx *gorm.DB, incidentID uint, updates []IncidentUpdate) error {
	var existing []models.ExternalIncidentUpdate
	if err := tx.Where("external_incident_id = ?", incidentID).Find(&existing).Error; err != nil {
		return err
	}
	byID := make(map[string]*models.ExternalIncidentUpdate, len(existing))
	for i := range existing {
		byID[existing[i].ExternalID] = &existing[i]
	}
	for _, u := range updates {
		row, ok := byID[u.ExternalID]
		if !ok {
			row = &models.ExternalIncidentUpdate{ExternalIncidentID: incidentID, ExternalID: u.ExternalID}
		} else if row.Status == u.Status && row.Body == u.Body && row.DisplayAt.Equal(u.DisplayAt) {
			continue
		}
		row.Status, row.Body, row.DisplayAt = u.Status, u.Body, u.DisplayAt
		if err := tx.Save(row).Error; err != nil {
			return err
		}
	}
	return nil
}

// syncIncidentComponents gleicht die betroffenen Komponenten ab: Neue kommen
// dazu, nicht mehr genannte werden (soft) entfernt.
func syncIncidentComponents(tx *gorm.DB, incidentID uint, comps []IncidentComponent) error {
	var existing []models.ExternalIncidentComponent
	if err := tx.Where("external_incident_id = ?", incidentID).Find(&existing).Error; err != nil {
		return err
	}
	want := make(map[string]IncidentComponent, len(comps))
	for _, c := range comps {
		want[c.ExternalID] = c
	}
	have := map[string]bool{}
	for i := range existing {
		row := &existing[i]
		c, ok := want[row.ComponentExternalID]
		if !ok {
			if err := tx.Delete(row).Error; err != nil {
				return err
			}
			continue
		}
		have[row.ComponentExternalID] = true
		if row.Name != c.Name {
			row.Name = c.Name
			if err := tx.Save(row).Error; err != nil {
				return err
			}
		}
	}
	for _, c := range comps {
		if have[c.ExternalID] {
			continue
		}
		if err := tx.Create(&models.ExternalIncidentComponent{ExternalIncidentID: incidentID, ComponentExternalID: c.ExternalID, Name: c.Name}).Error; err != nil {
			return err
		}
	}
	return nil
}

type ownRow struct {
	ComponentID uint
	Name        string
	SourceID    uint
	ExternalID  string
	IsGroup     bool
}

// OwnComponentMap liefert je Quelle der Statusseite (sourceID 0 = alle
// aktiven, sonst genau diese) und externer Komponenten-ID die gekoppelten
// eigenen Komponenten. Eine Kopplung auf eine externe Gruppe gilt auch für
// deren Mitglieder (dynamisch über group_external_id, wie in LinkedRefs).
// Mandantentrennung: Kopplung und Quelle müssen auf pageID zeigen.
func OwnComponentMap(db *gorm.DB, pageID, sourceID uint) (map[uint]map[string][]OwnComponent, error) {
	q := db.Table("external_component_links AS l").
		Select("l.component_id AS component_id, c.name AS name, ec.external_source_id AS source_id, ec.external_id AS external_id, ec.is_group AS is_group").
		Joins("JOIN external_components ec ON ec.id = l.external_component_id AND ec.deleted_at IS NULL").
		Joins("JOIN external_sources es ON es.id = ec.external_source_id AND es.deleted_at IS NULL").
		Joins("JOIN components c ON c.id = l.component_id AND c.deleted_at IS NULL").
		Where("l.deleted_at IS NULL AND l.status_page_id = ? AND es.status_page_id = ? AND c.status_page_id = ?", pageID, pageID, pageID)
	if sourceID != 0 {
		q = q.Where("es.id = ?", sourceID)
	} else {
		q = q.Where("es.active = ?", true)
	}
	var rows []ownRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[uint]map[string][]OwnComponent{}
	add := func(src uint, extID string, o OwnComponent) {
		if out[src] == nil {
			out[src] = map[string][]OwnComponent{}
		}
		for _, have := range out[src][extID] {
			if have.ID == o.ID {
				return
			}
		}
		out[src][extID] = append(out[src][extID], o)
	}
	groups := map[groupKey][]OwnComponent{}
	var sourceIDs []uint
	seenSrc := map[uint]bool{}
	for _, r := range rows {
		o := OwnComponent{ID: r.ComponentID, Name: r.Name}
		add(r.SourceID, r.ExternalID, o)
		if r.IsGroup {
			k := groupKey{r.SourceID, r.ExternalID}
			groups[k] = append(groups[k], o)
			if !seenSrc[r.SourceID] {
				seenSrc[r.SourceID] = true
				sourceIDs = append(sourceIDs, r.SourceID)
			}
		}
	}
	if len(groups) == 0 {
		return out, nil
	}
	var members []struct {
		ExternalSourceID uint
		ExternalID       string
		GroupExternalID  string
	}
	if err := db.Table("external_components").Select("external_source_id, external_id, group_external_id").
		Where("deleted_at IS NULL AND is_group = ? AND group_external_id <> '' AND external_source_id IN ?", false, sourceIDs).
		Scan(&members).Error; err != nil {
		return nil, err
	}
	for _, m := range members {
		for _, o := range groups[groupKey{m.ExternalSourceID, m.GroupExternalID}] {
			add(m.ExternalSourceID, m.ExternalID, o)
		}
	}
	return out, nil
}
