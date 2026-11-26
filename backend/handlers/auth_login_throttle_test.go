package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loginAttempt(r *gin.Engine, email, password string) *httptest.ResponseRecorder {
	return loginAttemptFrom(r, "192.0.2.1:1234", email, password)
}

// loginAttemptFrom sends the attempt from the given sender address (ip:port).
func loginAttemptFrom(r *gin.Engine, remoteAddr, email, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// The lockout applies per pair of account and source. After ten failed attempts the
// source is locked, and the lockout looks the same for known and unknown addresses.
// An attacker who only knows the address thus does not lock out
// the real user coming from a different source.
func TestLogin_ThrottleBlocksSourceButNotOwnerElsewhere(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)

	require.NoError(t, db.Create(&models.User{Email: "ziel@example.com", PasswordHash: testutils.MustHash("RichtigesPasswort1")}).Error)
	const attacker = "198.51.100.7:4000"

	for _, email := range []string{"ziel@example.com", "gibtsnicht@example.com"} {
		for i := 0; i < 10; i++ {
			w := loginAttemptFrom(r, attacker, email, "falsch-"+string(rune('a'+i)))
			require.Equal(t, http.StatusUnauthorized, w.Code, "%s Versuch %d", email, i+1)
		}
	}

	known := loginAttemptFrom(r, attacker, "ziel@example.com", "RichtigesPasswort1")
	unknown := loginAttemptFrom(r, attacker, "gibtsnicht@example.com", "egal-egal-1")
	for _, w := range []*httptest.ResponseRecorder{known, unknown} {
		assert.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"code":"rate_limited"`)
		assert.NotEmpty(t, w.Header().Get("Retry-After"))
	}
	assert.Equal(t, known.Body.String(), unknown.Body.String(), "Antwort verrät nicht, ob das Konto existiert")
	// The remaining time differs only by the elapsed time (with -race bcrypt takes about a second).
	for _, w := range []*httptest.ResponseRecorder{known, unknown} {
		secs, err := strconv.Atoi(w.Header().Get("Retry-After"))
		require.NoError(t, err)
		assert.InDelta(t, 30, secs, 30)
		assert.Positive(t, secs)
	}

	// The real user from a different source can sign in.
	assert.Equal(t, http.StatusOK, loginAttemptFrom(r, "203.0.113.20:5000", "ziel@example.com", "RichtigesPasswort1").Code)

	// Control check: a different account is untouched, and the same account is also
	// locked from the locked source after normalizing the spelling.
	require.NoError(t, db.Create(&models.User{Email: "anderes@example.com", PasswordHash: testutils.MustHash("RichtigesPasswort2")}).Error)
	assert.Equal(t, http.StatusOK, loginAttemptFrom(r, attacker, "anderes@example.com", "RichtigesPasswort2").Code)
	assert.Equal(t, http.StatusTooManyRequests, loginAttemptFrom(r, attacker, " ZIEL@example.com ", "RichtigesPasswort1").Code)
}

// 100 concurrent failed attempts from the same source against the same account: only the
// threshold gets through (checking and counting are one step).
func TestLogin_ParallelWrongPasswordsStopAtThreshold(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)
	require.NoError(t, db.Create(&models.User{Email: "ziel@example.com", PasswordHash: testutils.MustHash("RichtigesPasswort1")}).Error)

	var wg sync.WaitGroup
	var unauthorized, limited, other atomic.Int32
	start := make(chan struct{})
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			switch loginAttemptFrom(r, "198.51.100.7:4000", "ziel@example.com", "falsch-parallel").Code {
			case http.StatusUnauthorized:
				unauthorized.Add(1)
			case http.StatusTooManyRequests:
				limited.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	assert.Zero(t, other.Load())
	assert.LessOrEqual(t, int(unauthorized.Load()), 10, "höchstens die Schwelle kommt bis zur Passwortprüfung")
	assert.Equal(t, 100, int(unauthorized.Load()+limited.Load()))
}

func TestLogin_SuccessResetsPairCounter(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)
	require.NoError(t, db.Create(&models.User{Email: "reset@example.com", PasswordHash: testutils.MustHash("RichtigesPasswort1")}).Error)

	for round := 0; round < 3; round++ {
		for i := 0; i < 9; i++ {
			require.Equal(t, http.StatusUnauthorized, loginAttempt(r, "reset@example.com", "falsch").Code)
		}
		require.Equal(t, http.StatusOK, loginAttempt(r, "reset@example.com", "RichtigesPasswort1").Code, "Runde %d", round)
	}
}

// For an unknown account the same bcrypt comparison runs as for a known one, so the
// runtime does not reveal whether the address has an account. The threshold is
// generous: without the comparison the factor is above 100.
func TestLogin_TimingDoesNotRevealAccountExistence(t *testing.T) {
	if testing.Short() {
		t.Skip("Zeitmessung")
	}
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)
	require.NoError(t, db.Create(&models.User{Email: "bekannt@example.com", PasswordHash: testutils.MustHash("RichtigesPasswort1")}).Error)
	// SSO account without a password hash.
	require.NoError(t, db.Create(&models.User{Email: "sso-ohne-pw@example.com"}).Error)

	median := func(email string) time.Duration {
		var ds []time.Duration
		for i := 0; i < 5; i++ {
			start := time.Now()
			w := loginAttempt(r, email, "falsches-Passwort")
			ds = append(ds, time.Since(start))
			require.Equal(t, http.StatusUnauthorized, w.Code)
		}
		sort.Slice(ds, func(a, b int) bool { return ds[a] < ds[b] })
		return ds[len(ds)/2]
	}
	known := median("bekannt@example.com")
	for _, email := range []string{"gibtsnicht@example.com", "sso-ohne-pw@example.com"} {
		got := median(email)
		assert.Greater(t, got, known/2, "%s: Laufzeit %v zu weit unter der eines bekannten Kontos (%v)", email, got, known)
	}
}
