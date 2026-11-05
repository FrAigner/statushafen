package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die Google-Chat-Texte folgen der Sprache des Abonnenten (leer =
// Standardsprache der Seite); der globale Webhook bleibt englisch.

func captureWebhookServer(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	var mu sync.Mutex
	var text string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]string
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		text = body["text"]
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		return text
	}
}

func chatLangSetup(t *testing.T) (*Handler, notificationDispatchContext) {
	t.Helper()
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "P", Slug: "chat-lang"}
	require.NoError(t, db.Create(&page).Error)
	for _, l := range []models.Language{{Code: "de", Name: "Deutsch", IsActive: true}, {Code: "en", Name: "English", IsActive: true}} {
		require.NoError(t, db.Create(&l).Error)
	}
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true}).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"}).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "Latenz", Message: "Wir prüfen.", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	ctx := h.buildNotificationDispatchContext(inc, "CREATED", "API", nil)
	return h, ctx
}

func TestGoogleChat_TextInSpracheDesAbonnenten(t *testing.T) {
	newWebhookEnv(t)
	h, ctx := chatLangSetup(t)
	srv, got := captureWebhookServer(t)

	h.notifySubscriberGoogleChat(ctx, models.Subscriber{WebhookURL: srv.URL, Language: "en"})
	assert.Contains(t, got(), "**Component:** API")
	assert.Contains(t, got(), "**Status:** investigating")
	assert.NotContains(t, got(), "Komponente")

	// Leer = Seitenstandardsprache (de): deutsche Beschriftungen und Statuslabel.
	h.notifySubscriberGoogleChat(ctx, models.Subscriber{WebhookURL: srv.URL})
	assert.Contains(t, got(), "**Komponente:** API")
	assert.Contains(t, got(), "**Vorfall:** Latenz")
	assert.Contains(t, got(), "**Status:** Untersuchung")
	assert.NotContains(t, got(), "Component")

	// Unbekannte/abgewählte Sprache: ebenfalls Seitenstandard.
	h.notifySubscriberGoogleChat(ctx, models.Subscriber{WebhookURL: srv.URL, Language: "xx"})
	assert.Contains(t, got(), "**Komponente:** API")
}

func TestGoogleChat_GlobalerWebhookBleibtEnglisch(t *testing.T) {
	newWebhookEnv(t)
	_, ctx := chatLangSetup(t)
	srv, got := captureWebhookServer(t)
	t.Setenv("GOOGLE_CHAT_WEBHOOK_URL", srv.URL)

	dispatchGlobalGoogleChatWebhook(ctx)

	assert.Contains(t, got(), "**Component:** API")
}

// Review, Punkt 3: Die Chat-Sprache ist die effektive Sprache des
// Abonnenten, nicht die Sprache der zufällig gefundenen Mail-Vorlage. Abonnent
// wählt de, Seitenstandard ist en, es gibt nur eine englische Vorlage.
func TestGoogleChat_SpracheDesAbonnentenUnabhaengigVonDerVorlage(t *testing.T) {
	newWebhookEnv(t)
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "P", Slug: "chat-tpl"}
	require.NoError(t, db.Create(&page).Error)
	for _, l := range []models.Language{{Code: "de", Name: "Deutsch", IsActive: true}, {Code: "en", Name: "English", IsActive: true}} {
		require.NoError(t, db.Create(&l).Error)
	}
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en", IsDefault: true}).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de"}).Error)
	require.NoError(t, db.Create(&models.NotificationTemplate{StatusPageID: page.ID, Kind: NotificationKindIncidentCreated,
		Audience: NotificationAudienceSubscriber, Language: "en", Subject: "S", BodyHTML: "B"}).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "Latenz", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	ctx := h.buildNotificationDispatchContext(inc, "CREATED", "API", nil)
	srv, got := captureWebhookServer(t)

	h.notifySubscriberGoogleChat(ctx, models.Subscriber{WebhookURL: srv.URL, Language: "de"})
	assert.Contains(t, got(), "**Komponente:** API", "Abonnent wählt de")

	h.notifySubscriberGoogleChat(ctx, models.Subscriber{WebhookURL: srv.URL})
	assert.Contains(t, got(), "**Component:** API", "ohne Wahl: Seitenstandard en")

	// Externe Meldung: gleiche Regel über chatLang.
	m := &extIncidentMail{
		ev:        externalstatus.IncidentEvent{Kind: externalstatus.IncidentCreated, Source: models.ExternalSource{Name: "Q"}, Incident: models.ExternalIncident{Title: "T", Status: "in_progress"}},
		compNames: "API", message: "msg", chatLang: h.memoSubscriberChatLang(page.ID),
	}
	require.NoError(t, h.sendExternalIncidentWebhook(m, models.Subscriber{WebhookURL: srv.URL, Language: "de"}))
	assert.Contains(t, got(), "**Quelle:**")
	require.NoError(t, h.sendExternalIncidentWebhook(m, models.Subscriber{WebhookURL: srv.URL}))
	assert.Contains(t, got(), "**Source:**")
}

func TestStaffMailLang(t *testing.T) {
	for in, want := range map[string]string{"de": "de", "DE": "de", " de-AT ": "de", "en": "en", "": "en", "fr": "en", "xx\r\n": "en"} {
		assert.Equal(t, want, staffMailLang(in), "preferred=%q", in)
	}
}

func TestExternalWebhook_TextInSprache(t *testing.T) {
	newWebhookEnv(t)
	h, _ := chatLangSetup(t)
	srv, got := captureWebhookServer(t)
	m := &extIncidentMail{
		ev:        externalstatus.IncidentEvent{Kind: externalstatus.IncidentCreated, Source: models.ExternalSource{Name: "Quelle"}, Incident: models.ExternalIncident{Title: "T", Status: "in_progress"}},
		compNames: "API",
		message:   "msg",
		chatLang: func(preferred string) string {
			if preferred == "en" {
				return "en"
			}
			return "de"
		},
	}
	require.NoError(t, h.sendExternalIncidentWebhook(m, models.Subscriber{WebhookURL: srv.URL, Language: "en"}))
	assert.Contains(t, got(), "**Source:**")
	assert.Contains(t, got(), "[EXTERNAL]")
	require.NoError(t, h.sendExternalIncidentWebhook(m, models.Subscriber{WebhookURL: srv.URL}))
	assert.Contains(t, got(), "**Quelle:**")
	assert.Contains(t, got(), "[EXTERN]")
}
