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

// Ausfall-Erkennung einer externen Quelle.

type recordingNotifier struct {
	mu     sync.Mutex
	events []OutageEvent
}

func (r *recordingNotifier) SourceOutage(_ context.Context, ev OutageEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recordingNotifier) kinds() []OutageKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []OutageKind{}
	for _, e := range r.events {
		out = append(out, e.Kind)
	}
	return out
}

var boom = fakeResponse{err: errors.New("HTTP 503 von https://example.com/api/v2/summary.json")}

// outageFixture: Quelle mit gekoppelter Komponente (externer Status
// major_outage), letzter Erfolg vor 50 Minuten, Frist 60 Minuten.
type outageFixture struct {
	*serviceFixture
	notifier *recordingNotifier
	lastOK   time.Time
}

func newOutageFixture(t *testing.T, responses ...fakeResponse) *outageFixture {
	t.Helper()
	f := &outageFixture{serviceFixture: newServiceFixture(t, responses...), notifier: &recordingNotifier{}}
	f.svc.SetOutageNotifier(f.notifier)
	f.link(t, f.page.ID, f.comp.ID, f.ext.ID)
	f.setExtStatus(t, f.ext, componentstatus.MajorOutage)
	f.lastOK = f.now.Add(-50 * time.Minute)
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Updates(map[string]any{
		"last_success_at": f.lastOK, "outage_after_minutes": 60}).Error)
	return f
}

func (f *outageFixture) effective(t *testing.T) string {
	t.Helper()
	m, err := EffectiveStatuses(f.db, f.page.ID)
	require.NoError(t, err)
	return m[f.comp.ID]
}

func TestOutage_UebergaengeMailNurEinmalErinnerungNach24h(t *testing.T) {
	f := newOutageFixture(t, boom, boom, boom, boom, okSummary("e9", comp("vm", "VM", "operational")))
	ctx := context.Background()
	start := f.now

	// Fehler, aber erst 50 Minuten Funkstille: kein Ausfall, Status zählt noch.
	f.svc.RunOnce(ctx)
	require.Equal(t, 1, f.provider.calls())
	assert.Empty(t, f.notifier.kinds())
	assert.Nil(t, f.source(t).OutageSince)
	assert.Equal(t, componentstatus.MajorOutage, f.effective(t))

	// Genau 60 Minuten nach dem letzten Erfolg: Ausfall.
	f.now = start.Add(10 * time.Minute)
	f.svc.RunOnce(ctx)
	require.Equal(t, 2, f.provider.calls())
	src := f.source(t)
	require.NotNil(t, src.OutageSince)
	assert.True(t, src.OutageSince.Equal(f.lastOK), "Ausfall seit dem letzten Erfolg")
	require.NotNil(t, src.OutageNotifiedAt)
	assert.True(t, src.OutageNotifiedAt.Equal(f.now))
	assert.Nil(t, src.OutageRemindedAt)
	assert.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds())
	assert.Contains(t, f.notifier.events[0].Source.LastError, "HTTP 503")
	assert.Equal(t, componentstatus.Operational, f.effective(t), "veralteter Status zählt nicht mehr")
	require.Len(t, f.listener.changes, 1)
	assert.Equal(t, StatusChange{StatusPageID: f.page.ID, ComponentID: f.comp.ID, Previous: componentstatus.MajorOutage,
		Current: componentstatus.Operational, Cause: CauseOutage, SourceID: f.src.ID}, f.listener.changes[0])

	// Weitere fehlgeschlagene Abrufe: keine weitere Mail.
	f.now = start.Add(time.Hour)
	f.svc.RunOnce(ctx)
	require.Equal(t, 3, f.provider.calls())
	assert.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds())

	// Knapp vor 24 h nach der ersten Meldung: noch keine Erinnerung; dann genau eine.
	f.now = start.Add(10*time.Minute + OutageReminderAfter - time.Second)
	f.svc.RunOnce(ctx)
	require.Equal(t, 4, f.provider.calls())
	assert.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds())
	f.now = start.Add(10*time.Minute + OutageReminderAfter)
	f.svc.evaluateOutage(ctx, f.src.ID)
	assert.Equal(t, []OutageKind{OutageStarted, OutageReminder}, f.notifier.kinds())
	f.svc.evaluateOutage(ctx, f.src.ID)
	f.now = f.now.Add(48 * time.Hour)
	f.svc.evaluateOutage(ctx, f.src.ID)
	assert.Equal(t, []OutageKind{OutageStarted, OutageReminder}, f.notifier.kinds(), "nur eine Erinnerung")
	require.NotNil(t, f.source(t).OutageRemindedAt)

	// Wiederherstellung: Entwarnung, Zustand gelöscht, frischer Status zählt.
	f.svc.RunOnce(ctx)
	require.Equal(t, 5, f.provider.calls())
	assert.Equal(t, []OutageKind{OutageStarted, OutageReminder, OutageRecovered}, f.notifier.kinds())
	src = f.source(t)
	assert.Nil(t, src.OutageSince)
	assert.Nil(t, src.OutageNotifiedAt)
	assert.Nil(t, src.OutageRemindedAt)
	assert.Equal(t, componentstatus.Operational, f.effective(t))
	last := f.notifier.events[2]
	require.NotNil(t, last.Source.OutageSince, "Entwarnung trägt den Beginn des Ausfalls")
	assert.True(t, last.Source.OutageSince.Equal(f.lastOK))
}

