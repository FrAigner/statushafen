// Package selfmonitor überwacht Statushafen selbst und setzt das
// Ergebnis als Komponentenstatus auf der Systemseite.
//
// Ablauf je Takt: jede fällige Prüfung (Check) liefert ok/fehlgeschlagen/nicht
// prüfbar. Erst nach Threshold Fehlschlägen in Folge (Entprellung) wird die
// zugehörige Komponente auf den Ausfall-Status der Prüfung gesetzt; hält die
// Störung IncidentAfter an, legt der Monitor zusätzlich einen Vorfall an. Bei
// Erholung geht die Komponente zurück auf "operational" und der Vorfall wird
// geschlossen. Jeder Schritt steht im Audit-Log (Akteur "system").
//
// Der Monitor fasst eine Komponente nur an, wenn sie "operational" ist (z. B.
// kein Überschreiben einer Wartung oder eines manuell gesetzten Status) oder
// er sie selbst auf Störung gesetzt hat. Dieses "gehört dem Monitor" steht
// dauerhaft in SystemSetting (Schlüssel selfmonitor.failing.<slug>), damit ein
// Neustart mitten in einer Störung nichts verwaist zurücklässt.
package selfmonitor

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/statushistory"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const recoveredReason = "Die automatische Selbstüberwachung meldet: wieder erreichbar."

const (
	// DefaultInterval ist der Takt der Schleife.
	DefaultInterval = time.Minute
	// DefaultThreshold: Fehlschläge in Folge, bevor der Status kippt.
	DefaultThreshold = 3
	// DefaultIncidentAfter: so lange muss die Störung anhalten, bis ein Vorfall entsteht.
	DefaultIncidentAfter = 15 * time.Minute

	failingKeyPrefix  = "selfmonitor.failing."
	incidentKeyPrefix = "selfmonitor.incident."
	systemActor       = "system"
)

// Result ist das Ergebnis einer Prüfung. Skip bedeutet "nicht prüfbar" (z. B.
// Versand abgeschaltet, Messdatei fehlt): der Zustand bleibt unverändert.
type Result struct {
	OK   bool
	Skip bool
	// Off: die Prüfung ist abgeschaltet (z. B. Mailversand aus). Anders als Skip
	// gibt der Monitor eine von ihm gesetzte Störung frei: sonst
	// blieben Status, Marker und offener Vorfall ewig stehen.
	Off    bool
	Detail string
}

// Check ist eine Prüfung samt Komponente, die sie steuert.
type Check struct {
	// Slug der Komponente auf der Systemseite (database.CapabilitySlug).
	Slug string
	Name string
	// FailedStatus ist der Komponentenstatus bei Ausfall; zugleich der
	// Schweregrad des automatischen Vorfalls (degraded_performance|partial_outage|major_outage).
	FailedStatus string
	// Every: Prüfung nur in jedem n-ten Takt (0/1 = jedes Mal).
	Every int
	Run   func(ctx context.Context) Result
}

// Monitor führt die Prüfungen aus und schreibt die Ergebnisse.
type Monitor struct {
	DB     *gorm.DB
	Checks []Check
	// Enabled liefert den Schalter; ein Lesefehler ist NICHT "aus":
	// der Takt entfällt, nichts wird freigegeben. nil = immer an.
	Enabled       func() (bool, error)
	Threshold     int
	IncidentAfter time.Duration
	Interval      time.Duration
	Now           func() time.Time

	mu    sync.Mutex
	fails map[string]int
	tick  int
}

// New liefert einen Monitor mit den Standardwerten.
func New(db *gorm.DB, checks []Check, enabled func() (bool, error)) *Monitor {
	return &Monitor{
		DB: db, Checks: checks, Enabled: enabled,
		Threshold: DefaultThreshold, IncidentAfter: DefaultIncidentAfter,
		Interval: DefaultInterval, Now: time.Now,
		fails: map[string]int{},
	}
}

