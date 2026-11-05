package externalstatus

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

const (
	// DefaultPollIntervalSeconds/MinPollIntervalSeconds: Abfrage-Intervall
	// einer Quelle. Unter einer Minute würden wir Anbieter unnötig belasten
	// (die Summary-API wird oft nur alle 10 s neu erzeugt).
	DefaultPollIntervalSeconds = 120
	MinPollIntervalSeconds     = 60
	// MaxPollIntervalSeconds: ein Tag; mehr ergibt für einen Status keinen Sinn.
	MaxPollIntervalSeconds = 86400

	// PollingEnabledEnv schaltet den Hintergrund-Abruf ab ("false", "0",
	// "no", "off"); Default an. "Jetzt abrufen" im Admin funktioniert weiter.
	PollingEnabledEnv = "EXTERNAL_STATUS_POLLING_ENABLED"

	defaultTick       = 15 * time.Second
	defaultMaxBackoff = 30 * time.Minute
	// advisoryLockKey: anwendungsweit eindeutiger Schlüssel (frei gewählte Zahl).
	advisoryLockKey int64 = 507484
	// ErrFetchInProgress ist der Fehlertext im FetchResult, wenn für die
	// Quelle gerade ein Abruf läuft.
	ErrFetchInProgress = "fetch_in_progress"
	// maxErrorLen begrenzt den gespeicherten Fehlertext.
	maxErrorLen = 500
)

// sourceLockKey: Schlüssel der Sperre für den Abruf einer einzelnen Quelle.
// Oben im Schlüsselraum der Leader-Sperre (advisoryLockKey), damit beide sich
// nie überschneiden.
func sourceLockKey(id uint) int64 { return advisoryLockKey<<32 | int64(id) }

// Locker verhindert, dass mehrere Instanzen gleichzeitig abfragen
// (Standard: database.AdvisoryLock).
type Locker interface {
	TryLock(ctx context.Context) (bool, error)
	Unlock()
}

// Options konfiguriert NewService. Leere Felder = Produktionswerte; Tests
// setzen Fake-Provider, Fake-Uhr und festen Jitter.
type Options struct {
	Providers    map[string]Provider
	Listener     ChangeListener
	Locker       Locker
	Now          func() time.Time
	Jitter       func(max time.Duration) time.Duration
	TickInterval time.Duration
	MaxBackoff   time.Duration
	// SourceLocker liefert die Sperre, die einen Abruf genau einer Quelle
	// gegen Überschneidung schützt; Standard: database.AdvisoryLock
	// mit sourceLockKey. Tests setzen eine Fake-Sperre.
	SourceLocker func(sourceID uint) Locker
	// Outage empfängt Ausfall-Ereignisse; nil = nur Logeintrag.
	Outage OutageNotifier
	// Incidents empfängt Vorfall-/Wartungs-Ereignisse; nil = nur spiegeln.
	Incidents IncidentNotifier
	// Retention: Aufbewahrung beendeter gespiegelter Meldungen;
	// 0 = DefaultRetentionDays.
	Retention time.Duration
}

// Service ist der Abfrage-Dienst für externe Statusseiten.
type Service struct {
	db         *gorm.DB
	providers  map[string]Provider
	listener   ChangeListener
	locker     Locker
	now        func() time.Time
	jitter     func(time.Duration) time.Duration
	tick       time.Duration
	maxBackoff time.Duration

	sourceLocker func(uint) Locker
	outage       OutageNotifier
	incidents    IncidentNotifier
	retention    time.Duration
	lastPrune    time.Time

	// fetchMu serialisiert die Abrufe im Prozess (Hintergrund-Takt und
	// "Jetzt abrufen"): Die Ereignis-Erkennung vergleicht effektive Status
	// vor und nach einem Abruf und verträgt keine überlappenden Abrufe
	// derselben Seite.
	fetchMu sync.Mutex
	// mu schützt state und inflight (nur kurze Abschnitte, nie während des
	// Netzwerkabrufs).
	mu       sync.Mutex
	state    map[uint]*sourceState
	inflight map[uint]bool
}

