package externalstatus

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider liefert die nächste Antwort aus responses (letzte bleibt).
type fakeProvider struct {
	mu        sync.Mutex
	responses []fakeResponse
	requests  []FetchRequest
}

type fakeResponse struct {
	sum *Summary
	err error
}

func (p *fakeProvider) FetchSummary(_ context.Context, req FetchRequest) (*Summary, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
	r := p.responses[0]
	if len(p.responses) > 1 {
		p.responses = p.responses[1:]
	}
	return r.sum, r.err
}

func (p *fakeProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

type fakeLocker struct {
	mu       sync.Mutex
	ok       bool
	err      error
	unlocked int
	tries    int // Zahl der TryLock-Aufrufe = Zahl der gestarteten Takte
}

func (l *fakeLocker) TryLock(context.Context) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tries++
	return l.ok, l.err
}

func (l *fakeLocker) tryCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tries
}
func (l *fakeLocker) Unlock() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.unlocked++
}

type recordingListener struct {
	mu      sync.Mutex
	changes []StatusChange
}

func (r *recordingListener) EffectiveStatusChanged(_ context.Context, ch StatusChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.changes = append(r.changes, ch)
}

type serviceFixture struct {
	*storeFixture
	svc      *Service
	provider *fakeProvider
	locker   *fakeLocker
	listener *recordingListener
	now      time.Time
}

// newServiceFixture: Fake-Provider, Fake-Uhr, fester Jitter (immer das
// Maximum, damit Erwartungen exakt sind).
func newServiceFixture(t *testing.T, responses ...fakeResponse) *serviceFixture {
	t.Helper()
	f := &serviceFixture{storeFixture: newStoreFixture(t), provider: &fakeProvider{responses: responses},
		locker: &fakeLocker{ok: true}, listener: &recordingListener{}}
	f.now = f.clock
	f.svc = NewService(f.db, Options{
		Providers: map[string]Provider{ProviderSummaryV2: f.provider},
		Listener:  f.listener,
		Locker:    f.locker,
		Now:       func() time.Time { return f.now },
		Jitter:    func(max time.Duration) time.Duration { return max },
	})
	// Nur die eigene Quelle aktiv, damit RunOnce-Zählungen eindeutig sind.
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.otherSrc.ID).Update("active", false).Error)
	return f
}

func (f *serviceFixture) source(t *testing.T) models.ExternalSource {
	t.Helper()
	var src models.ExternalSource
	require.NoError(t, f.db.First(&src, f.src.ID).Error)
	return src
}

func okSummary(etag string, comps ...Component) fakeResponse {
	s := summaryOf(comps...)
	s.ETag, s.LastModified = etag, "Thu, 29 Oct 2026 12:00:00 GMT"
	return fakeResponse{sum: s}
}

func TestNewService_Defaults(t *testing.T) {
	db := database.DB
	s := NewService(db, Options{})
	assert.Equal(t, []string{ProviderSummaryV2}, s.SupportedProviders())
	assert.True(t, s.SupportsProvider(ProviderSummaryV2))
	assert.False(t, s.SupportsProvider("statusio"))
	assert.IsType(t, LogListener{}, s.Listener())
	assert.IsType(t, &database.AdvisoryLock{}, s.locker)
	assert.Equal(t, defaultTick, s.tick)
	assert.Equal(t, defaultMaxBackoff, s.maxBackoff)
	assert.NotNil(t, s.now())
	assert.Less(t, s.jitter(time.Second), time.Second)
}

func TestRandomJitter(t *testing.T) {
	assert.Equal(t, time.Duration(0), randomJitter(0))
	assert.Equal(t, time.Duration(0), randomJitter(-time.Second))
	for range 50 {
		j := randomJitter(time.Minute)
		assert.GreaterOrEqual(t, j, time.Duration(0))
		assert.Less(t, j, time.Minute)
	}
}

func TestNextBackoff(t *testing.T) {
	iv, capD := 2*time.Minute, 30*time.Minute
	assert.Equal(t, 4*time.Minute, nextBackoff(0, iv, capD), "erster Fehler: doppeltes Intervall")
	assert.Equal(t, 8*time.Minute, nextBackoff(4*time.Minute, iv, capD))
	assert.Equal(t, 16*time.Minute, nextBackoff(8*time.Minute, iv, capD))
	assert.Equal(t, 30*time.Minute, nextBackoff(16*time.Minute, iv, capD), "Deckel")
	assert.Equal(t, 30*time.Minute, nextBackoff(30*time.Minute, iv, capD))
	assert.Equal(t, time.Hour, nextBackoff(0, time.Hour, capD), "nie kürzer als das Intervall")
}