// Start startet die Schleife (erster Takt nach einem Intervall, damit der
// eigene HTTP-Server beim Start schon lauscht) und liefert die Stopp-Funktion.
func (m *Monitor) Start(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(m.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.RunOnce(ctx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// neutralizeEvery: jeder n-te Takt (Minuten) bereinigt Vorfalltexte ausgeblendeter
// Komponenten (Review).
const neutralizeEvery = 10

// RunOnce ist ein Takt.
func (m *Monitor) RunOnce(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tick++
	if m.tick%neutralizeEvery == 0 {
		// Review Wird eine Komponente erst nach regulärer
		// Erholung ausgeblendet/gelöscht/verschoben, gehört der Vorfall dem Monitor
		// nicht mehr und bliebe bis zum nächsten Start mit Namen öffentlich. Vor der
		// Schalterprüfung, weil die Texte öffentlich sind (wie der Backfill beim
		// Start); idempotent und eine Abfrage-Runde je 10 Takte. Fehler: geloggt,
		// nichts geschrieben, der Takt läuft weiter.
		if err := NeutralizeHiddenIncidents(m.DB); err != nil {
			slog.Error("Selbstüberwachung: Vorfalltexte nicht bereinigt", "error", err)
		}
	}
	if m.Enabled != nil {
		on, err := m.Enabled()
		if err != nil {
			// Wackelnde DB darf keine echte Störung freigeben.
			slog.Warn("Selbstüberwachung: Schalter nicht lesbar, Takt entfällt", "error", err)
			return
		}
		if !on {
			m.fails = map[string]int{} // Zähler nicht über eine Abschaltung hinweg mitschleppen
			m.releaseAll(offReason)
			return
		}
	}
	var page models.StatusPage
	if err := m.DB.Where("is_system = ?", true).First(&page).Error; err != nil {
		slog.Warn("Selbstüberwachung: keine Systemseite", "error", err)
		return
	}
	for _, c := range m.Checks {
		if c.Every > 1 && (m.tick-1)%c.Every != 0 {
			continue
		}
		m.apply(page.ID, c, c.Run(ctx))
	}
}

// offReason ist die Meldung im Vorfall, wenn der Monitor eine Störung wegen
// einer Abschaltung freigibt (statt wegen Erholung).
const offReason = "Automatische Verfolgung beendet."

// hiddenReason: Abschlusstext, wenn die Komponente archiviert/ausgeblendet wurde
// bewusst ohne Komponentennamen, der Vorfall ist öffentlich.
const hiddenReason = "Automatische Verfolgung beendet. Die Komponente wird nicht mehr angezeigt."

// releaseAll gibt alle vom Monitor gesetzten Störungen frei, wenn
// die Überwachung ausgeschaltet wurde. Ohne Marker kostet das nur eine Abfrage
// je Prüfung.
func (m *Monitor) releaseAll(reason string) {
	var page models.StatusPage
	if err := m.DB.Where("is_system = ?", true).First(&page).Error; err != nil {
		return
	}
	for _, c := range m.Checks {
		_, owned, err := m.getValue(failingKeyPrefix + c.Slug)
		if err != nil {
			logReadError(failingKeyPrefix+c.Slug, err)
			continue
		}
		if !owned {
			continue
		}
		var comp models.Component
		if err := m.DB.Where("status_page_id = ? AND slug = ?", page.ID, c.Slug).First(&comp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				m.releaseOrphan(page.ID, c) // Komponente gelöscht
			}
			continue
		}
		m.recover(c, comp, reason)
	}
}

func (m *Monitor) apply(pageID uint, c Check, res Result) {
	var comp models.Component
	if err := m.DB.Where("status_page_id = ? AND slug = ?", pageID, c.Slug).First(&comp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Komponente vom Admin gelöscht. Hatte der Monitor sie besetzt,
			// blieben Marker und offener Vorfall sonst ewig stehen.
			// Auch bei "nicht prüfbar" (Skip), sonst bleibt die Waise
			// stehen, solange die Prüfung nie wieder ein Ergebnis liefert.
			m.releaseOrphan(pageID, c)
		}
		return
	}
	hidden, ok := m.publiclyHidden(pageID, c, comp)
	if !ok {
		return
	}
	if hidden {
		// Öffentlich ausgeblendete Komponenten (archiviert, unsichtbar
		// oder Nachfahre davon, dieselbe Regel wie die Statusseite) werden nicht
		// ausgewertet: Ein öffentlicher Vorfall nennt den Prüfungsnamen (c.Name)
		// und gehört zu einer Komponente, die es öffentlich nicht gibt. Eine
		// laufende eigene Störung wird freigegeben (auch bei Skip); ohne Besitz
		// passiert nichts.
		m.fails[c.Slug] = 0
		m.recover(c, comp, hiddenReason)
		return
	}
	if res.Skip {
		return
	}
	if res.Off {
		m.fails[c.Slug] = 0
		m.recover(c, comp, offReason)
		return
	}
	if res.OK {
		m.fails[c.Slug] = 0
		m.recover(c, comp, recoveredReason)
		return
	}
	m.fails[c.Slug]++
	if m.fails[c.Slug] >= m.Threshold {
		m.fail(pageID, c, comp, res.Detail)
	}
}

// publiclyHidden wendet die öffentliche Regel (models.HiddenComponentIDs, inkl.
// Nachfahren) auf die Komponenten der Systemseite an. ok=false: Liste nicht
// lesbar, der Schritt entfällt (nächster Takt wiederholt ihn).
func (m *Monitor) publiclyHidden(pageID uint, c Check, comp models.Component) (hidden, ok bool) {
	var all []models.Component
	if err := m.DB.Where("status_page_id = ?", pageID).Find(&all).Error; err != nil {
		slog.Error("Selbstüberwachung: Komponentenliste nicht lesbar, Schritt entfällt", "slug", c.Slug, "error", err)
		return false, false
	}
	return models.HiddenComponentIDs(all)[comp.ID], true
}

func (m *Monitor) fail(pageID uint, c Check, comp models.Component, detail string) {
	now := m.Now()
	since, owned, err := m.getTime(failingKeyPrefix + c.Slug)
	if err != nil {
		logReadError(failingKeyPrefix+c.Slug, err) // Schritt entfällt, nächster Takt wiederholt ihn
		return
	}
	if !owned {
		if comp.Status != componentstatus.Operational {
			return // manuell gesetzt (Wartung, Admin): nicht überschreiben
		}
		// Status und Besitzmarke in EINER Transaktion. Scheitert die
		// Marke, bliebe die Komponente sonst ohne Besitzer auf Störung stehen.
		err = m.DB.Transaction(func(tx *gorm.DB) error {
			// Bedingtes Update: ein zwischenzeitlich vom Admin gesetzter Status
			// (z. B. Wartung) wird nicht überschrieben.
			// Status und Historieneintrag über statushistory (bedingt).
			changed, err := statushistory.Set(tx, comp.ID, componentstatus.Operational, c.FailedStatus, models.StatusSourceSelfMonitor, now)
			if err != nil {
				return err
			}
			if !changed {
				return errStaleStatus
			}
			return setValue(tx, failingKeyPrefix+c.Slug, now.UTC().Format(time.RFC3339))
		})
		if errors.Is(err, errStaleStatus) {
			return
		}
		if err != nil {
			slog.Error("Selbstüberwachung: Status nicht gesetzt", "slug", c.Slug, "error", err)
			return
		}
		m.audit("selfmonitor.component_failed", comp, c.FailedStatus+": "+detail)
		since = now
	}
	_, has, err := m.getValue(incidentKeyPrefix + c.Slug)
	if err != nil {
		// Ein Lesefehler ist NICHT "keine Marke", sonst entstünde ein
		// zweiter Vorfall und der erste bliebe öffentlich offen.
		logReadError(incidentKeyPrefix+c.Slug, err)
		return
	}
	if has || now.Sub(since) < m.IncidentAfter {
		return
	}
	m.openIncident(pageID, c, comp, since, detail)
}

// incidentTitle und incidentMessagePrefix bilden Titel und Meldung des
// automatischen Vorfalls. neutralizeIncident erkennt daran die
// unveränderten Texte des Monitors, deshalb an EINER Stelle.
func incidentTitle(c Check) string { return "Störung: " + c.Name }

func incidentMessagePrefix(c Check) string { return incidentMessageHead + c.Name + " (" }

// incidentMessageHead ist der feste Anfang der Meldung (der Backfill
// liest daraus den Prüfungsnamen zurück).
const incidentMessageHead = "Die automatische Selbstüberwachung meldet eine Störung bei "

func (m *Monitor) openIncident(pageID uint, c Check, comp models.Component, since time.Time, detail string) {
	msg := incidentMessagePrefix(c) + detail + ")."
	inc := models.Incident{
		StatusPageID: pageID, Title: incidentTitle(c), Message: msg,
		Status: "investigating", Severity: c.FailedStatus, StartedAt: &since,
	}
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		// Review Die Störungs-Marke in der Transaktion erneut lesen (Zeilensperre
		// in Postgres, SQLite ignoriert sie): hat eine andere Replika inzwischen
		// die Erholung gespeichert, entsteht kein Vorfall mehr, den nie jemand schließt.
		var own models.SystemSetting
		lerr := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where("key = ?", failingKeyPrefix+c.Slug).First(&own).Error
		if errors.Is(lerr, gorm.ErrRecordNotFound) {
			return errNotOwned
		}
		if lerr != nil {
			return lerr
		}
		if err := tx.Create(&inc).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.IncidentUpdate{IncidentID: inc.ID, Status: "investigating", Message: msg, AuthorEmail: systemActor}).Error; err != nil {
			return err
		}
		// Die Vorfall-Marke gehört in dieselbe Transaktion. Scheitert
		// sie, entstünde sonst bei jedem Takt ein neuer Vorfall.
		// Insert mit DO NOTHING statt Upsert: Existiert die Marke schon
		// (zweite Replika, übergangene Lesung), wird zurückgerollt, statt den
		// ersten Vorfall zu verwaisen.
		mark := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoNothing: true}).
			Create(&models.SystemSetting{Key: incidentKeyPrefix + c.Slug, Value: strconv.FormatUint(uint64(inc.ID), 10)})
		if mark.Error != nil {
			return mark.Error
		}
		if mark.RowsAffected == 0 {
			return errIncidentExists
		}
		return nil
	})
	if errors.Is(err, errNotOwned) {
		return
	}
	if errors.Is(err, errIncidentExists) {
		slog.Warn("Selbstüberwachung: Vorfall-Marke existiert bereits, kein zweiter Vorfall", "slug", c.Slug)
		return
	}
	if err != nil {
		slog.Error("Selbstüberwachung: Vorfall nicht angelegt", "slug", c.Slug, "error", err)
		return
	}
	m.audit("selfmonitor.incident_opened", comp, "Vorfall "+strconv.FormatUint(uint64(inc.ID), 10))
}

