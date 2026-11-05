package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Wer eine Seite verlassen hat (Rolle entzogen), darf die von ihm für Dritte
// angelegten Abonnenten weder über /api/me/subscription ändern oder löschen
// noch im Export /api/me/export sehen: created_by_user_id ist nur so lange ein
// Besitzrecht, wie der Ersteller noch eine Rolle auf der Seite hat. Das
// Selbst-Abo über die eigene E-Mail-Adresse bleibt davon unberührt.
func TestMeSubscription_EntzogenerAdminBehaeltKeinenZugriff(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		if v := c.GetHeader("X-Mock-User"); v != "" {
			id, _ := strconv.ParseUint(v, 10, 32)
			c.Set("userID", uint(id))
			c.Set("email", c.GetHeader("X-Mock-Email"))
		}
	})
	r.GET("/api/me/subscription", h.GetMySubscriptionHandler)
	r.PUT("/api/me/subscription", h.UpdateMySubscriptionHandler)
	r.DELETE("/api/me/subscription", h.DeleteMySubscriptionHandler)
	r.GET("/api/me/export", h.MeExportHandler)

	role := models.Role{Name: "Admin", Permissions: "subscribers:create"}
	require.NoError(t, db.Create(&role).Error)

	newPage := func(slug string) models.StatusPage {
		p := models.StatusPage{Name: slug, Slug: slug}
		require.NoError(t, db.Create(&p).Error)
		return p
	}
	newUser := func(email string, global bool) models.User {
		u := models.User{Email: email, IsGlobalAdmin: global}
		require.NoError(t, db.Create(&u).Error)
		return u
	}
	grant := func(u models.User, p models.StatusPage) {
		require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: p.ID, RoleID: role.ID}).Error)
	}
	revoke := func(u models.User, p models.StatusPage) {
		require.NoError(t, db.Where("user_id = ? AND status_page_id = ?", u.ID, p.ID).Delete(&models.UserStatusPageRole{}).Error)
	}
	call := func(method string, u models.User, slug, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/me/subscription?slug="+slug, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Mock-User", strconv.FormatUint(uint64(u.ID), 10))
		req.Header.Set("X-Mock-Email", u.Email)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	export := func(u models.User) string {
		req := httptest.NewRequest(http.MethodGet, "/api/me/export", nil)
		req.Header.Set("X-Mock-User", strconv.FormatUint(uint64(u.ID), 10))
		req.Header.Set("X-Mock-Email", u.Email)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var parsed struct {
			Subscriptions []struct{ Email string } `json:"subscriptions"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &parsed))
		var emails []string
		for _, s := range parsed.Subscriptions {
			emails = append(emails, s.Email)
		}
		return strings.Join(emails, ",")
	}

	t.Run("fremde Adresse: mit Rolle Zugriff, nach Entzug nicht mehr", func(t *testing.T) {
		page := newPage("exadmin-a")
		admin := newUser("exadmin-a@example.com", false)
		grant(admin, page)
		sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "kunde-a@example.org", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive, CreatedByUserID: &admin.ID}
		require.NoError(t, db.Create(&sub).Error)

		// Gegenprobe: solange die Rolle besteht, gilt das Besitzrecht.
		require.Equal(t, http.StatusOK, call(http.MethodGet, admin, page.Slug, "").Code)
		assert.Contains(t, export(admin), "kunde-a@example.org")

		revoke(admin, page)
		assert.Equal(t, http.StatusNotFound, call(http.MethodGet, admin, page.Slug, "").Code, "GET")
		assert.Equal(t, http.StatusNotFound, call(http.MethodPut, admin, page.Slug, `{"component_ids":"1"}`).Code, "PUT")
		assert.Equal(t, http.StatusNotFound, call(http.MethodDelete, admin, page.Slug, "").Code, "DELETE")
		assert.NotContains(t, export(admin), "kunde-a@example.org")

		var after models.Subscriber
		require.NoError(t, db.First(&after, sub.ID).Error, "Abo darf nicht gelöscht worden sein")
		assert.Equal(t, "all", after.SubscribedComponentIDs, "Abo darf nicht geändert worden sein")
	})

	t.Run("Webhook-Abo ohne E-Mail folgt derselben Regel", func(t *testing.T) {
		page := newPage("exadmin-b")
		admin := newUser("exadmin-b@example.com", false)
		grant(admin, page)
		sub := models.Subscriber{StatusPageID: page.ID, Channel: "google_chat", WebhookURL: "https://chat.googleapis.com/v1/spaces/x/messages?key=k&token=t", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive, CreatedByUserID: &admin.ID}
		require.NoError(t, db.Create(&sub).Error)
		require.Equal(t, http.StatusOK, call(http.MethodGet, admin, page.Slug, "").Code)
		revoke(admin, page)
		w := call(http.MethodGet, admin, page.Slug, "")
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NotContains(t, w.Body.String(), "chat.googleapis.com")
	})

	t.Run("Selbst-Abo über die eigene Adresse bleibt nach Rollenentzug erhalten", func(t *testing.T) {
		page := newPage("exadmin-c")
		admin := newUser("exadmin-c@example.com", false)
		grant(admin, page)
		sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: admin.Email, SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive, CreatedByUserID: &admin.ID}
		require.NoError(t, db.Create(&sub).Error)
		revoke(admin, page)
		assert.Equal(t, http.StatusOK, call(http.MethodGet, admin, page.Slug, "").Code)
		assert.Contains(t, export(admin), admin.Email)
	})

	t.Run("Global Admin ohne Seitenrolle behält den Zugriff", func(t *testing.T) {
		page := newPage("exadmin-d")
		ga := newUser("exadmin-d@example.com", true)
		sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "kunde-d@example.org", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive, CreatedByUserID: &ga.ID}
		require.NoError(t, db.Create(&sub).Error)
		assert.Equal(t, http.StatusOK, call(http.MethodGet, ga, page.Slug, "").Code)
		assert.Contains(t, export(ga), "kunde-d@example.org")
	})

	t.Run("Rolle auf einer anderen Seite genügt nicht", func(t *testing.T) {
		pageA := newPage("exadmin-e1")
		pageB := newPage("exadmin-e2")
		admin := newUser("exadmin-e@example.com", false)
		grant(admin, pageB)
		sub := models.Subscriber{StatusPageID: pageA.ID, Channel: "email", Email: "kunde-e@example.org", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive, CreatedByUserID: &admin.ID}
		require.NoError(t, db.Create(&sub).Error)
		assert.Equal(t, http.StatusNotFound, call(http.MethodGet, admin, pageA.Slug, "").Code)
		assert.NotContains(t, export(admin), "kunde-e@example.org")
	})
}