func TestPollInterval(t *testing.T) {
	assert.Equal(t, 2*time.Minute, PollInterval(models.ExternalSource{PollIntervalSeconds: 120}))
	assert.Equal(t, time.Minute, PollInterval(models.ExternalSource{PollIntervalSeconds: 5}), "Minimum")
	assert.Equal(t, time.Minute, PollInterval(models.ExternalSource{PollIntervalSeconds: 60}))
}

func TestPollingEnabled(t *testing.T) {
	for v, want := range map[string]bool{"": true, "true": true, "1": true, "yes": true, "false": false, "0": false, "no": false, "off": false, " OFF ": false, "False": false} {
		assert.Equal(t, want, PollingEnabled(func(string) string { return v }), "%q", v)
	}
}

func TestStartFromEnv(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1", comp("vm", "VM", "operational")))
	stop := StartFromEnv(context.Background(), f.svc, func(string) string { return "false" })
	stop()
	assert.Equal(t, 0, f.provider.calls(), "abgeschaltet: kein Abruf")
	assert.Equal(t, 0, f.locker.unlocked)

	f.svc.tick = 5 * time.Millisecond
	stop = StartFromEnv(context.Background(), f.svc, func(string) string { return "" })
	require.Eventually(t, func() bool { return f.provider.calls() >= 1 }, 2*time.Second, 5*time.Millisecond, "sofortiger erster Takt")
	stop()
	f.locker.mu.Lock()
	assert.Equal(t, 1, f.locker.unlocked, "Stopp gibt die Sperre frei")
	f.locker.mu.Unlock()
}

func TestStart_StopptMitContext(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1"))
	f.svc.tick = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	stop := f.svc.Start(ctx)
	// auf Ereignisse statt auf feste Zeit warten. Der dritte TryLock-Aufruf beweist,
	// dass der zweite Takt (über den Ticker, Quelle nicht fällig, Sperre wird erneut geprüft)
	// vollständig durchgelaufen ist.
	require.Eventually(t, func() bool { return f.locker.tryCount() >= 3 }, 10*time.Second, time.Millisecond,
		"mindestens zwei Ticker-Takte nach dem ersten")
	cancel()
	stop()
	assert.Equal(t, 1, f.provider.calls(), "Quelle ist nach dem ersten Abruf erst nach dem Intervall wieder fällig")
}

func TestRunOnce_SperreUndFaelligkeit(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1", comp("vm", "VM", "operational")))
	ctx := context.Background()

	f.locker.ok = false
	f.svc.RunOnce(ctx)
	assert.Equal(t, 0, f.provider.calls(), "ohne Sperre kein Abruf (andere Instanz)")

	f.locker.ok, f.locker.err = true, errors.New("db weg")
	f.svc.RunOnce(ctx)
	assert.Equal(t, 0, f.provider.calls(), "Sperrfehler: kein Abruf")
	f.locker.err = nil

	f.svc.RunOnce(ctx)
	assert.Equal(t, 1, f.provider.calls(), "nie abgerufen → sofort fällig")
	src := f.source(t)
	require.NotNil(t, src.LastFetchOK)
	assert.True(t, *src.LastFetchOK)
	assert.True(t, src.LastFetchAt.Equal(f.now))
	assert.True(t, src.LastSuccessAt.Equal(f.now))
	assert.Equal(t, "e1", src.ETag)
	assert.Equal(t, "Thu, 29 Oct 2026 12:00:00 GMT", src.LastModified)
	assert.Equal(t, "https://status.example.com", src.PageURL)
	assert.Empty(t, src.LastError)

	// next = now + 120 s + Jitter (max = 12 s).
	f.now = f.now.Add(131 * time.Second)
	f.svc.RunOnce(ctx)
	assert.Equal(t, 1, f.provider.calls(), "vor Intervall+Jitter nicht fällig")
	f.now = f.now.Add(time.Second)
	f.svc.RunOnce(ctx)
	assert.Equal(t, 2, f.provider.calls(), "genau bei Intervall+Jitter fällig")
	assert.Equal(t, "e1", f.provider.requests[1].ETag, "Conditional Request mit gespeichertem ETag")
	assert.Equal(t, "https://example.com", f.provider.requests[1].BaseURL)
}

func TestRunOnce_NeustartRichtetSichNachLastFetchAt(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1"))
	last := f.now.Add(-time.Minute)
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("last_fetch_at", last).Error)
	f.svc.RunOnce(context.Background())
	assert.Equal(t, 0, f.provider.calls(), "vor einer Minute abgerufen, Intervall 2 min")
	f.now = last.Add(2 * time.Minute)
	f.svc.RunOnce(context.Background())
	assert.Equal(t, 1, f.provider.calls())
}