// errStaleStatus: der Status hat sich seit dem Lesen geändert (Admin).
var errStaleStatus = errors.New("komponentenstatus zwischenzeitlich geändert")

// errNotOwned: die Störungs-Marke fehlt inzwischen (Erholung/Freigabe gespeichert).
var errNotOwned = errors.New("störungs-marke fehlt")

// errIncidentExists: die Vorfall-Marke gibt es schon.
var errIncidentExists = errors.New("vorfall-marke existiert bereits")

// orphanReason ist die Meldung im Vorfall, wenn die Komponente gelöscht wurde.
const orphanReason = "Die überwachte Komponente ist nicht mehr vorhanden. Automatische Verfolgung beendet."

// releaseOrphan räumt auf, wenn die Komponente gelöscht wurde, während der
// Monitor sie besaß: Vorfall schließen, beide Marker löschen, alles
// in EINER Transaktion; Scheitern wird beim nächsten Takt wiederholt.
func (m *Monitor) releaseOrphan(pageID uint, c Check) {
	var incID string
	released, closed := false, false
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		_, owned, err := readValue(tx, failingKeyPrefix+c.Slug)
		if err != nil || !owned {
			return err
		}
		released = true
		raw, has, err := readValue(tx, incidentKeyPrefix+c.Slug)
		if err != nil {
			return err
		}
		if has {
			incID = raw
			var rerr error
			if closed, rerr = resolveIncident(tx, pageID, m.Now(), raw, orphanReason); rerr != nil {
				return rerr
			}
			// Auch der Vorfall einer gelöschten oder verschobenen
			// Komponente bleibt öffentlich in der Historie (wie).
			if err := neutralizeIncident(tx, pageID, raw, c); err != nil {
				return err
			}
			if err := tx.Unscoped().Where("key = ?", incidentKeyPrefix+c.Slug).Delete(&models.SystemSetting{}).Error; err != nil {
				return err
			}
		}
		return tx.Unscoped().Where("key = ?", failingKeyPrefix+c.Slug).Delete(&models.SystemSetting{}).Error
	})
	if err != nil {
		slog.Error("Selbstüberwachung: Aufräumen nicht gespeichert", "slug", c.Slug, "incident", incID, "error", err)
		return
	}
	if released {
		comp, where := m.identify(pageID, c.Slug, incID)
		details := "Komponente nicht mehr auf der Systemseite"
		switch where {
		case compDeleted:
			details = "Komponente gelöscht"
		case compMoved:
			details = "Komponente auf eine andere Seite verschoben"
		}
		if closed {
			details += "; Vorfall " + incID + " geschlossen"
		}
		if where == compMoved {
			// Die Komponente ist per ID bekannt und trägt noch den
			// Ausfallstatus, den der Monitor gesetzt hat: Das gehört ihm, er setzt
			// ihn bedingt zurück (wie recover). Jeder andere Status bleibt.
			changed, serr := statushistory.Set(m.DB, comp.ID, c.FailedStatus, componentstatus.Operational, models.StatusSourceSelfMonitor, m.Now())
			if serr == nil && changed {
				details += "; Status zurückgesetzt"
			} else {
				details += "; Status unverändert"
			}
		}
		m.audit("selfmonitor.component_released", comp, details)
	}
}