func TestOutage_WiederherstellungStelltFrischenStatusWiederHer(t *testing.T) {
	f := newOutageFixture(t, boom, okSummary("e2", comp("vm", "VM", "partial_outage")))
	ctx := context.Background()
	f.now = f.now.Add(10 * time.Minute)
	f.svc.RunOnce(ctx)
	require.Equal(t, componentstatus.Operational, f.effective(t))

	f.now = f.now.Add(time.Hour)
	f.svc.RunOnce(ctx)
	assert.Equal(t, componentstatus.PartialOutage, f.effective(t))
	assert.Equal(t, []OutageKind{OutageStarted, OutageRecovered}, f.notifier.kinds())
	require.GreaterOrEqual(t, len(f.listener.changes), 2)
	assert.Equal(t, componentstatus.PartialOutage, f.listener.changes[len(f.listener.changes)-1].Current)
}

func TestOutage_NeustartMeldetNichtErneut(t *testing.T) {
	f := newOutageFixture(t, boom)
	ctx := context.Background()
	f.now = f.now.Add(10 * time.Minute)
	f.svc.RunOnce(ctx)
	require.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds())

	// Neuer Dienst auf derselben DB = Neustart bzw. zweite Instanz.
	again := &recordingNotifier{}
	svc := NewService(f.db, Options{
		Providers: map[string]Provider{ProviderSummaryV2: f.provider}, Listener: f.listener, Locker: f.locker,
		Now: func() time.Time { return f.now }, Jitter: func(m time.Duration) time.Duration { return m }, Outage: again,
	})
	f.now = f.now.Add(time.Hour)
	svc.RunOnce(ctx)
	svc.RunOnce(ctx)
	assert.Empty(t, again.kinds(), "gespeicherte Zeitstempel verhindern die zweite Mail")
	assert.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds())
}

func TestOutage_ZweiInstanzenBeanspruchenGenauEineMail(t *testing.T) {
	f := newOutageFixture(t, boom)
	f.now = f.now.Add(10 * time.Minute)
	ctx := context.Background()
	// Beide Instanzen lesen den Stand vor dem Ausfall; erst danach schreibt eine.
	var stale models.ExternalSource
	require.NoError(t, f.db.First(&stale, f.src.ID).Error)
	f.svc.evaluateOutage(ctx, f.src.ID)
	require.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds())
	assert.False(t, f.svc.setOutage(ctx, &stale, f.lastOK), "Übergang ist schon beansprucht")
	f.svc.notifyOutage(ctx, stale, f.now)
	assert.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds(), "Zeitstempel ist schon beansprucht")
}

func TestOutage_NichtFaelligeQuelleWirdTrotzdemBewertet(t *testing.T) {
	f := newOutageFixture(t, boom)
	f.now = f.now.Add(10 * time.Minute)
	// Gerade erst abgerufen (nicht fällig), aber seit 60 Minuten kein Erfolg.
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("last_fetch_at", f.now).Error)
	f.svc.RunOnce(context.Background())
	assert.Equal(t, 0, f.provider.calls())
	assert.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds())
}

func TestOutage_304ZaehltAlsErfolg(t *testing.T) {
	f := newOutageFixture(t, fakeResponse{sum: &Summary{NotModified: true}})
	f.now = f.now.Add(15 * time.Minute) // 65 Minuten seit dem letzten Erfolg
	f.svc.RunOnce(context.Background())
	assert.Empty(t, f.notifier.kinds(), "Erfolg (304) vor der Bewertung")
	assert.Nil(t, f.source(t).OutageSince)
}

func TestOutage_FristJeQuelle(t *testing.T) {
	f := newOutageFixture(t, boom)
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("outage_after_minutes", 120).Error)
	f.now = f.now.Add(15 * time.Minute) // 65 Minuten
	f.svc.RunOnce(context.Background())
	assert.Empty(t, f.notifier.kinds(), "Frist 120 Minuten noch nicht erreicht")
	f.now = f.now.Add(55 * time.Minute)
	f.svc.evaluateOutage(context.Background(), f.src.ID)
	assert.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds())
}

