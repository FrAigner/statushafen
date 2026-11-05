package externalstatus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Spiegelung von Vorfällen und Wartungen, stiller Erstimport,
// Idempotenz und Mandantentrennung auf Dienst-Ebene.

// incProvider ergänzt den Fake-Provider um die Feeds.
type incProvider struct {
	*fakeProvider
	mu    sync.Mutex
	feeds map[string]*IncidentFeed
	errs  map[string]error
	reqs  []IncidentRequest
}

func (p *incProvider) FetchIncidents(_ context.Context, req IncidentRequest) (*IncidentFeed, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = append(p.reqs, req)
	if err := p.errs[req.Kind]; err != nil {
		return nil, err
	}
	if f := p.feeds[req.Kind]; f != nil {
		return f, nil
	}
	return &IncidentFeed{}, nil
}

func (p *incProvider) set(kind string, items ...Incident) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.feeds[kind] = &IncidentFeed{ETag: `"` + kind + `"`, Items: items}
}

type incRecorder struct {
	mu     sync.Mutex
	events []IncidentEvent
	// Aufräum-Meldungen (RetentionReporter).
	pruned    []int
	retention time.Duration
}

func (r *incRecorder) ExternalIncident(_ context.Context, ev IncidentEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *incRecorder) take() []IncidentEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

func (r *incRecorder) notified() []IncidentEvent {
	var out []IncidentEvent
	for _, e := range r.take() {
		if e.Notify {
			out = append(out, e)
		}
	}
	return out
}

type incFixture struct {
	*serviceFixture
	prov *incProvider
	rec  *incRecorder
}

// newIncFixture: Dienst mit Vorfall-Provider und Recorder; "vm" ist an die
// eigene Komponente gekoppelt.
func newIncFixture(t *testing.T) *incFixture {
	t.Helper()
	sf := newServiceFixture(t, okSummary("e1", comp("vm", "Service 03", "operational")))
	prov := &incProvider{fakeProvider: sf.provider, feeds: map[string]*IncidentFeed{}, errs: map[string]error{}}
	rec := &incRecorder{}
	sf.svc = NewService(sf.db, Options{
		Providers: map[string]Provider{ProviderSummaryV2: prov},
		Listener:  sf.listener,
		Locker:    sf.locker,
		Now:       func() time.Time { return sf.now },
		Jitter:    func(max time.Duration) time.Duration { return max },
		Incidents: rec,
	})
	sf.link(t, sf.page.ID, sf.comp.ID, sf.ext.ID)
	return &incFixture{serviceFixture: sf, prov: prov, rec: rec}
}

func (f *incFixture) poll() FetchResult {
	f.now = f.now.Add(5 * time.Minute)
	return f.svc.FetchNow(context.Background(), f.source2())
}

func (f *incFixture) source2() models.ExternalSource {
	var src models.ExternalSource
	if err := f.db.First(&src, f.src.ID).Error; err != nil {
		panic(err)
	}
	return src
}

func inc(id, status string, updates ...string) Incident {
	st, closed := NormalizeIncidentStatus(models.ExternalIncidentKindIncident, status)
	it := Incident{ExternalID: id, Kind: models.ExternalIncidentKindIncident, Title: "Titel " + id, Status: st, RawStatus: status, Impact: "major", Closed: closed,
		URL: "https://example.com/incidents/" + id, Components: []IncidentComponent{{ExternalID: "vm", Name: "Service 03"}}}
	at := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	t0 := at
	it.StartedAt, it.UpdatedAt = &t0, &at
	for i, u := range updates {
		it.Updates = append(it.Updates, IncidentUpdate{ExternalID: id + "-u" + string(rune('0'+i)), Status: st, Body: u, DisplayAt: at.Add(time.Duration(i) * time.Minute)})
	}
	if len(updates) > 0 {
		upd := at.Add(time.Duration(len(updates)) * time.Minute)
		it.UpdatedAt = &upd
	}
	return it
}