const (
	compUnknown = iota // Identität nicht bekannt
	compDeleted        // auf der Systemseite gelöscht
	compMoved          // auf eine andere Seite verschoben
)

// identify bestimmt, was aus der Komponente wurde. Exakt ist nur die
// ID aus dem markierten Vorfall (IncidentComponent); nach dem Slug allein wird
// auf fremden Seiten NICHT geraten, dort kann zufällig eine fremde Komponente
// denselben Slug tragen. Ohne Vorfall bleibt nur die (soft) gelöschte Komponente
// der Systemseite; sonst ist die Identität unbekannt (Slug dient als Name).
func (m *Monitor) identify(pageID uint, slug, incID string) (models.Component, int) {
	if id, err := strconv.ParseUint(incID, 10, 64); err == nil {
		var link models.IncidentComponent
		var comp models.Component
		if m.DB.Where("incident_id = ?", id).Order("id").First(&link).Error == nil &&
			m.DB.Unscoped().First(&comp, link.ComponentID).Error == nil {
			if comp.StatusPageID != pageID {
				return comp, compMoved
			}
			return comp, compDeleted
		}
	}
	comp := models.Component{Name: slug}
	// Order id DESC: bei mehreren gelöschten Komponenten mit gleichem Slug die jüngste.
	if m.DB.Unscoped().Where("status_page_id = ? AND slug = ?", pageID, slug).Order("id DESC").First(&comp).Error == nil {
		return comp, compDeleted
	}
	return models.Component{Name: slug}, compUnknown
}