func TestOutage_OhneAusfallMailKeineEntwarnung(t *testing.T) {
	f := newOutageFixture(t, okSummary("e1", comp("vm", "VM", "operational")))
	// Ausfall ist gesetzt, die Mail aber nie beansprucht worden (z. B. Absturz).
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("outage_since", f.lastOK).Error)
	f.svc.RunOnce(context.Background())
	assert.Nil(t, f.source(t).OutageSince)
	assert.Empty(t, f.notifier.kinds())
}

func TestOutage_QuelleOhneErfolgZaehltAbAnlage(t *testing.T) {
	f := newOutageFixture(t, boom)
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("last_success_at", nil).Error)
	src := f.source(t)
	assert.True(t, outageReference(src).Equal(src.CreatedAt))
	f.now = src.CreatedAt.Add(59 * time.Minute)
	f.svc.evaluateOutage(context.Background(), f.src.ID)
	assert.Empty(t, f.notifier.kinds())
	f.now = src.CreatedAt.Add(60 * time.Minute)
	f.svc.evaluateOutage(context.Background(), f.src.ID)
	assert.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds())
}

func TestOutage_DBFehler(t *testing.T) {
	t.Run("Quelle nicht lesbar", func(t *testing.T) {
		f := newOutageFixture(t, boom)
		f.now = f.now.Add(10 * time.Minute)
		failOn(t, f.db, "query", "external_sources", 0)
		f.svc.evaluateOutage(context.Background(), f.src.ID)
		assert.Empty(t, f.notifier.kinds())
	})
	t.Run("Ausfall nicht speicherbar", func(t *testing.T) {
		f := newOutageFixture(t, boom)
		f.now = f.now.Add(10 * time.Minute)
		failOn(t, f.db, "update", "external_sources", 0)
		f.svc.evaluateOutage(context.Background(), f.src.ID)
		assert.Empty(t, f.notifier.kinds(), "ohne gesetzten Ausfall keine Mail")
	})
	t.Run("Zeitstempel nicht beanspruchbar", func(t *testing.T) {
		f := newOutageFixture(t, boom)
		f.now = f.now.Add(10 * time.Minute)
		failOn(t, f.db, "update", "external_sources", 1)
		f.svc.evaluateOutage(context.Background(), f.src.ID)
		assert.Empty(t, f.notifier.kinds(), "ohne beanspruchten Zeitstempel keine Mail")
		assert.NotNil(t, f.source(t).OutageSince)
	})
	t.Run("Entwarnung nicht speicherbar", func(t *testing.T) {
		f := newOutageFixture(t, okSummary("e1", comp("vm", "VM", "operational")))
		require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).
			Updates(map[string]any{"outage_since": f.lastOK, "outage_notified_at": f.now}).Error)
		failOn(t, f.db, "update", "external_sources", 1) // 0 = Abrufstatus
		f.svc.RunOnce(context.Background())
		assert.Empty(t, f.notifier.kinds())
		assert.NotNil(t, f.source(t).OutageSince)
	})
	t.Run("Status nicht berechenbar", func(t *testing.T) {
		f := newOutageFixture(t, boom)
		f.now = f.now.Add(10 * time.Minute)
		failOn(t, f.db, "query", "components", 0)
		f.svc.evaluateOutage(context.Background(), f.src.ID)
		assert.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds(), "Ausfall und Mail laufen trotzdem")
		assert.Empty(t, f.listener.changes, "ohne Vorher-Stand kein Ereignis")
	})
	t.Run("Nachher-Stand nicht berechenbar", func(t *testing.T) {
		f := newOutageFixture(t, boom)
		f.now = f.now.Add(10 * time.Minute)
		failOn(t, f.db, "query", "components", 1)
		f.svc.evaluateOutage(context.Background(), f.src.ID)
		assert.Equal(t, []OutageKind{OutageStarted}, f.notifier.kinds())
		assert.Empty(t, f.listener.changes)
	})
}

func TestOutage_OhneEmpfaengerNurLog(t *testing.T) {
	f := newOutageFixture(t, boom)
	f.svc.SetOutageNotifier(nil)
	f.now = f.now.Add(10 * time.Minute)
	f.svc.RunOnce(context.Background())
	assert.NotNil(t, f.source(t).OutageNotifiedAt, "Zustand wird auch ohne Empfänger geführt")
}