func maint(id, status string) Incident {
	st, closed := NormalizeIncidentStatus(models.ExternalIncidentKindMaintenance, status)
	it := inc(id, "investigating", "geplant")
	it.Kind, it.Status, it.RawStatus, it.Closed, it.Impact = models.ExternalIncidentKindMaintenance, st, status, closed, "maintenance"
	return it
}

func (f *incFixture) rows(t *testing.T) []models.ExternalIncident {
	t.Helper()
	var rows []models.ExternalIncident
	require.NoError(t, f.db.Preload("Updates").Preload("Components").Order("id").Find(&rows).Error)
	return rows
}

func TestIncidents_ErstimportIstStill(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "Wir prüfen"), inc("i2", "resolved", "Behoben"))
	f.prov.set(models.ExternalIncidentKindMaintenance, maint("m1", "scheduled"))

	res := f.poll()
	require.True(t, res.OK, res.Error)

	assert.Empty(t, f.rec.notified(), "erster Abruf: keine einzige Benachrichtigung, auch nicht für offene Vorfälle")
	rows := f.rows(t)
	require.Len(t, rows, 3, "gespiegelt wird trotzdem")
	src := f.source2()
	assert.NotNil(t, src.IncidentsSyncedAt)
	assert.NotNil(t, src.MaintenancesSyncedAt)
	assert.Empty(t, src.IncidentsError)
	assert.Equal(t, `"incident"`, src.IncidentsETag)

	// Danach: neues Update und neuer Vorfall melden.
	f.prov.set(models.ExternalIncidentKindIncident,
		inc("i1", "identified", "Wir prüfen", "Ursache gefunden"), inc("i2", "resolved", "Behoben"), inc("i3", "investigating", "Neu"))
	f.prov.set(models.ExternalIncidentKindMaintenance, maint("m1", "scheduled"))
	f.poll()
	got := f.rec.notified()
	require.Len(t, got, 2)
	assert.Equal(t, IncidentUpdated, got[0].Kind)
	assert.Equal(t, "i1", got[0].Incident.ExternalID)
	assert.Equal(t, "identified", got[0].Incident.Status)
	require.Len(t, got[0].Components, 1)
	assert.Equal(t, f.comp.ID, got[0].Components[0].ID)
	assert.Equal(t, "Ursache gefunden", got[0].Incident.Updates[0].Body, "neuestes Update zuerst")
	assert.Equal(t, IncidentCreated, got[1].Kind)
	assert.Equal(t, "i3", got[1].Incident.ExternalID)
	assert.Equal(t, "Example Cloud", got[1].Source.Name)
}

func TestIncidents_ZweimalPollenKeineZweiteMeldung(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident)
	f.poll() // Erstimport (leer)

	f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "Wir prüfen"))
	f.poll()
	require.Len(t, f.rec.notified(), 1)
	before := f.rows(t)

	for range 3 {
		f.poll()
	}
	assert.Empty(t, f.rec.take(), "derselbe Stand erzeugt kein Ereignis mehr")
	after := f.rows(t)
	require.Len(t, after, 1)
	assert.Equal(t, before[0].UpdatedAt, after[0].UpdatedAt, "und ändert nichts")
	assert.Len(t, after[0].Updates, 1)
	assert.Len(t, after[0].Components, 1)
}

func TestIncidents_BedingtesUpdateGegenParallelenAbruf(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "a"))
	f.poll()
	f.rec.take()

	// Eine andere Instanz hat den neuen Stand schon beansprucht: unser (veralteter)
	// Abruf darf ihn nicht noch einmal melden.
	stale := f.rows(t)[0]
	newer := inc("i1", "monitoring", "a", "b")
	require.NoError(t, f.db.Model(&models.ExternalIncident{}).Where("id = ?", stale.ID).Update("notify_key", fingerprint(newer)).Error)

	ev, err := applyIncident(f.db, f.source2(), newer, &stale, map[string][]OwnComponent{"vm": {{ID: f.comp.ID, Name: "API"}}}, false, f.now)
	require.NoError(t, err)
	assert.Nil(t, ev, "0 Zeilen vom bedingten UPDATE: kein Ereignis")
}