// logReadError meldet einen Lesefehler auf einer Marke (der Fehlertext wird vom
// Logger bereinigt, siehe Logging-Regeln).
func logReadError(key string, err error) {
	slog.Error("Selbstüberwachung: Marke nicht lesbar, Schritt entfällt", "key", key, "error", err)
}

// recover setzt Status zurück, schließt den Vorfall und löscht beide Marker in
// EINER Transaktion. Sonst blieben Status "operational" und Marke
// "failing" widersprüchlich stehen. Scheitert etwas, wird beim nächsten Takt
// alles erneut versucht; die Audit-Meldung gibt es erst nach dem Commit.
func (m *Monitor) recover(c Check, comp models.Component, reason string) {
	_, owned, rerr := m.getValue(failingKeyPrefix + c.Slug)
	if rerr != nil {
		logReadError(failingKeyPrefix+c.Slug, rerr)
		return
	}
	if !owned {
		return
	}
	var incID string
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		// Re-Review Dieselbe Sperre wie in openIncident zuerst (gleiche
		// Reihenfolge, kein Deadlock): Eine zweite Replika liest die Vorfall-Marke
		// erst nach dem Commit dieser Erholung.
		if _, _, lerr := readValueLocked(tx, failingKeyPrefix+c.Slug); lerr != nil {
			return lerr
		}
		if comp.Status == c.FailedStatus { // hat der Admin inzwischen etwas anderes gesetzt, bleibt es
			if _, err := statushistory.Set(tx, comp.ID, c.FailedStatus, componentstatus.Operational, models.StatusSourceSelfMonitor, m.Now()); err != nil {
				return err
			}
		}
		var inc models.SystemSetting
		ierr := tx.Unscoped().Where("key = ?", incidentKeyPrefix+c.Slug).First(&inc).Error
		if ierr != nil && !errors.Is(ierr, gorm.ErrRecordNotFound) {
			return ierr
		}
		if ierr == nil {
			incID = inc.Value
			if _, err := resolveIncident(tx, comp.StatusPageID, m.Now(), inc.Value, reason); err != nil {
				return err
			}
			if reason == hiddenReason {
				// Der Vorfall bleibt in der öffentlichen Historie, nur die
				// Zuordnung zur Komponente blendet die Seite aus. Titel und Meldung
				// nennen sonst weiter den Prüfungsnamen.
				if err := neutralizeIncident(tx, comp.StatusPageID, inc.Value, c); err != nil {
					return err
				}
			}
			if err := tx.Unscoped().Where("key = ?", incidentKeyPrefix+c.Slug).Delete(&models.SystemSetting{}).Error; err != nil {
				return err
			}
		}
		return tx.Unscoped().Where("key = ?", failingKeyPrefix+c.Slug).Delete(&models.SystemSetting{}).Error
	})
	if err != nil {
		slog.Error("Selbstüberwachung: Erholung nicht gespeichert", "slug", c.Slug, "incident", incID, "error", err)
		return
	}
	details := ""
	if reason == offReason {
		details = "Überwachung abgeschaltet" // von echter Erholung unterscheidbar
	}
	if reason == hiddenReason {
		details = "Komponente archiviert oder ausgeblendet"
	}
	m.audit("selfmonitor.component_recovered", comp, details)
}

