package handlers_test

import (
	"bytes"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Bestätigungsmails werden asynchron versendet: Die Antwortzeit darf weder vom
// SMTP-Server abhängen (hängender Server) noch verraten, ob eine Adresse neu
// oder schon eingetragen ist (Mail ja/nein).

// startHangingSMTP nimmt Verbindungen an und sendet nie ein Banner. Alle
// Die Verbindungen schließt die zurückgegebene Funktion (spätestens beim Testende),
// damit hängende Versand-Goroutinen enden.
func startHangingSMTP(t *testing.T) (release func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	release = func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	}
	host, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	t.Setenv("SMTP_HOST", host)
	t.Setenv("SMTP_PORT", port)
	t.Cleanup(release)
	return release
}

type asyncMailEnv struct {
	db   *gorm.DB
	h    *handlers.Handler
	r    *gin.Engine
	page models.StatusPage
}

// setupAsyncMailEnv baut Router und Handler. Beim Testende werden zuerst die
// hängenden SMTP-Verbindungen (release, darf nil sein) geschlossen, dann wird
// auf den Versand gewartet, bevor die SMTP-Umgebung zurückgesetzt wird.
func setupAsyncMailEnv(t *testing.T, release func()) *asyncMailEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	t.Cleanup(func() {
		if release != nil {
			release()
		}
		h.WaitMails()
	})
	r := testutils.SetupRouter()
	r.POST("/api/subscribe", h.SubscribeHandler)
	r.POST("/api/pages/:page_id/subscribers", h.CreateSubscriberAdminHandler)
	page := models.StatusPage{Name: "Async Page", Slug: "doi-page"}
	require.NoError(t, db.Create(&page).Error)
	return &asyncMailEnv{db: db, h: h, r: r, page: page}
}

func timed(f func()) time.Duration {
	start := time.Now()
	f()
	return time.Since(start)
}

func TestSubscribe_AntwortOhneWartenAufHaengendenSMTP(t *testing.T) {
	e := setupAsyncMailEnv(t, startHangingSMTP(t))
	var code int
	d := timed(func() { code = subscribeEmail(e.r, "haengt@example.com", nil).Code })
	assert.Equal(t, http.StatusOK, code)
	assert.Less(t, d, 2*time.Second, "die Antwort wartet nicht auf den SMTP-Server (Dial 5 s, Gesamtfrist 15 s)")
}

func TestCreateSubscriberAdmin_AntwortOhneWartenAufHaengendenSMTP(t *testing.T) {
	e := setupAsyncMailEnv(t, startHangingSMTP(t))
	var code int
	d := timed(func() {
		code = postJSON(e.r, "/api/pages/"+itoa(e.page.ID)+"/subscribers",
			map[string]any{"channel": "email", "email": "admin-angelegt@example.com", "component_ids": "all"}, nil).Code
	})
	assert.Equal(t, http.StatusCreated, code)
	assert.Less(t, d, 2*time.Second)
}

// Robuste Relation statt enger Schwelle: Mit hängendem SMTP-Server dauert eine
// neue Adresse (Mail wird angestoßen) nicht merklich länger als eine bereits
// aktive (keine Mail). Wäre der Versand synchron, läge der Unterschied bei Sekunden.
func TestSubscribe_AntwortzeitNeuGegenBekanntOhneUnterschied(t *testing.T) {
	e := setupAsyncMailEnv(t, startHangingSMTP(t))
	require.NoError(t, e.db.Create(&models.Subscriber{StatusPageID: e.page.ID, Channel: "email", Email: "aktiv@example.com", Status: models.SubscriberStatusActive}).Error)

	const n = 5
	var neu, bekannt []time.Duration
	for i := 0; i < n; i++ {
		email := "neu" + itoa(uint(i)) + "@example.com"
		neu = append(neu, timed(func() { assert.Equal(t, http.StatusOK, subscribeEmail(e.r, email, nil).Code) }))
		bekannt = append(bekannt, timed(func() { assert.Equal(t, http.StatusOK, subscribeEmail(e.r, "aktiv@example.com", nil).Code) }))
	}
	median := func(d []time.Duration) time.Duration {
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		return d[len(d)/2]
	}
	assert.Less(t, median(neu), median(bekannt)+500*time.Millisecond,
		"neu %v gegen bekannt %v: kein Zeitunterschied durch den Mailversand", median(neu), median(bekannt))
}

// captureSlog leitet slog in einen Puffer um und stellt den Standard beim Testende wieder her.
func captureSlog(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Gegenprobe zum hängenden Server: Die Mail wird im Hintergrund tatsächlich zugestellt.
func TestSubscribe_BestaetigungsmailWirdImHintergrundZugestellt(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	e := setupAsyncMailEnv(t, nil)

	require.Equal(t, http.StatusOK, subscribeEmail(e.r, "zustellung@example.com", nil).Code)
	msg := awaitSMTPMessage(t, srv)
	assert.Contains(t, msg, "To: zustellung@example.com")
	assert.Regexp(t, confirmLinkRe, msg)
	e.h.WaitMails()
}

func TestCreateSubscriberAdmin_BestaetigungsmailWirdZugestellt(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	e := setupAsyncMailEnv(t, nil)

	w := postJSON(e.r, "/api/pages/"+itoa(e.page.ID)+"/subscribers",
		map[string]any{"channel": "email", "email": "admin-zustellung@example.com", "component_ids": "all"}, nil)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Contains(t, awaitSMTPMessage(t, srv), "To: admin-zustellung@example.com")
	e.h.WaitMails()
}

// Ist die Warteschlange voll, wird die Mail verworfen und geloggt; die Anfrage
// blockiert nicht und das Log nennt weder Adresse noch sonstige Eingaben.
func TestSubscribe_VolleWarteschlangeBlockiertNichtUndLoggtOhneEingaben(t *testing.T) {
	logBuf := captureSlog(t)
	release := startHangingSMTP(t)
	e := setupAsyncMailEnv(t, release)
	e.h.SetMailQueueSize(1)

	for i := 0; i < 4; i++ {
		email := "geheim-" + itoa(uint(i)) + "-eingabe@example.com"
		var code int
		d := timed(func() { code = subscribeEmail(e.r, email, nil).Code })
		assert.Equal(t, http.StatusOK, code)
		assert.Less(t, d, 2*time.Second, "auch bei voller Warteschlange keine Blockade")
	}
	assert.Equal(t, 3, strings.Count(logBuf.String(), "Mail queue full, mail dropped"), "ein Platz belegt, die übrigen drei verworfen")
	assert.NotContains(t, logBuf.String(), "geheim-")
	assert.NotContains(t, logBuf.String(), "example.com", "das Verwerfen loggt keine Adresse")

	release()
	e.h.WaitMails()
	var subs int64
	e.db.Model(&models.Subscriber{}).Count(&subs)
	assert.EqualValues(t, 4, subs, "verworfene Mails lassen die Abos wartend bestehen")
	var failed int64
	e.db.Model(&models.AuditLog{}).Where("action = ?", "subscriber.confirmation_email_failed").Count(&failed)
	assert.EqualValues(t, 1, failed, "nur die eine angenommene Mail wurde versucht")
}