func TestIncidents_NurGekoppelteKomponentenZaehlen(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident)
	f.poll()

	other := inc("i1", "investigating", "x")
	other.Components = []IncidentComponent{{ExternalID: "ungekoppelt", Name: "Anderes"}}
	none := inc("i2", "investigating", "x")
	none.Components = nil
	f.prov.set(models.ExternalIncidentKindIncident, other, none, inc("i3", "investigating", "x"))
	f.poll()

	got := f.rec.take()
	require.Len(t, got, 1, "nur der Vorfall mit gekoppelter Komponente")
	assert.Equal(t, "i3", got[0].Incident.ExternalID)
	assert.Len(t, f.rows(t), 3, "die übrigen werden trotzdem gespiegelt")

	// Wird später gekoppelt, meldet erst das nächste Update.
	other2 := inc("i1", "identified", "x", "y")
	other2.Components = other.Components
	f.prov.set(models.ExternalIncidentKindIncident, other2)
	f.poll()
	assert.Empty(t, f.rec.take())
}

func TestIncidents_GruppenKopplungGiltFuerMitglieder(t *testing.T) {
	f := newIncFixture(t)
	// Gruppe "g1" mit Mitglied "vm2"; die eigene Komponente koppelt die Gruppe.
	grp := models.ExternalComponent{ExternalSourceID: f.src.ID, ExternalID: "g1", Name: "Gruppe", IsGroup: true, Status: componentstatus.Operational, LastSeenAt: f.now}
	member := models.ExternalComponent{ExternalSourceID: f.src.ID, ExternalID: "vm2", Name: "VM2", GroupExternalID: "g1", Status: componentstatus.Operational, LastSeenAt: f.now}
	require.NoError(t, f.db.Create(&grp).Error)
	require.NoError(t, f.db.Create(&member).Error)
	ownB := models.Component{StatusPageID: f.page.ID, Name: "Backend", Status: componentstatus.Operational}
	require.NoError(t, f.db.Create(&ownB).Error)
	f.link(t, f.page.ID, ownB.ID, grp.ID)

	f.prov.set(models.ExternalIncidentKindIncident)
	f.poll()
	it := inc("i1", "investigating", "x")
	it.Components = []IncidentComponent{{ExternalID: "vm2", Name: "VM2"}}
	f.prov.set(models.ExternalIncidentKindIncident, it)
	f.poll()

	got := f.rec.take()
	require.Len(t, got, 1)
	require.Len(t, got[0].Components, 1)
	assert.Equal(t, "Backend", got[0].Components[0].Name)
}

func TestIncidents_BereitsBeendetBeimAuftauchenIstStill(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident)
	f.poll()

	f.prov.set(models.ExternalIncidentKindIncident, inc("alt", "resolved", "Behoben"))
	f.poll()
	assert.Empty(t, f.rec.notified(), "Vorfall, der schon beendet auftaucht, löst nichts aus")
	assert.Len(t, f.rows(t), 1)

	// Nachträgliches Update einer beendeten Meldung (z. B. Postmortem) ebenfalls nicht.
	f.prov.set(models.ExternalIncidentKindIncident, inc("alt", "resolved", "Behoben", "Postmortem"))
	f.poll()
	assert.Empty(t, f.rec.notified())
}