// resolveIncident schließt den Vorfall. Ein DB-Fehler wird zurückgegeben (der
// Aufrufer behält dann die Vorfall-Marke); "0 Zeilen" (Admin schloss schon oder
// löschte) ist Erfolg.
// nur Vorfälle der Systemseite (pageID). Der Rückgabewert sagt, ob
// dieser Aufruf den Vorfall wirklich geschlossen hat.
func resolveIncident(tx *gorm.DB, pageID uint, now time.Time, rawID, reason string) (bool, error) {
	// Bitgröße 0 = Größe von uint: Werte, die nicht in uint passen, werden hier
	// abgelehnt, statt bei uint(id) unten abgeschnitten zu werden (32-Bit).
	id, err := strconv.ParseUint(rawID, 10, 0)
	if err != nil {
		return false, nil //nolint:nilerr // ungültige Kennung: nichts zu schließen, Marke wird entfernt
	}
	res := tx.Model(&models.Incident{}).Where("id = ? AND status_page_id = ? AND status <> ?", id, pageID, "resolved").
		Updates(map[string]any{"status": "resolved", "resolved_at": now})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil // schon vom Admin geschlossen oder gelöscht
	}
	if err := tx.Create(&models.IncidentUpdate{IncidentID: uint(id), Status: "resolved", AuthorEmail: systemActor,
		Message: reason}).Error; err != nil {
		return false, err // in Postgres würde der Fehler die Transaktion ohnehin abbrechen
	}
	return true, nil
}

// neutrale Texte für Vorfälle einer ausgeblendeten Komponente,
// ohne Prüfungsnamen und ohne Fehlerdetail.
const (
	neutralTitle   = "Störung: automatische Überwachung"
	neutralMessage = "Die automatische Selbstüberwachung meldete eine Störung."
)

