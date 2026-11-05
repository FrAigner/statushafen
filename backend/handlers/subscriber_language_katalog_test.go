package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Rest: Sprache im Admin-Formular,
// Fallback bei deaktivierter Sprache, Mails an interne Nutzer in deren
// UI-Sprache.

func TestSubscriberSprache_DeaktivierteKatalogspracheWirdNichtGespeichert(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	setPageDefaultLanguage(t, db, page.ID, "de")
	addPageLanguage(t, db, page.ID, "en")
	require.NoError(t, db.Model(&models.Language{}).Where("code = ?", "en").Update("is_active", false).Error)

	w := postJSON(r, "/api/subscribe", map[string]string{
		"channel": "email", "email": "l703-inactive@example.com", "component_ids": "all", "slug": "doi-page", "language": "en",
	}, nil)
	requireStatusCode(t, w, http.StatusOK)
	var s models.Subscriber
	require.NoError(t, db.Where("email = ?", "l703-inactive@example.com").First(&s).Error)
	assert.Equal(t, "", s.Language)
}

func TestSubscriberSprache_NachtraeglichDeaktiviert_VersandInSeitenstandard(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "l703-deact", "l703-admin@example.com")
	setPageDefaultLanguage(t, db, page.ID, "de")
	addPageLanguage(t, db, page.ID, "en")
	addLangSubscriber(t, db, page.ID, "l703-deact@example.com", "en")
	// Die Sprache wird nach dem Abo bei der Seite abgewählt.
	require.NoError(t, db.Where("status_page_id = ? AND language_code = ?", page.ID, "en").Delete(&models.StatusPageLanguage{}).Error)
	inc := seedIncident(t, db, page.ID, "Latenz", "Wir prüfen.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessageTo(t, srv, "l703-deact@example.com")
	assert.Contains(t, msg, "Status-Update für", "Rückfall auf die Seitenstandardsprache")
}

// --- Admin-Formular -----------------------------------------------------------

func newAdminSubRouter(db *gorm.DB) http.Handler {
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/pages/:page_id/subscribers", h.CreateSubscriberAdminHandler)
	r.PUT("/api/pages/:page_id/subscribers/:id", h.UpdateSubscriberHandler)
	return r
}

func adminSubCall(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminAbonnent_AnlegenUndAendernMitSprache(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "P", Slug: "l703-admin"}
	require.NoError(t, db.Create(&page).Error)
	other := models.StatusPage{Name: "O", Slug: "l703-admin-other"}
	require.NoError(t, db.Create(&other).Error)
	setPageDefaultLanguage(t, db, page.ID, "de")
	addPageLanguage(t, db, page.ID, "en")
	addPageLanguage(t, db, other.ID, "fr")
	r := newAdminSubRouter(db)
	base := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/subscribers"

	lang := func(email string) string {
		var s models.Subscriber
		require.NoError(t, db.Where("email = ?", email).First(&s).Error)
		return s.Language
	}

	w := adminSubCall(r, http.MethodPost, base, `{"Channel":"email","Email":"l703-a@example.com","SubscribedComponentIDs":"all","Language":" EN "}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, "en", lang("l703-a@example.com"))

	// Sprache einer anderen Seite / keine Angabe: Seitenstandard (leer).
	w = adminSubCall(r, http.MethodPost, base, `{"Channel":"email","Email":"l703-b@example.com","SubscribedComponentIDs":"all","Language":"fr"}`)
	require.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "", lang("l703-b@example.com"), "fr gehört nur zur anderen Seite")
	w = adminSubCall(r, http.MethodPost, base, `{"Channel":"email","Email":"l703-c@example.com","SubscribedComponentIDs":"all"}`)
	require.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "", lang("l703-c@example.com"))

	var a models.Subscriber
	require.NoError(t, db.Where("email = ?", "l703-a@example.com").First(&a).Error)
	upd := base + "/" + strconv.Itoa(int(a.ID))
	body := func(language string) string {
		return `{"Channel":"email","Email":"l703-a@example.com","SubscribedComponentIDs":"all"` + language + `}`
	}
	w = adminSubCall(r, http.MethodPut, upd, body(""))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "en", lang("l703-a@example.com"), "ohne Feld bleibt die Sprache")
	w = adminSubCall(r, http.MethodPut, upd, body(`,"Language":"fr"`))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", lang("l703-a@example.com"), "unbekannt = Seitenstandard")
	w = adminSubCall(r, http.MethodPut, upd, body(`,"Language":"de"`))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "de", lang("l703-a@example.com"))

	// Mandantentrennung: ein Abo einer anderen Seite ist über diese Seite nicht änderbar.
	foreign := models.Subscriber{StatusPageID: other.ID, Email: "l703-x@example.com", Channel: "email", SubscribedComponentIDs: "all"}
	require.NoError(t, db.Create(&foreign).Error)
	w = adminSubCall(r, http.MethodPut, base+"/"+strconv.Itoa(int(foreign.ID)), `{"Channel":"email","Email":"l703-x@example.com","SubscribedComponentIDs":"all","Language":"en"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)
	var f models.Subscriber
	require.NoError(t, db.First(&f, foreign.ID).Error)
	assert.Equal(t, "", f.Language)
}

// Das Admin-Formular beschriftet die Sprachen mit dem Namen aus dem Katalog;
// die Liste der Seitensprachen liefert ihn deshalb mit.
func TestPageLanguagesList_LiefertSprachnamen(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/languages", h.GetPageLanguagesHandler)
	page := models.StatusPage{Name: "P", Slug: "l703-names"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.Language{Code: "de", Name: "Deutsch", IsActive: true}).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true}).Error)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/pages/"+strconv.Itoa(int(page.ID))+"/languages", nil))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"Name":"Deutsch"`)

	// DB-Fehler -> 500 mit Code
	require.NoError(t, db.Migrator().DropTable(&models.StatusPageLanguage{}))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/pages/"+strconv.Itoa(int(page.ID))+"/languages", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "server_error")
}