func TestIncidents_VerlaufMeldetErstellungUpdateUndBehebung(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident)
	f.poll()

	steps := []struct {
		it   Incident
		want IncidentEventKind
	}{
		{inc("i1", "investigating", "a"), IncidentCreated},
		{inc("i1", "identified", "a", "b"), IncidentUpdated},
		{inc("i1", "identified", "a", "b", "c"), IncidentUpdated}, // gleicher Status, neues Update
		{inc("i1", "resolved", "a", "b", "c", "d"), IncidentResolved},
		{inc("i1", "investigating", "a", "b", "c", "d", "e"), IncidentUpdated}, // wieder geöffnet
	}
	for _, s := range steps {
		f.prov.set(models.ExternalIncidentKindIncident, s.it)
		f.poll()
		got := f.rec.notified()
		require.Len(t, got, 1, string(s.want))
		assert.Equal(t, s.want, got[0].Kind)
	}
}

func TestIncidents_RueckdatiertesUpdateWirdGemeldet(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident)
	f.poll()
	f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "a", "b"))
	f.poll()
	f.rec.take()

	// Neues Update mit älterem display_at, gleicher Status.
	it := inc("i1", "investigating", "a", "b")
	it.Updates = append(it.Updates, IncidentUpdate{ExternalID: "i1-uX", Status: "investigating", Body: "nachgetragen", DisplayAt: it.Updates[0].DisplayAt.Add(-time.Hour)})
	f.prov.set(models.ExternalIncidentKindIncident, it)
	f.poll()
	assert.Len(t, f.rec.notified(), 1)
}

func TestIncidents_WartungenUndEigenerMarker(t *testing.T) {
	f := newIncFixture(t)
	// Nur die Vorfälle sind beim ersten Abruf lesbar; die Wartungen scheitern.
	f.prov.errs[models.ExternalIncidentKindMaintenance] = errors.New("HTTP 500")
	f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "a"))
	res := f.poll()
	require.True(t, res.OK, "ein Feed-Fehler lässt den Abruf der Komponenten gelingen")
	src := f.source2()
	assert.NotNil(t, src.IncidentsSyncedAt)
	assert.Nil(t, src.MaintenancesSyncedAt)
	assert.Contains(t, src.IncidentsError, "maintenance: HTTP 500")
	assert.True(t, *src.LastFetchOK)

	// Die Wartungen kommen später: ihr Erstimport ist still, auch wenn die Vorfälle schon laufen.
	delete(f.prov.errs, models.ExternalIncidentKindMaintenance)
	f.prov.set(models.ExternalIncidentKindMaintenance, maint("m1", "in_progress"))
	f.poll()
	assert.Empty(t, f.rec.notified())
	src = f.source2()
	assert.NotNil(t, src.MaintenancesSyncedAt)
	assert.Empty(t, src.IncidentsError, "Fehlertext wird nach Erfolg gelöscht")

	// Ab jetzt melden Wartungen: Beginn, Ende.
	f.prov.set(models.ExternalIncidentKindMaintenance, maint("m1", "in_progress"), maint("m2", "scheduled"))
	f.poll()
	got := f.rec.notified()
	require.Len(t, got, 1)
	assert.Equal(t, models.ExternalIncidentKindMaintenance, got[0].Incident.Kind)
	assert.Equal(t, IncidentCreated, got[0].Kind)
	done := maint("m1", "completed")
	done.Updates = append(done.Updates, IncidentUpdate{ExternalID: "m1-end", Status: "completed", Body: "fertig", DisplayAt: time.Now()})
	f.prov.set(models.ExternalIncidentKindMaintenance, done)
	f.poll()
	got = f.rec.notified()
	require.Len(t, got, 1)
	assert.Equal(t, IncidentResolved, got[0].Kind)
}

func TestIncidents_FehlerVorErstimportBleibtStill(t *testing.T) {
	f := newIncFixture(t)
	f.prov.errs[models.ExternalIncidentKindIncident] = errors.New("kaputt")
	f.prov.errs[models.ExternalIncidentKindMaintenance] = errors.New("kaputt")
	f.poll()
	assert.Nil(t, f.source2().IncidentsSyncedAt, "ohne Erfolg keine Marke")

	delete(f.prov.errs, models.ExternalIncidentKindIncident)
	f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "a"))
	f.poll()
	assert.Empty(t, f.rec.notified(), "der erste erfolgreiche Abruf ist der stille Erstimport")
}