func TestRunOnce_UeberspringtInaktiveUndGeloeschteSeiten(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1"))
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("active", false).Error)
	f.svc.RunOnce(context.Background())
	assert.Equal(t, 0, f.provider.calls(), "inaktive Quelle")

	g := newServiceFixture(t, okSummary("e1"))
	require.NoError(t, g.db.Delete(&g.page).Error)
	g.svc.RunOnce(context.Background())
	assert.Equal(t, 0, g.provider.calls(), "Quelle einer gelöschten Statusseite")
}

func TestRunOnce_AbgebrochenerContext(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.svc.RunOnce(ctx)
	assert.Equal(t, 0, f.provider.calls())
}

func TestRunOnce_QuellenNichtLesbar(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1"))
	failOn(t, f.db, "query", "external_sources", 0)
	f.svc.RunOnce(context.Background())
	assert.Equal(t, 0, f.provider.calls())
}

func TestRunOnce_PruneState(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1"))
	f.svc.RunOnce(context.Background())
	require.Contains(t, f.svc.state, f.src.ID)
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("active", false).Error)
	f.svc.RunOnce(context.Background())
	assert.NotContains(t, f.svc.state, f.src.ID, "Zustand deaktivierter Quellen wird verworfen")
}

func TestPoll_BackoffVerdoppeltUndErfolgSetztZurueck(t *testing.T) {
	boom := fakeResponse{err: errors.New("HTTP 503 von https://example.com/api/v2/summary.json")}
	f := newServiceFixture(t, boom, boom, boom, boom, boom, okSummary("e1"))
	ctx := context.Background()

	wantBackoff := []time.Duration{4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for i, want := range wantBackoff {
		f.svc.RunOnce(ctx)
		require.Equal(t, i+1, f.provider.calls())
		st := f.svc.state[f.src.ID]
		assert.Equal(t, want, st.backoff, "Fehler %d", i+1)
		assert.True(t, st.next.Equal(f.now.Add(want+12*time.Second)), "Backoff + Jitter")
		src := f.source(t)
		require.NotNil(t, src.LastFetchOK)
		assert.False(t, *src.LastFetchOK)
		assert.Contains(t, src.LastError, "HTTP 503")
		assert.Nil(t, src.LastSuccessAt)
		// Kurz vorher nicht fällig, dann schon.
		f.now = st.next.Add(-time.Second)
		f.svc.RunOnce(ctx)
		require.Equal(t, i+1, f.provider.calls())
		f.now = st.next
	}

	f.svc.RunOnce(ctx)
	assert.Equal(t, time.Duration(0), f.svc.state[f.src.ID].backoff, "Erfolg setzt zurück")
	src := f.source(t)
	assert.True(t, *src.LastFetchOK)
	assert.Empty(t, src.LastError, "Fehlertext wird gelöscht")
}

func TestPoll_NotModified(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1", comp("vm", "VM", "major_outage")), fakeResponse{sum: &Summary{NotModified: true, ETag: "e1"}})
	f.link(t, f.page.ID, f.comp.ID, f.ext.ID)
	res := f.svc.FetchNow(context.Background(), f.source(t))
	require.True(t, res.OK)
	require.Len(t, f.listener.changes, 1)

	f.now = f.now.Add(time.Hour)
	res = f.svc.FetchNow(context.Background(), f.source(t))
	assert.True(t, res.OK)
	assert.True(t, res.NotModified)
	assert.Equal(t, UpsertResult{}, res.UpsertResult)
	src := f.source(t)
	assert.True(t, src.LastSuccessAt.Equal(f.now), "304 zählt als Erfolg")
	assert.Equal(t, "e1", src.ETag)
	assert.Equal(t, componentstatus.MajorOutage, extRows(t, f.db, f.src.ID)["vm"].Status, "304 lässt Komponenten unverändert")
	assert.Len(t, f.listener.changes, 1, "kein neues Ereignis")
}

func TestPoll_EreignisBeiStatuswechsel(t *testing.T) {
	f := newServiceFixture(t,
		okSummary("e1", comp("vm", "VM", "operational")),
		okSummary("e2", comp("vm", "VM", "major_outage")),
		okSummary("e3", comp("vm", "VM", "major_outage")),
		okSummary("e4"),
	)
	f.link(t, f.page.ID, f.comp.ID, f.ext.ID)
	ctx := context.Background()

	res := f.svc.FetchNow(ctx, f.source(t))
	require.True(t, res.OK, res.Error)
	assert.Equal(t, UpsertResult{Seen: 1}, res.UpsertResult)
	assert.Empty(t, f.listener.changes, "unverändert operational: kein Ereignis")

	f.svc.FetchNow(ctx, f.source(t))
	require.Len(t, f.listener.changes, 1)
	assert.Equal(t, StatusChange{StatusPageID: f.page.ID, ComponentID: f.comp.ID, Previous: componentstatus.Operational,
		Current: componentstatus.MajorOutage, Cause: CauseFetch, SourceID: f.src.ID}, f.listener.changes[0])

	f.svc.FetchNow(ctx, f.source(t))
	assert.Len(t, f.listener.changes, 1, "gleicher Status: kein weiteres Ereignis")

	// Verschwindet die Komponente, zählt sie nicht mehr → zurück auf operational.
	res = f.svc.FetchNow(ctx, f.source(t))
	assert.Equal(t, 1, res.Vanished)
	require.Len(t, f.listener.changes, 2)
	assert.Equal(t, componentstatus.Operational, f.listener.changes[1].Current)

	var stored models.Component
	require.NoError(t, f.db.First(&stored, f.comp.ID).Error)
	assert.Equal(t, componentstatus.Operational, stored.Status, "eigener Status nie überschrieben")
}

func TestPoll_FetchNowAuchFuerInaktiveQuelle(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1", comp("neu", "Neu", "operational")))
	src := f.source(t)
	src.Active = false
	res := f.svc.FetchNow(context.Background(), src)
	assert.True(t, res.OK)
	assert.Equal(t, 1, res.Created)
	assert.True(t, res.FetchedAt.Equal(f.now))
}

func TestPoll_UnbekannterProvider(t *testing.T) {
	f := newServiceFixture(t, okSummary("e1"))
	src := f.source(t)
	src.Provider = "statusio"
	res := f.svc.FetchNow(context.Background(), src)
	assert.False(t, res.OK)
	assert.Equal(t, "unbekannter Provider", res.Error)
	assert.Equal(t, 0, f.provider.calls())
}

func TestPoll_DBFehlerInApply(t *testing.T) {
	cases := []struct {
		name, op, table string
		skip            int
	}{
		{"Vorher-Stand", "query", "components", 0},
		{"Upsert", "create", "external_components", 0},
		{"Validatoren", "update", "external_sources", 0},
		{"Nachher-Stand", "query", "components", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newServiceFixture(t, okSummary("e1", comp("neu", "Neu", "operational")))
			src := f.source(t)
			failOn(t, f.db, tc.op, tc.table, tc.skip)
			res := f.svc.FetchNow(context.Background(), src)
			assert.False(t, res.OK)
			assert.Contains(t, res.Error, "injizierter Fehler")
		})
	}
}