// --- Interne Nutzer nach PreferredLanguage ---------------------------------------

// collectMailsByRecipient liest genau n Mails und ordnet sie dem Empfänger zu
// (die Reihenfolge der Versandschleife ist offen).
func collectMailsByRecipient(t *testing.T, srv *fakeSMTPServer, n int, recipients ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	deadline := time.After(5 * time.Second)
	for len(out) < n {
		select {
		case msg := <-srv.dataCh:
			for _, r := range recipients {
				if strings.Contains(msg, "To: "+r) {
					out[r] = msg
				}
			}
		case <-deadline:
			t.Fatalf("nur %d von %d Mails erhalten: %v", len(out), n, recipients)
		}
	}
	return out
}

func TestInterneNutzer_VorfallMailInIhrerSprache(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "l703-staff", "l703-staff-admin@example.com")
	require.NoError(t, db.Create(&models.User{Email: "l703-de@example.com", IsGlobalAdmin: true, PreferredLanguage: "de"}).Error)
	require.NoError(t, db.Create(&models.User{Email: "l703-en@example.com", IsGlobalAdmin: true, PreferredLanguage: "en"}).Error)
	inc := seedIncident(t, db, page.ID, "Latenz", "Wir prüfen.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	got := collectMailsByRecipient(t, srv, 3, "l703-de@example.com", "l703-en@example.com", "l703-staff-admin@example.com")
	assert.Contains(t, got["l703-de@example.com"], "Status-Update für")
	assert.NotContains(t, got["l703-de@example.com"], "Status update for")
	assert.Contains(t, got["l703-en@example.com"], "Status update for")
	// Ohne gespeicherte Sprache bleibt es beim bisherigen Englisch.
	assert.Contains(t, got["l703-staff-admin@example.com"], "Status update for")
}

func TestInterneNutzer_VorlageNachSpracheMitRueckfallAufEnglisch(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "l703-stafftpl", "l703-stafftpl-admin@example.com")
	require.NoError(t, db.Create(&models.User{Email: "l703-tpl-de@example.com", IsGlobalAdmin: true, PreferredLanguage: "de"}).Error)
	// Nur die englische Staff-Vorlage gepflegt: gilt auch für den deutschen Nutzer (wie vor der Umstellung).
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceStaff, "en", "STAFF-EN {{.Language}}", "<p>X</p>")
	inc := seedIncident(t, db, page.ID, "Latenz", "Wir prüfen.", "investigating")
	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")
	got := collectMailsByRecipient(t, srv, 2, "l703-tpl-de@example.com", "l703-stafftpl-admin@example.com")
	assert.Contains(t, got["l703-tpl-de@example.com"], "Subject: STAFF-EN en")

	// Jetzt eine deutsche Staff-Vorlage: der deutsche Nutzer bekommt sie, die anderen die englische.
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceStaff, "de", "STAFF-DE {{.Language}}", "<p>Y</p>")
	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")
	got = collectMailsByRecipient(t, srv, 2, "l703-tpl-de@example.com", "l703-stafftpl-admin@example.com")
	assert.Contains(t, got["l703-tpl-de@example.com"], "Subject: STAFF-DE de")
	assert.Contains(t, got["l703-stafftpl-admin@example.com"], "Subject: STAFF-EN en")
}