func TestIncidents_Validatoren(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident)
	f.poll()
	f.poll()
	f.prov.mu.Lock()
	defer f.prov.mu.Unlock()
	var last IncidentRequest
	for _, r := range f.prov.reqs {
		if r.Kind == models.ExternalIncidentKindIncident {
			last = r
		}
	}
	assert.Equal(t, `"incident"`, last.ETag, "der zweite Abruf schickt den gespeicherten ETag")
}

func TestIncidents_NotModifiedAendertNichts(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "a"))
	f.poll()
	f.rec.take()
	f.prov.mu.Lock()
	f.prov.feeds[models.ExternalIncidentKindIncident] = &IncidentFeed{NotModified: true}
	f.prov.mu.Unlock()
	f.poll()
	assert.Empty(t, f.rec.take())
	assert.Equal(t, `"incident"`, f.source2().IncidentsETag, "Validator bleibt erhalten")
	assert.Len(t, f.rows(t), 1)
}

func TestIncidents_InaktiveQuelleSpiegeltOhneMeldung(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident)
	f.poll()
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("active", false).Error)
	f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "a"))
	f.poll() // "Jetzt abrufen" geht auch bei inaktiven Quellen
	got := f.rec.take()
	require.Len(t, got, 1)
	assert.False(t, got[0].Notify)
	assert.Len(t, f.rows(t), 1)
}

func TestIncidents_UpdatesUndKomponentenWerdenAbgeglichen(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "alt"))
	f.poll()

	it := inc("i1", "investigating", "korrigiert")
	it.Updates[0].ExternalID = "i1-u0"
	it.Components = []IncidentComponent{{ExternalID: "vm", Name: "VM umbenannt"}, {ExternalID: "x", Name: "Neu"}}
	upd := time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)
	it.UpdatedAt = &upd
	f.prov.set(models.ExternalIncidentKindIncident, it)
	f.poll()

	rows := f.rows(t)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].Updates, 1)
	assert.Equal(t, "korrigiert", rows[0].Updates[0].Body, "geänderter Text überschreibt")
	require.Len(t, rows[0].Components, 2)

	it.Components = []IncidentComponent{{ExternalID: "x", Name: "Neu"}}
	upd = upd.Add(time.Minute)
	it.UpdatedAt = &upd
	f.prov.set(models.ExternalIncidentKindIncident, it)
	f.poll()
	rows = f.rows(t)
	require.Len(t, rows[0].Components, 1, "nicht mehr genannte Komponenten entfallen")
	assert.Equal(t, "x", rows[0].Components[0].ComponentExternalID)
}

func TestIncidents_ProviderOhneVorfallUnterstuetzung(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1", comp("vm", "VM", "operational")))
	res := f.svc.FetchNow(context.Background(), f.source(t))
	require.True(t, res.OK)
	var n int64
	f.db.Model(&models.ExternalIncident{}).Count(&n)
	assert.Zero(t, n)
	assert.Empty(t, f.source(t).IncidentsError)
}