func TestRecordFetch_Fehler(t *testing.T) {
	f := newServiceFixture(t, fakeResponse{sum: &Summary{NotModified: true}})
	src := f.source(t)
	failOn(t, f.db, "update", "external_sources", 0)
	res := f.svc.FetchNow(context.Background(), src)
	assert.True(t, res.OK, "Speicherfehler wird nur geloggt")
	assert.Nil(t, f.source(t).LastFetchAt)
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 3))
	assert.Equal(t, "ab", truncate("abc", 2))
	s := truncate(strings.Repeat("ä", 10), 5)
	assert.Equal(t, "ää", s, "kein zerteiltes UTF-8-Zeichen")
	f := newServiceFixture(t, fakeResponse{err: errors.New(strings.Repeat("x", 2*maxErrorLen))})
	res := f.svc.FetchNow(context.Background(), f.source(t))
	assert.Len(t, res.Error, maxErrorLen)
	assert.Len(t, f.source(t).LastError, maxErrorLen)
}

func TestDiffEffective_UndLogListener(t *testing.T) {
	l := &recordingListener{}
	base := StatusChange{StatusPageID: 1, Cause: CauseLink, SourceID: 9}
	DiffEffective(context.Background(), l,
		map[uint]string{1: "operational", 2: "operational", 3: "major_outage"},
		map[uint]string{1: "operational", 2: "partial_outage", 4: "major_outage"}, base)
	require.Len(t, l.changes, 1, "nur geänderte, in beiden Ständen vorhandene Komponenten")
	assert.Equal(t, StatusChange{StatusPageID: 1, ComponentID: 2, Previous: "operational", Current: "partial_outage", Cause: CauseLink, SourceID: 9}, l.changes[0])

	DiffEffective(context.Background(), l, nil, map[uint]string{2: "major_outage"}, base)
	assert.Len(t, l.changes, 1, "ohne Vorher-Stand kein Ereignis")

	LogListener{}.EffectiveStatusChanged(context.Background(), l.changes[0])
}