// sourceState: nächster geplanter Abruf und aktuelle Backoff-Pause einer
// Quelle. Bewusst nur im Speicher: Nach einem Neustart richtet sich der
// erste Abruf nach LastFetchAt, ein laufender Backoff beginnt neu.
type sourceState struct {
	next    time.Time
	backoff time.Duration
}

// FetchResult ist das Ergebnis eines Abrufs (auch Antwort von "Jetzt abrufen").
type FetchResult struct {
	OK          bool      `json:"ok"`
	NotModified bool      `json:"not_modified"`
	Error       string    `json:"error"`
	FetchedAt   time.Time `json:"fetched_at"`
	UpsertResult
}

// NewService baut den Dienst; Start startet die Hintergrund-Schleife.
func NewService(db *gorm.DB, opts Options) *Service {
	s := &Service{
		db:         db,
		providers:  opts.Providers,
		listener:   opts.Listener,
		locker:     opts.Locker,
		now:        opts.Now,
		jitter:     opts.Jitter,
		tick:       opts.TickInterval,
		maxBackoff: opts.MaxBackoff,
		state:      map[uint]*sourceState{},
		inflight:   map[uint]bool{},

		sourceLocker: opts.SourceLocker,
		outage:       opts.Outage,
		incidents:    opts.Incidents,
		retention:    opts.Retention,
	}
	if s.retention <= 0 {
		s.retention = DefaultRetentionDays * 24 * time.Hour
	}
	if s.providers == nil {
		s.providers = map[string]Provider{ProviderSummaryV2: SummaryV2Provider{Client: NewSafeClient(ClientConfig{})}}
	}
	if s.listener == nil {
		s.listener = LogListener{}
	}
	if s.locker == nil {
		s.locker = database.NewAdvisoryLock(db, advisoryLockKey)
	}
	if s.sourceLocker == nil {
		s.sourceLocker = func(id uint) Locker { return database.NewAdvisoryLock(db, sourceLockKey(id)) }
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.jitter == nil {
		s.jitter = randomJitter
	}
	if s.tick <= 0 {
		s.tick = defaultTick
	}
	if s.maxBackoff <= 0 {
		s.maxBackoff = defaultMaxBackoff
	}
	return s
}

// Listener liefert den Empfänger für StatusChange-Ereignisse (auch für die
// Admin-Handler beim Koppeln/Entkoppeln).
func (s *Service) Listener() ChangeListener { return s.listener }

func randomJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(max)))
}

// PollInterval liefert das Intervall einer Quelle, mindestens MinPollIntervalSeconds.
func PollInterval(src models.ExternalSource) time.Duration {
	return time.Duration(max(src.PollIntervalSeconds, MinPollIntervalSeconds)) * time.Second
}

// nextBackoff (Muster): Nach einem Fehler verdoppelt sich die
// Pause, beginnend beim doppelten Intervall, gedeckelt bei maxBackoff (aber
// nie kürzer als das normale Intervall). Ein Erfolg setzt sie zurück (poll).
func nextBackoff(cur, interval, maxBackoff time.Duration) time.Duration {
	cur = max(cur, interval)
	return min(cur*2, max(maxBackoff, interval))
}

// PollingEnabled liest PollingEnabledEnv; alles außer false/0/no/off = an.
func PollingEnabled(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(PollingEnabledEnv))) {
	case "false", "0", "no", "off":
		return false
	}
	return true
}

// StartFromEnv startet die Hintergrund-Schleife, sofern nicht per
// PollingEnabledEnv abgeschaltet, und liefert die Stopp-Funktion (wartet,
// bis die Schleife beendet ist). Aufruf in main.go: defer StartFromEnv(...)().
func StartFromEnv(ctx context.Context, s *Service, getenv func(string) string) (stop func()) {
	if !PollingEnabled(getenv) {
		slog.Info("Abruf externer Statusseiten deaktiviert", "env", PollingEnabledEnv)
		return func() {}
	}
	slog.Info("Abruf externer Statusseiten gestartet")
	return s.Start(ctx)
}