func TestOwnComponentMap_Mandantentrennung(t *testing.T) {
	f := newIncFixture(t)
	// Fremde Seite koppelt ihre eigene "vm" - darf in unserer Auswertung nicht auftauchen.
	f.link(t, f.otherPage.ID, f.otherComp.ID, f.otherExt.ID)
	// Gelöschte eigene Komponente zählt nicht.
	gone := models.Component{StatusPageID: f.page.ID, Name: "Weg", Status: componentstatus.Operational}
	require.NoError(t, f.db.Create(&gone).Error)
	f.link(t, f.page.ID, gone.ID, f.ext.ID)
	require.NoError(t, f.db.Delete(&gone).Error)

	m, err := OwnComponentMap(f.db, f.page.ID, 0)
	require.NoError(t, err)
	require.Len(t, m, 1)
	require.Len(t, m[f.src.ID]["vm"], 1)
	assert.Equal(t, f.comp.ID, m[f.src.ID]["vm"][0].ID)

	other, err := OwnComponentMap(f.db, f.otherPage.ID, f.otherSrc.ID) // otherSrc ist im Fixture inaktiv
	require.NoError(t, err)
	assert.Len(t, other[f.otherSrc.ID]["vm"], 1)
	assert.Empty(t, other[f.src.ID])

	// Inaktive Quellen entfallen bei sourceID 0, mit expliziter ID nicht.
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("active", false).Error)
	m, _ = OwnComponentMap(f.db, f.page.ID, 0)
	assert.Empty(t, m)
	m, _ = OwnComponentMap(f.db, f.page.ID, f.src.ID)
	assert.Len(t, m[f.src.ID]["vm"], 1)
	// Seite und Quelle passen nicht zusammen: nichts.
	m, _ = OwnComponentMap(f.db, f.otherPage.ID, f.src.ID)
	assert.Empty(t, m)
}

func TestFingerprint(t *testing.T) {
	a := inc("i", "investigating", "x", "y")
	b := inc("i", "investigating", "x", "y")
	assert.Equal(t, fingerprint(a), fingerprint(b))
	b.Updates[0], b.Updates[1] = b.Updates[1], b.Updates[0]
	assert.Equal(t, fingerprint(a), fingerprint(b), "Reihenfolge der Updates ist egal")
	c := inc("i", "monitoring", "x", "y")
	assert.NotEqual(t, fingerprint(a), fingerprint(c), "Status")
	d := inc("i", "investigating", "x", "y", "z")
	assert.NotEqual(t, fingerprint(a), fingerprint(d), "neues Update")
}

func TestSetIncidentNotifier(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1"))
	rec := &incRecorder{}
	f.svc.SetIncidentNotifier(rec)
	f.svc.mu.Lock()
	assert.Same(t, rec, f.svc.incidents)
	f.svc.mu.Unlock()
}

func TestAffectedOwn_SortierungUndDoppelte(t *testing.T) {
	own := map[string][]OwnComponent{
		"a": {{ID: 7, Name: "Zeta"}, {ID: 3, Name: "Alpha"}},
		"b": {{ID: 2, Name: "Alpha"}, {ID: 7, Name: "Zeta"}},
	}
	it := Incident{Components: []IncidentComponent{{ExternalID: "a"}, {ExternalID: "b"}, {ExternalID: "ohne"}}}
	got := affectedOwn(own, it)
	require.Len(t, got, 3, "Komponente 7 nur einmal")
	assert.Equal(t, []uint{2, 3, 7}, []uint{got[0].ID, got[1].ID, got[2].ID}, "nach Name, bei gleichem Namen nach ID")
}

func TestOwnComponentMap_GruppeUndMitgliedGleichzeitigGekoppelt(t *testing.T) {
	f, _ := prepareChangeState(t)
	// "Backend" koppelt die Gruppe g1 und zusätzlich direkt deren Mitglied: ein Eintrag, nicht zwei.
	member := models.ExternalComponent{ExternalSourceID: f.src.ID, ExternalID: "m1", Name: "Mitglied", GroupExternalID: "g1", Status: componentstatus.Operational, LastSeenAt: f.now}
	require.NoError(t, f.db.Create(&member).Error)
	var backend models.Component
	require.NoError(t, f.db.Where("name = ?", "Backend").First(&backend).Error)
	f.link(t, f.page.ID, backend.ID, member.ID)

	m, err := OwnComponentMap(f.db, f.page.ID, f.src.ID)
	require.NoError(t, err)
	require.Len(t, m[f.src.ID]["m1"], 1)
	assert.Equal(t, backend.ID, m[f.src.ID]["m1"][0].ID)
}
