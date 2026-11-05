package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Das Abonnement speichert die gewünschte Mailsprache; Vorfälle,
// Ankündigungen, externe Meldungen und die Bestätigung gehen in dieser Sprache
// hinaus (leer = Standardsprache der Seite).

// addPageLanguage aktiviert eine weitere (Nicht-Standard-)Sprache an der Seite.
func addPageLanguage(t *testing.T, db *gorm.DB, pageID uint, code string) {
	t.Helper()
	require.NoError(t, db.Where("code = ?", code).FirstOrCreate(&models.Language{Code: code, Name: code, IsActive: true}).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: pageID, LanguageCode: code}).Error)
}

func addLangSubscriber(t *testing.T, db *gorm.DB, pageID uint, email, lang string) {
	t.Helper()
	require.NoError(t, db.Create(&models.Subscriber{StatusPageID: pageID, Email: email, Channel: "email", SubscribedComponentIDs: "all", Language: lang}).Error)
}

func TestSubscriberSprache_Vorfall_JederBekommtSeineSprache(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "sl668-inc", "sl668-admin@example.com")
	setPageDefaultLanguage(t, db, page.ID, "de")
	addPageLanguage(t, db, page.ID, "en")
	addLangSubscriber(t, db, page.ID, "sl668-en@example.com", "en")
	addLangSubscriber(t, db, page.ID, "sl668-de@example.com", "")
	inc := seedIncident(t, db, page.ID, "Latency", "Looking.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	en := awaitSMTPMessageTo(t, srv, "sl668-en@example.com")
	assert.Contains(t, en, "Status update for")
	assert.NotContains(t, en, "Status-Update für")
	de := awaitSMTPMessageTo(t, srv, "sl668-de@example.com")
	assert.Contains(t, de, "Status-Update für", "ohne Angabe: Seitenstandardsprache")
}

func TestSubscriberSprache_Vorfall_VorlageDerAbonnentensprache(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "sl668-tpl", "sl668-admin2@example.com")
	setPageDefaultLanguage(t, db, page.ID, "de")
	addPageLanguage(t, db, page.ID, "en")
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceSubscriber, "de", "DE-BETREFF", "<p>DE-TEXT</p>")
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceSubscriber, "en", "EN-SUBJECT {{.Language}}", "<p>EN-TEXT</p>")
	addLangSubscriber(t, db, page.ID, "sl668-tpl-en@example.com", "en")
	inc := seedIncident(t, db, page.ID, "Latency", "Looking.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessageTo(t, srv, "sl668-tpl-en@example.com")
	assert.Contains(t, msg, "Subject: EN-SUBJECT en")
	assert.Contains(t, msg, "EN-TEXT")
	assert.NotContains(t, msg, "DE-TEXT")
}

func TestSubscriberSprache_Ankuendigung_JederBekommtSeineSprache(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "sl668-notice")
	setPageDefaultLanguage(t, h.DB, page.ID, "de")
	addPageLanguage(t, h.DB, page.ID, "en")
	addLangSubscriber(t, h.DB, page.ID, "sl668-notice-en@example.com", "en")

	h.DispatchNoticeNotifications(noticeFor(page.ID), "created")

	// Reihenfolge der beiden Mails ist offen: nach Empfänger einsortieren.
	for range 2 {
		msg := waitForEmail(t, srv)
		if strings.Contains(msg, "To: sl668-notice-en@example.com") {
			assert.Contains(t, msg, "View status page")
			assert.NotContains(t, msg, "Statusseite ansehen")
		} else {
			assert.Contains(t, msg, "Statusseite ansehen")
		}
	}
}

func TestSubscriberSprache_Extern_VorlageDerAbonnentensprache(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	setPageDefaultLanguage(t, s.db, s.page.ID, "de")
	addPageLanguage(t, s.db, s.page.ID, "fr")
	for _, lang := range []string{"de", "fr"} {
		seedTemplate(t, s.db, s.page.ID, "external_incident_created", handlers.NotificationAudienceSubscriber, lang, "SUJET-"+lang, "<p>CORPS-"+lang+"</p>")
	}
	addLangSubscriber(t, s.db, s.page.ID, "sl668-ext-fr@example.com", "fr")

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "Wir schauen"))
	s.poll()
	m := drainMails(t, srv, 1)[0]
	assert.Contains(t, m.body, "CORPS-fr")
	assert.NotContains(t, m.body, "CORPS-de")
}