// Start startet die Hintergrund-Schleife: sofort ein Takt, danach alle
// TickInterval. Die zurückgegebene Funktion beendet sie, wartet darauf und
// gibt die DB-Sperre frei.
func (s *Service) Start(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer s.locker.Unlock()
		ticker := time.NewTicker(s.tick)
		defer ticker.Stop()
		for {
			s.RunOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// RunOnce ist ein Takt: Sperre holen, fällige aktive Quellen abrufen.
// Quellen gelöschter Statusseiten bleiben außen vor.
func (s *Service) RunOnce(ctx context.Context) {
	locked, err := s.locker.TryLock(ctx)
	if err != nil || !locked {
		if err != nil {
			slog.Warn("Externe Statusseiten: DB-Sperre nicht verfügbar", "error", err)
		}
		return
	}
	var sources []models.ExternalSource
	if err := s.db.Where("active = ? AND status_page_id IN (?)", true, s.db.Model(&models.StatusPage{}).Select("id")).
		Order("id").Find(&sources).Error; err != nil {
		slog.Error("Externe Statusseiten: Quellen nicht lesbar", "error", err)
		return
	}
	s.pruneState(sources)
	for _, src := range sources {
		if ctx.Err() != nil {
			return
		}
		if s.isDue(src, s.now()) {
			s.poll(ctx, src) // wertet den Ausfall nach dem Abruf aus
		} else {
			s.evaluateOutage(ctx, src.ID)
		}
	}
	s.maybePrune(ctx)
}

// pruneState entfernt den Zustand gelöschter oder deaktivierter Quellen.
func (s *Service) pruneState(active []models.ExternalSource) {
	keep := make(map[uint]bool, len(active))
	for _, src := range active {
		keep[src.ID] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.state {
		if !keep[id] {
			delete(s.state, id)
		}
	}
}

// isDue: Ohne Zustand (erster Takt nach dem Start) zählt LastFetchAt, damit
// ein Neustart die Anbieter nicht sofort erneut abfragt.
func (s *Service) isDue(src models.ExternalSource, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.state[src.ID]; ok {
		return !now.Before(st.next)
	}
	return src.LastFetchAt == nil || !now.Before(src.LastFetchAt.Add(PollInterval(src)))
}

// FetchNow ruft eine Quelle sofort ab ("Jetzt abrufen" im Admin), auch wenn
// sie inaktiv ist, und plant den nächsten Abruf neu.
func (s *Service) FetchNow(ctx context.Context, src models.ExternalSource) FetchResult {
	return s.poll(ctx, src)
}

// errUnknownProvider: Quelle mit einem Provider, für den es keinen Adapter gibt.
var errUnknownProvider = errors.New("unbekannter Provider")

// poll ruft eine Quelle ab, übernimmt das Ergebnis und plant den nächsten Abruf.
//
// Pro Quelle läuft höchstens ein Abruf. Im Prozess verhindert das
// inflight, über Instanzen hinweg die Advisory-Sperre der Quelle (dieselbe
// Mechanik wie die Leader-Sperre des Hintergrund-Abrufs, nur mit eigenem
// Schlüssel je Quelle). Läuft schon ein Abruf, kommt sofort ErrFetchInProgress
// zurück, statt zu warten: Ein zweiter Abruf direkt danach brächte nichts
// Neues (die Fremdseite cached ohnehin), und die Anfrage bliebe nicht bis zu
// 10 s hängen. Der Hintergrund-Takt überspringt die Quelle und versucht es
// beim nächsten Takt erneut.
func (s *Service) poll(ctx context.Context, src models.ExternalSource) FetchResult {
	if !s.beginFetch(src.ID) {
		return s.busyResult()
	}
	defer s.endFetch(src.ID)
	lock := s.sourceLocker(src.ID)
	got, lockErr := lock.TryLock(ctx)
	if lockErr != nil {
		slog.Warn("Externe Statusseite: Quellen-Sperre nicht verfügbar", "source_id", src.ID, "error", lockErr)
	}
	if !got {
		return s.busyResult()
	}
	defer lock.Unlock()
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()

	now := s.now()
	res := FetchResult{FetchedAt: now}
	interval := PollInterval(src)

	sum, err := s.fetch(ctx, src)
	if err == nil && !sum.NotModified {
		res.UpsertResult, err = s.apply(ctx, src, sum, now)
	}
	if err != nil {
		retryIn := s.schedule(src.ID, now, interval, true)
		res.Error = truncate(err.Error(), maxErrorLen)
		s.recordFetch(src.ID, now, false, res.Error)
		slog.Warn("Externe Statusseite: Abruf fehlgeschlagen", "source_id", src.ID, "error", res.Error, "retry_in", retryIn)
		s.evaluateOutage(ctx, src.ID)
		return res
	}
	s.schedule(src.ID, now, interval, false)
	res.OK = true
	res.NotModified = sum.NotModified
	s.recordFetch(src.ID, now, true, "")
	// Vorfälle und Wartungen; ein Fehler dort lässt den Abruf gelingen.
	s.syncIncidents(ctx, src.ID, now)
	s.evaluateOutage(ctx, src.ID)
	return res
}

func (s *Service) busyResult() FetchResult {
	return FetchResult{FetchedAt: s.now(), Error: ErrFetchInProgress}
}

// beginFetch markiert die Quelle als in Arbeit; false, wenn sie es schon ist.
func (s *Service) beginFetch(id uint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[id] {
		return false
	}
	s.inflight[id] = true
	return true
}

func (s *Service) endFetch(id uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, id)
}

// schedule plant den nächsten Abruf: nach einem Fehler mit wachsender Pause
// (Backoff), sonst im normalen Intervall. Liefert die Backoff-Pause.
func (s *Service) schedule(id uint, now time.Time, interval time.Duration, failed bool) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.state[id]
	if !ok {
		st = &sourceState{}
		s.state[id] = st
	}
	if failed {
		st.backoff = nextBackoff(st.backoff, interval, s.maxBackoff)
		st.next = now.Add(st.backoff + s.jitter(interval/10))
		return st.backoff
	}
	st.backoff = 0
	st.next = now.Add(interval + s.jitter(interval/10))
	return 0
}

func (s *Service) fetch(ctx context.Context, src models.ExternalSource) (*Summary, error) {
	p, ok := s.providers[src.Provider]
	if !ok {
		return nil, errUnknownProvider
	}
	return p.FetchSummary(ctx, FetchRequest{BaseURL: src.BaseURL, ETag: src.ETag, LastModified: src.LastModified})
}

// apply übernimmt einen 200-Abruf: Upsert und Validatoren in einer
// Transaktion, danach Ereignisse für geänderte effektive Status.
func (s *Service) apply(ctx context.Context, src models.ExternalSource, sum *Summary, now time.Time) (UpsertResult, error) {
	before, err := EffectiveStatuses(s.db, src.StatusPageID)
	if err != nil {
		return UpsertResult{}, err
	}
	var res UpsertResult
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		if res, txErr = upsertComponents(tx, src, sum, now); txErr != nil {
			return txErr
		}
		return tx.Model(&models.ExternalSource{}).Where("id = ?", src.ID).
			Select("ETag", "LastModified", "PageURL").
			Updates(models.ExternalSource{ETag: sum.ETag, LastModified: sum.LastModified, PageURL: sum.PageURL}).Error
	})
	if err != nil {
		return UpsertResult{}, err
	}
	after, err := EffectiveStatuses(s.db, src.StatusPageID)
	if err != nil {
		return res, err
	}
	DiffEffective(ctx, s.listener, before, after, StatusChange{StatusPageID: src.StatusPageID, Cause: CauseFetch, SourceID: src.ID})
	return res, nil
}

// recordFetch speichert das Ergebnis am Source-Datensatz. Ein Fehler beim
// Speichern wird nur geloggt: Der nächste Takt versucht es ohnehin erneut.
func (s *Service) recordFetch(id uint, now time.Time, ok bool, errText string) {
	updates := map[string]any{"last_fetch_at": now, "last_fetch_ok": ok, "last_error": errText}
	if ok {
		updates["last_success_at"] = now
	}
	if err := s.db.Model(&models.ExternalSource{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.Error("Externe Statusseite: Abrufstatus nicht gespeichert", "source_id", id, "error", err)
	}
}

// truncate kürzt auf höchstens n Bytes, ohne ein UTF-8-Zeichen zu zerteilen
// (Postgres lehnt ungültiges UTF-8 ab).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