// neutralizeIncident ersetzt Titel und Meldungen des automatischen Vorfalls
// durch neutrale Texte. Nur, was der Monitor geschrieben hat und
// unverändert ist: Als Originalmeldung gilt die des Eröffnungs-Updates (erstes
// Update des Akteurs "system" mit dem Präfix des Monitors); verglichen wird
// exakt, ein vom Admin ergänzter Text bleibt also stehen (Review), ebenso
// ein geänderter Titel und Updates des Admins. Die Bedingung steht im UPDATE
// selbst: Ein Admin-Edit zwischen Lesen und Schreiben wird nicht überschrieben.
// Übersetzungen ersetzter Texte werden gelöscht, sie nennen den Namen ebenfalls
// und passen nicht mehr zum Text. Fehler brechen die Transaktion ab, der
// nächste Takt wiederholt die Freigabe.
func neutralizeIncident(tx *gorm.DB, pageID uint, rawID string, c Check) error {
	id, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil {
		return nil //nolint:nilerr // ungültige Kennung: nichts zu neutralisieren (wie resolveIncident)
	}
	var inc models.Incident
	if err := tx.Where("id = ? AND status_page_id = ?", id, pageID).First(&inc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // vom Admin gelöscht
		}
		return err
	}
	var updates []models.IncidentUpdate
	if err := tx.Where("incident_id = ? AND author_email = ?", inc.ID, systemActor).Order("id").Find(&updates).Error; err != nil {
		return err
	}
	original := ""
	for _, u := range updates {
		if strings.HasPrefix(u.Message, incidentMessagePrefix(c)) {
			original = u.Message
			break
		}
	}
	titled, err := replaceIf(tx, &models.Incident{}, inc.ID, "title", incidentTitle(c), neutralTitle)
	if err != nil {
		return err
	}
	messaged, err := replaceIf(tx, &models.Incident{}, inc.ID, "message", original, neutralMessage)
	if err != nil {
		return err
	}
	if titled || messaged {
		if err := tx.Where("incident_id = ?", inc.ID).Delete(&models.IncidentTranslation{}).Error; err != nil {
			return err
		}
	}
	for _, u := range updates {
		replaced, err := replaceIf(tx, &models.IncidentUpdate{}, u.ID, "message", original, neutralMessage)
		if err != nil {
			return err
		}
		if !replaced {
			continue
		}
		if err := tx.Where("update_id = ?", u.ID).Delete(&models.IncidentUpdateTranslation{}).Error; err != nil {
			return err
		}
	}
	return nil
}

// replaceIf setzt col auf to, aber nur, solange dort noch genau want steht
// (Review Bedingung im UPDATE, kein Überschreiben eines
// zwischenzeitlichen Admin-Edits). Ein leeres want ersetzt nie etwas.
func replaceIf(tx *gorm.DB, model any, id uint, col, want, to string) (bool, error) {
	res := tx.Model(model).Where("id = ? AND "+col+" = ? AND "+col+" <> ''", id, want).Update(col, to)
	return res.RowsAffected > 0, res.Error
}

func (m *Monitor) audit(action string, comp models.Component, details string) {
	entry := models.AuditLog{Action: action, Category: "system", TargetType: "Component",
		TargetID: strconv.FormatUint(uint64(comp.ID), 10), TargetName: comp.Name, Details: details, ActorEmail: systemActor}
	if err := m.DB.Create(&entry).Error; err != nil {
		slog.Error("Selbstüberwachung: Audit-Eintrag fehlgeschlagen", "action", action, "error", err)
	}
}

// getValue liefert den Wert einer Marke. Ein Lesefehler wird zurückgegeben und
// ist NICHT "keine Marke".
func (m *Monitor) getValue(key string) (string, bool, error) { return readValue(m.DB, key) }

// readValue liest auch soft-gelöschte Zeilen (Review): ON CONFLICT (key)
// greift ebenfalls auf sie, "keine Marke" wäre sonst ein stiller Dauer-Rollback.
func readValue(db *gorm.DB, key string) (string, bool, error) { return readValueWith(db, key, false) }

// readValueLocked liest mit Zeilensperre (FOR UPDATE in Postgres, SQLite ignoriert sie).
func readValueLocked(tx *gorm.DB, key string) (string, bool, error) {
	return readValueWith(tx, key, true)
}

func readValueWith(db *gorm.DB, key string, lock bool) (string, bool, error) {
	var s models.SystemSetting
	q := db.Unscoped()
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Where("key = ?", key).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return s.Value, true, nil
}

func (m *Monitor) getTime(key string) (time.Time, bool, error) {
	v, ok, err := m.getValue(key)
	if err != nil || !ok {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		// Beschädigter Wert gilt als "gerade begonnen" UND wird neu
		// geschrieben. Sonst lieferte jeder Takt wieder "jetzt" und IncidentAfter
		// würde nie erreicht.
		now := m.Now()
		if werr := setValue(m.DB, key, now.UTC().Format(time.RFC3339)); werr != nil {
			slog.Error("Selbstüberwachung: Zeitwert nicht repariert", "key", key, "error", werr)
		}
		return now, true, nil
	}
	return t, true, nil
}

func setValue(db *gorm.DB, key, value string) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at", "deleted_at"}),
	}).Create(&models.SystemSetting{Key: key, Value: value}).Error
}