func TestSubscriberSprache_Bestaetigung_InDerGewaehltenSprache(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	setPageDefaultLanguage(t, db, page.ID, "de")
	addPageLanguage(t, db, page.ID, "en")

	w := postJSON(r, "/api/subscribe", map[string]string{
		"channel": "email", "email": "sl668-confirm@example.com", "component_ids": "all", "slug": "doi-page", "language": "en",
	}, nil)
	requireStatusCode(t, w, http.StatusOK)

	msg := awaitSMTPMessage(t, srv)
	assert.Contains(t, msg, "Subject: Please confirm your subscription for DOI Page")
	assert.NotContains(t, msg, "Bitte bestätige")
}

func TestSubscriberSprache_Abo_SpeichertNurAktiveSeitensprache(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	setPageDefaultLanguage(t, db, page.ID, "de")
	addPageLanguage(t, db, page.ID, "en")

	stored := func(email string) string {
		var s models.Subscriber
		require.NoError(t, db.Where("email = ?", email).First(&s).Error)
		return s.Language
	}
	for email, tc := range map[string][2]string{
		"sl668-a@example.com": {" EN ", "en"},          // normalisiert
		"sl668-b@example.com": {"fr", ""},              // bei der Seite nicht aktiv
		"sl668-c@example.com": {"", ""},                // keine Angabe
		"sl668-d@example.com": {"en-ZZ-unbekannt", ""}, // Müll ohne Steuerzeichen
	} {
		w := postJSON(r, "/api/subscribe", map[string]string{
			"channel": "email", "email": email, "component_ids": "all", "slug": "doi-page", "language": tc[0],
		}, nil)
		requireStatusCode(t, w, http.StatusOK)
		assert.Equal(t, tc[1], stored(email), "language=%q", tc[0])
	}

	// Steuerzeichen in der Sprache (Header-Einschleusung, NUL-Byte) werden
	// nicht mehr stillschweigend verworfen, sondern mit 400 abgewiesen.
	w := postJSON(r, "/api/subscribe", map[string]string{
		"channel": "email", "email": "sl668-e@example.com", "component_ids": "all", "slug": "doi-page", "language": "en\r\nX:",
	}, nil)
	requireStatusCode(t, w, http.StatusBadRequest)
	var n int64
	db.Model(&models.Subscriber{}).Where("email = ?", "sl668-e@example.com").Count(&n)
	assert.Zero(t, n)
}

func TestSubscriberSprache_MeAbo_LesenUndAendern(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		if e := c.GetHeader("X-Mock-Email"); e != "" {
			c.Set("email", e)
		}
		c.Next()
	})
	r.GET("/api/me/subscription", h.GetMySubscriptionHandler)
	r.PUT("/api/me/subscription", h.UpdateMySubscriptionHandler)
	page := models.StatusPage{Name: "P", Slug: "sl668-me"}
	require.NoError(t, db.Create(&page).Error)
	setPageDefaultLanguage(t, db, page.ID, "de")
	addPageLanguage(t, db, page.ID, "en")
	addLangSubscriber(t, db, page.ID, "sl668-me@example.com", "")

	call := func(method, body string) map[string]any {
		req := httptest.NewRequest(method, "/api/me/subscription?slug=sl668-me", strings.NewReader(body))
		req.Header.Set("X-Mock-Email", "sl668-me@example.com")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}

	assert.Equal(t, "", call(http.MethodGet, "")["language"])
	call(http.MethodPut, `{"component_ids":"all","language":"en"}`)
	assert.Equal(t, "en", call(http.MethodGet, "")["language"])
	call(http.MethodPut, `{"component_ids":"all"}`)
	assert.Equal(t, "en", call(http.MethodGet, "")["language"], "ohne Feld bleibt die Sprache")
	call(http.MethodPut, `{"component_ids":"all","language":"xx"}`)
	assert.Equal(t, "", call(http.MethodGet, "")["language"], "unbekannt = Seitenstandard")
}
