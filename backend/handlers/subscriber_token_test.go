package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
)

// TestUnsubscribeHandler: a Subscriber can remove themselves
// via a signed token, without logging in - previously the only way was a
// Statuspage-Admin manually deleting them via
// DELETE /api/pages/:page_id/subscribers/:id.
//
// Abbestellen läuft nur noch per POST (Token als
// JSON-Body von Unsubscribe.vue oder als ?token= aus dem RFC-8058-One-Click-
// Header List-Unsubscribe). GET verändert nichts mehr, siehe
// TestUnsubscribeRedirectHandler.
func TestUnsubscribeHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/public/subscribers/unsubscribe", h.UnsubscribeHandler)

	page := models.StatusPage{Name: "Unsub Page", Slug: "unsub-page"}
	db.Create(&page)

	newSub := func(email string) models.Subscriber {
		sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: email, SubscribedComponentIDs: "all"}
		db.Create(&sub)
		return sub
	}
	sub := newSub("sub@example.com")

	otherPage := models.StatusPage{Name: "Other Page", Slug: "other-page"}
	db.Create(&otherPage)
	otherSub := models.Subscriber{StatusPageID: otherPage.ID, Channel: "email", Email: "other@example.com", SubscribedComponentIDs: "all"}
	db.Create(&otherSub)

	postJSON := func(token string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest("POST", "/api/public/subscribers/unsubscribe", strings.NewReader(`{"token":"`+token+`"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("Invalid token is rejected", func(t *testing.T) {
		w := postJSON("not-a-real-token")
		assert.Equal(t, http.StatusBadRequest, w.Code)

		var stillThere models.Subscriber
		assert.NoError(t, db.First(&stillThere, sub.ID).Error)
	})

	t.Run("Missing token is rejected", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/public/subscribers/unsubscribe", strings.NewReader("not json"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Token for wrong status page is rejected", func(t *testing.T) {
		// Token claims a SubscriberID that exists, but under a different
		// StatusPageID than the one it actually belongs to - must not be
		// accepted as if it were valid for otherSub's real page.
		mismatched, err := middleware.GenerateSubscriberToken(sub.ID, otherPage.ID)
		assert.NoError(t, err)

		w := postJSON(mismatched)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		var stillThere models.Subscriber
		assert.NoError(t, db.First(&stillThere, sub.ID).Error)
	})

	t.Run("Valid token in JSON body removes the subscriber", func(t *testing.T) {
		validToken, err := middleware.GenerateSubscriberToken(sub.ID, page.ID)
		assert.NoError(t, err)

		w := postJSON(validToken)
		assert.Equal(t, http.StatusOK, w.Code)

		err = db.First(&models.Subscriber{}, sub.ID).Error
		assert.Error(t, err, "subscriber should have been deleted")

		// The unrelated subscriber on the other page must be unaffected.
		var untouched models.Subscriber
		assert.NoError(t, db.First(&untouched, otherSub.ID).Error)
	})

	t.Run("RFC-8058-One-Click: Token in der URL, Formular-Body", func(t *testing.T) {
		oneClick := newSub("oneclick@example.com")
		token, err := middleware.GenerateSubscriberToken(oneClick.ID, page.ID)
		assert.NoError(t, err)

		req, _ := http.NewRequest("POST", "/api/public/subscribers/unsubscribe?token="+url.QueryEscape(token), strings.NewReader("List-Unsubscribe=One-Click"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Error(t, db.First(&models.Subscriber{}, oneClick.ID).Error, "subscriber should have been deleted")
	})
}

// TestUnsubscribeRedirectHandler: GET auf den
// alten Abbestell-Endpunkt löschte das Abo direkt - Link-Scanner und
// Mail-Prefetcher beendeten so Abos ungewollt. Bereits versendete Links
// funktionieren weiter, führen aber nur noch auf die Frontend-Seite, die per
// Knopf POST auslöst.
func TestUnsubscribeRedirectHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/subscribers/unsubscribe", h.UnsubscribeRedirectHandler)
	t.Setenv("FRONTEND_URL", "https://status.example.test")

	page := models.StatusPage{Name: "Redirect Page", Slug: "redirect-page"}
	db.Create(&page)
	sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "redirect@example.com", SubscribedComponentIDs: "all"}
	db.Create(&sub)
	token, err := middleware.GenerateSubscriberToken(sub.ID, page.ID)
	assert.NoError(t, err)

	t.Run("gültiger Token: Weiterleitung, Abo bleibt bestehen", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/public/subscribers/unsubscribe?token="+url.QueryEscape(token), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "https://status.example.test/unsubscribe?token="+url.QueryEscape(token), w.Header().Get("Location"))
		assert.NoError(t, db.First(&models.Subscriber{}, sub.ID).Error, "GET darf das Abo nicht löschen")
	})

	t.Run("ohne Token: Weiterleitung auf die Seite ohne Query", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/public/subscribers/unsubscribe", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "https://status.example.test/unsubscribe", w.Header().Get("Location"))
	})
}

// TestPrivateStatusPage_SubscriberToken: a subscriber token
// scoped to a private StatusPage grants read access to that page's dashboard
// without any user login, but a token scoped to a DIFFERENT page (or none at
// all) does not.
func TestPrivateStatusPage_SubscriberToken(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	publicGroup := r.Group("/api/public")
	publicGroup.Use(middleware.OptionalAuthMiddleware())
	publicGroup.GET("/pages/:slug/dashboard", h.GetPublicDashboardHandler)

	privatePage := models.StatusPage{Name: "Private Token Page", Slug: "private-token-page", IsPrivate: true}
	db.Create(&privatePage)

	// Seit braucht das Token einen Ersteller, der die
	// private Seite selbst sehen darf.
	creator := models.User{Email: "creator@example.com", IsGlobalAdmin: true}
	db.Create(&creator)
	sub := models.Subscriber{StatusPageID: privatePage.ID, Channel: "email", Email: "sub@example.com", SubscribedComponentIDs: "all", CreatedByUserID: &creator.ID}
	db.Create(&sub)

	otherPrivatePage := models.StatusPage{Name: "Other Private Page", Slug: "other-private-page", IsPrivate: true}
	db.Create(&otherPrivatePage)

	t.Run("Valid subscriber token for this page grants access", func(t *testing.T) {
		token, err := middleware.GenerateSubscriberToken(sub.ID, privatePage.ID)
		assert.NoError(t, err)

		req, _ := http.NewRequest("GET", "/api/public/pages/private-token-page/dashboard?token="+token, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Subscriber token scoped to a different page is rejected", func(t *testing.T) {
		token, err := middleware.GenerateSubscriberToken(sub.ID, otherPrivatePage.ID)
		assert.NoError(t, err)

		req, _ := http.NewRequest("GET", "/api/public/pages/private-token-page/dashboard?token="+token, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "404 wie bei einer fehlenden Seite")
	})

	t.Run("Malformed token is rejected like no token at all", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/public/pages/private-token-page/dashboard?token=garbage", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "404 wie bei einer fehlenden Seite")
	})
}