// NeutralizeHiddenIncidents ist der Backfill: Monitor-Vorfälle der
// Systemseite, die schon GESCHLOSSEN sind (vor dem Fix beim Ausblenden
// oder nach regulärer Erholung) und deren Komponente inzwischen öffentlich
// ausgeblendet, gelöscht oder verschoben ist, behalten sonst Prüfungsnamen und
// Fehlerdetail in der öffentlichen Historie.
//
// Design-Entscheidungen:
//   - Erkannt wird der Vorfall am Eröffnungs-Update des Akteurs "system" mit dem
//     festen Meldungsanfang; der Prüfungsname wird daraus gelesen. So braucht der
//     Backfill keine Liste der Prüfungen und läuft auch, wenn eine Prüfung
//     inzwischen entfällt. Nach dem Ersetzen passt das Update nicht mehr: ein
//     zweiter Lauf findet nichts (idempotent), ohne Marker in SystemSetting.
//   - Ersetzt wird über neutralizeIncident: nur Unverändertes vom Monitor, Texte
//     und Updates des Admins bleiben.
//   - Nur Vorfälle der Systemseite (Mandantentrennung), nur geschlossene: offene
//     gehören dem Monitor, der sie beim Freigeben selbst neutralisiert.
//   - Ohne auffindbare Zuordnung oder Komponente gilt der Vorfall als verwaist und
//     wird neutralisiert (konservativ: neutraler Text schadet nie).
//   - Alles in EINER Transaktion; jeder DB-Fehler rollt zurück und wird
//     zurückgegeben (fail closed, der nächste Start wiederholt den Lauf).
func NeutralizeHiddenIncidents(db *gorm.DB) error {
	var page models.StatusPage
	if err := db.Where("is_system = ?", true).First(&page).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // keine Systemseite, nichts zu tun
		}
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var active []models.Component
		if err := tx.Where("status_page_id = ?", page.ID).Find(&active).Error; err != nil {
			return err
		}
		hidden := models.HiddenComponentIDs(active)
		var openings []models.IncidentUpdate
		if err := tx.Joins("JOIN incidents ON incidents.id = incident_updates.incident_id AND incidents.deleted_at IS NULL").
			Where("incidents.status_page_id = ? AND incidents.status = ?", page.ID, "resolved").
			Where("incident_updates.author_email = ? AND incident_updates.message LIKE ?", systemActor, incidentMessageHead+"%").
			Order("incident_updates.id").Find(&openings).Error; err != nil {
			return err
		}
		seen := map[uint]bool{}
		for _, u := range openings {
			name, _, found := strings.Cut(strings.TrimPrefix(u.Message, incidentMessageHead), " (")
			if seen[u.IncidentID] || !found || name == "" {
				continue
			}
			seen[u.IncidentID] = true
			gone, err := componentGone(tx, page.ID, u.IncidentID, hidden)
			if err != nil {
				return err
			}
			if !gone {
				continue
			}
			if err := neutralizeIncident(tx, page.ID, strconv.FormatUint(uint64(u.IncidentID), 10), Check{Name: name}); err != nil {
				return err
			}
		}
		return nil
	})
}

// componentGone sagt, ob die Komponente des Vorfalls öffentlich nicht (mehr)
// erscheint: ausgeblendet (hidden, Regel der Statusseite), gelöscht, auf eine
// andere Seite verschoben oder gar nicht mehr auffindbar. Die Zuordnung wird
// Unscoped gelesen, damit eine entfernte Zuordnung nicht wie "sichtbar" wirkt.
func componentGone(tx *gorm.DB, pageID, incidentID uint, hidden map[uint]bool) (bool, error) {
	var link models.IncidentComponent
	if err := tx.Unscoped().Where("incident_id = ?", incidentID).Order("id").First(&link).Error; err != nil {
		return errors.Is(err, gorm.ErrRecordNotFound), onlyReadError(err)
	}
	var comp models.Component
	if err := tx.Unscoped().First(&comp, link.ComponentID).Error; err != nil {
		return errors.Is(err, gorm.ErrRecordNotFound), onlyReadError(err)
	}
	return comp.StatusPageID != pageID || comp.DeletedAt.Valid || hidden[comp.ID], nil
}

// onlyReadError gibt err zurück, außer "nicht gefunden" (das ist hier ein Ergebnis).
func onlyReadError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}
