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
	// DefaultPollIntervalSeconds/MinPollIntervalSeconds: polling interval
	// of a source. Below one minute we would burden vendors needlessly
	// (the summary API is often regenerated only every 10 s).
	DefaultPollIntervalSeconds = 120
	MinPollIntervalSeconds     = 60
	// MaxPollIntervalSeconds: one day; more makes no sense for a status.
	MaxPollIntervalSeconds = 86400

	// PollingEnabledEnv turns off background polling ("false", "0",
	// "no", "off"); default on. "Fetch now" in the admin keeps working.
	PollingEnabledEnv = "EXTERNAL_STATUS_POLLING_ENABLED"

	defaultTick       = 15 * time.Second
	defaultMaxBackoff = 30 * time.Minute
	// advisoryLockKey: application-wide unique key (freely chosen number).
	advisoryLockKey int64 = 507484
	// ErrFetchInProgress is the error text in FetchResult when a fetch for the
	// source is currently running.
	ErrFetchInProgress = "fetch_in_progress"
	// maxErrorLen limits the stored error text.
	maxErrorLen = 500
)

// sourceLockKey: key of the lock for fetching a single source.
// At the top of the key space of the leader lock (advisoryLockKey), so the two
// never overlap.
func sourceLockKey(id uint) int64 { return advisoryLockKey<<32 | int64(id) }

// Locker prevents multiple instances from polling at the same time
// (default: database.AdvisoryLock).
type Locker interface {
	TryLock(ctx context.Context) (bool, error)
	Unlock()
}

// Options configures NewService. Empty fields = production values; tests
// set a fake provider, fake clock and fixed jitter.
type Options struct {
	Providers    map[string]Provider
	Listener     ChangeListener
	Locker       Locker
	Now          func() time.Time
	Jitter       func(max time.Duration) time.Duration
	TickInterval time.Duration
	MaxBackoff   time.Duration
	// SourceLocker provides the lock that protects a fetch of exactly one source
	// against overlap; default: database.AdvisoryLock
	// with sourceLockKey. Tests set a fake lock.
	SourceLocker func(sourceID uint) Locker
	// Outage receives outage events; nil = log entry only.
	Outage OutageNotifier
	// Incidents receives incident/maintenance events; nil = mirror only.
	Incidents IncidentNotifier
	// Retention: retention of finished mirrored reports;
	// 0 = DefaultRetentionDays.
	Retention time.Duration
}

// Service is the polling service for external status pages.
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

	// fetchMu serializes the fetches within the process (background tick and
	// "Fetch now"): the event detection compares effective statuses
	// before and after a fetch and cannot cope with overlapping fetches
	// of the same page.
	fetchMu sync.Mutex
	// mu protects state and inflight (only short sections, never during the
	// network fetch).
	mu       sync.Mutex
	state    map[uint]*sourceState
	inflight map[uint]bool
}

// sourceState: next scheduled fetch and current backoff pause of a
// source. Deliberately in memory only: after a restart the first fetch
// is based on LastFetchAt, a running backoff starts over.
type sourceState struct {
	next    time.Time
	backoff time.Duration
}

// FetchResult is the result of a fetch (also the response to "Fetch now").
type FetchResult struct {
	OK          bool      `json:"ok"`
	NotModified bool      `json:"not_modified"`
	Error       string    `json:"error"`
	FetchedAt   time.Time `json:"fetched_at"`
	UpsertResult
}

// NewService builds the service; Start starts the background loop.
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

// Listener returns the recipient for StatusChange events (also for the
// admin handlers when linking/unlinking).
func (s *Service) Listener() ChangeListener { return s.listener }

func randomJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(max)))
}

// PollInterval returns a source's interval, at least MinPollIntervalSeconds.
func PollInterval(src models.ExternalSource) time.Duration {
	return time.Duration(max(src.PollIntervalSeconds, MinPollIntervalSeconds)) * time.Second
}