func TestOutageAfter(t *testing.T) {
	assert.Equal(t, time.Hour, OutageAfter(models.ExternalSource{}), "Altdaten: Standard")
	assert.Equal(t, 10*time.Minute, OutageAfter(models.ExternalSource{OutageAfterMinutes: 10}))
	assert.Equal(t, 10*time.Minute, OutageAfter(models.ExternalSource{OutageAfterMinutes: 3}), "Minimum 10")
	assert.Equal(t, 24*time.Hour, OutageAfter(models.ExternalSource{OutageAfterMinutes: 1440}))
}

func TestEffective_AusgefalleneQuelleZaehltNicht(t *testing.T) {
	assert.Equal(t, componentstatus.Operational, Effective(componentstatus.Operational, []models.ExternalStatusRef{
		{Status: componentstatus.MajorOutage, SourceOutage: true},
	}))
	assert.Equal(t, componentstatus.PartialOutage, Effective(componentstatus.Operational, []models.ExternalStatusRef{
		{Status: componentstatus.MajorOutage, SourceOutage: true}, {Status: componentstatus.PartialOutage},
	}), "andere Quellen zählen weiter")
}

func TestLinkedRefs_MarkiertAusfall(t *testing.T) {
	f := newStoreFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.ext.ID)
	refs, err := LinkedRefs(f.db, f.page.ID)
	require.NoError(t, err)
	assert.False(t, refs[f.comp.ID][0].SourceOutage)
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("outage_since", f.clock).Error)
	refs, err = LinkedRefs(f.db, f.page.ID)
	require.NoError(t, err)
	assert.True(t, refs[f.comp.ID][0].SourceOutage)
}

// ein Abruf je Quelle.

// blockingProvider hält den Abruf an, bis release geschlossen wird.
type blockingProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (p *blockingProvider) FetchSummary(context.Context, FetchRequest) (*Summary, error) {
	p.entered <- struct{}{}
	<-p.release
	return summaryOf(), nil
}

func TestPoll_LaeuftSchonEinAbrufMeldetFetchInProgress(t *testing.T) {
	f := newServiceFixture(t)
	bp := &blockingProvider{entered: make(chan struct{}), release: make(chan struct{})}
	f.svc.providers[ProviderSummaryV2] = bp
	src := f.source(t)

	done := make(chan FetchResult)
	go func() { done <- f.svc.FetchNow(context.Background(), src) }()
	<-bp.entered

	res := f.svc.FetchNow(context.Background(), src)
	assert.False(t, res.OK)
	assert.Equal(t, ErrFetchInProgress, res.Error)
	assert.True(t, res.FetchedAt.Equal(f.now))
	assert.Equal(t, "fetch_in_progress", ErrFetchInProgress)

	// Der Hintergrund-Takt überspringt die Quelle ebenfalls, ohne sie zu planen.
	f.svc.RunOnce(context.Background())
	assert.NotContains(t, f.svc.state, f.src.ID)

	close(bp.release)
	first := <-done
	assert.True(t, first.OK)

	// Danach ist die Quelle wieder frei.
	bp.release = make(chan struct{})
	close(bp.release)
	go func() { <-bp.entered }()
	assert.True(t, f.svc.FetchNow(context.Background(), src).OK)
}

func TestPoll_QuellenSperreAndererInstanz(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1"))
	other := &fakeLocker{ok: false}
	f.svc.sourceLocker = func(uint) Locker { return other }
	res := f.svc.FetchNow(context.Background(), f.source(t))
	assert.False(t, res.OK)
	assert.Equal(t, ErrFetchInProgress, res.Error)
	assert.Equal(t, 0, f.provider.calls())
	assert.NotContains(t, f.svc.inflight, f.src.ID, "Markierung wird wieder freigegeben")

	held := &fakeLocker{ok: true}
	f.svc.sourceLocker = func(uint) Locker { return held }
	assert.True(t, f.svc.FetchNow(context.Background(), f.source(t)).OK)
	assert.Equal(t, 1, held.unlocked, "Sperre wird nach dem Abruf freigegeben")
}

func TestPoll_QuellenSperreFehlerGiltAlsBelegt(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1"))
	f.svc.sourceLocker = func(uint) Locker { return &fakeLocker{ok: false, err: errors.New("db weg")} }
	res := f.svc.FetchNow(context.Background(), f.source(t))
	assert.Equal(t, ErrFetchInProgress, res.Error)
	assert.Equal(t, 0, f.provider.calls())
}

func TestSourceLockKey(t *testing.T) {
	assert.NotEqual(t, advisoryLockKey, sourceLockKey(1))
	assert.NotEqual(t, sourceLockKey(1), sourceLockKey(2))
	assert.Greater(t, sourceLockKey(1), advisoryLockKey)
}