// nextBackoff (pattern): after an error the pause doubles,
// starting at twice the interval, capped at maxBackoff (but
// never shorter than the normal interval). A success resets it (poll).
func nextBackoff(cur, interval, maxBackoff time.Duration) time.Duration {
	cur = max(cur, interval)
	return min(cur*2, max(maxBackoff, interval))
}

// PollingEnabled reads PollingEnabledEnv; anything except false/0/no/off = on.
func PollingEnabled(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(PollingEnabledEnv))) {
	case "false", "0", "no", "off":
		return false
	}
	return true
}

// StartFromEnv starts the background loop unless turned off via
// PollingEnabledEnv, and returns the stop function (waits
// until the loop has ended). Call in main.go: defer StartFromEnv(...)().
func StartFromEnv(ctx context.Context, s *Service, getenv func(string) string) (stop func()) {
	if !PollingEnabled(getenv) {
		slog.Info("Abruf externer Statusseiten deaktiviert", "env", PollingEnabledEnv)
		return func() {}
	}
	slog.Info("Abruf externer Statusseiten gestartet")
	return s.Start(ctx)
}

// Start starts the background loop: one tick immediately, then every
// TickInterval. The returned function stops it, waits for it and
// releases the DB lock.
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

// RunOnce is one tick: acquire the lock, fetch due active sources.
// Sources of deleted status pages are left out.
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
			s.poll(ctx, src) // evaluates the outage after the fetch
		} else {
			s.evaluateOutage(ctx, src.ID)
		}
	}
	s.maybePrune(ctx)
}

// pruneState removes the state of deleted or deactivated sources.
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

// isDue: without state (first tick after startup) LastFetchAt counts, so
// a restart does not immediately query the vendors again.
func (s *Service) isDue(src models.ExternalSource, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.state[src.ID]; ok {
		return !now.Before(st.next)
	}
	return src.LastFetchAt == nil || !now.Before(src.LastFetchAt.Add(PollInterval(src)))
}

// FetchNow fetches a source immediately ("Fetch now" in the admin), even if
// it is inactive, and reschedules the next fetch.
func (s *Service) FetchNow(ctx context.Context, src models.ExternalSource) FetchResult {
	return s.poll(ctx, src)
}

// errUnknownProvider: source with a provider for which there is no adapter.
var errUnknownProvider = errors.New("unbekannter Provider")

// poll fetches a source, applies the result and schedules the next fetch.
//
// At most one fetch runs per source. Within the process, inflight prevents
// this; across instances, the source's advisory lock does (same mechanism
// as the leader lock of the background fetch, just with its own key per
// source). If a fetch is already running, ErrFetchInProgress is returned
// immediately instead of waiting: a second fetch right after would bring
// nothing new (the third-party site caches anyway), and the request would not
// hang for up to 10 s. The background tick skips the source and tries again
// on the next tick.
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
	// Incidents and maintenances; an error there lets the fetch succeed.
	s.syncIncidents(ctx, src.ID, now)
	s.evaluateOutage(ctx, src.ID)
	return res
}

func (s *Service) busyResult() FetchResult {
	return FetchResult{FetchedAt: s.now(), Error: ErrFetchInProgress}
}

// beginFetch marks the source as in progress; false if it already is.
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

// schedule plans the next fetch: after an error with a growing pause
// (backoff), otherwise at the normal interval. Returns the backoff pause.
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

// apply applies a 200 fetch: upsert and validators in one
// transaction, then events for changed effective statuses.
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

// recordFetch stores the result on the Source record. An error while
// saving is only logged: the next tick tries again anyway.
func (s *Service) recordFetch(id uint, now time.Time, ok bool, errText string) {
	updates := map[string]any{"last_fetch_at": now, "last_fetch_ok": ok, "last_error": errText}
	if ok {
		updates["last_success_at"] = now
	}
	if err := s.db.Model(&models.ExternalSource{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.Error("Externe Statusseite: Abrufstatus nicht gespeichert", "source_id", id, "error", err)
	}
}

// truncate shortens to at most n bytes without splitting a UTF-8 character
// (Postgres rejects invalid UTF-8).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
