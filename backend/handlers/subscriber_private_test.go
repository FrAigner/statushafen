package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
)

// Ein anonymes Abo auf eine private Statusseite lieferte über
// das Subscriber-Token im Mail-Link dauerhaften Lesezugriff auf das
// Dashboard. Abos auf privaten Seiten sind nur noch für eingeloggte Nutzer
// mit Rolle auf der Seite (oder Global Admins) erlaubt, sonst 404 wie beim
// Dashboard - die Existenz der Seite wird nicht verraten.
func TestSubscribe_PrivatePage(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	// Simuliert OptionalAuthMiddleware: der Header steht für einen gültigen
	// Login des Users mit dieser ID.
	r.POST("/api/subscribe", func(c *gin.Context) {
		if uid := c.GetHeader("X-Test-User"); uid != "" {
			id, _ := strconv.ParseUint(uid, 10, 32)
			c.Set("userID", uint(id))
		}
		c.Next()
	}, h.SubscribeHandler)

	private := models.StatusPage{Name: "Intern", Slug: "intern-317", IsPrivate: true}
	db.Create(&private)
	public := models.StatusPage{Name: "Offen", Slug: "offen-317"}
	db.Create(&public)

	role := models.Role{Name: "Viewer 317", Permissions: "components:read"}
	db.Create(&role)
	member := models.User{Email: "member-317@example.com"}
	db.Create(&member)
	db.Create(&models.UserStatusPageRole{UserID: member.ID, StatusPageID: private.ID, RoleID: role.ID})
	outsider := models.User{Email: "outsider-317@example.com"}
	db.Create(&outsider)
	admin := models.User{Email: "admin-317@example.com", IsGlobalAdmin: true}
	db.Create(&admin)

	subscribe := func(email, slug string, userID uint) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]interface{}{
			"channel":       "email",
			"email":         email,
			"component_ids": "all",
			"slug":          slug,
		})
		req, _ := http.NewRequest("POST", "/api/subscribe", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		if userID != 0 {
			req.Header.Set("X-Test-User", strconv.FormatUint(uint64(userID), 10))
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	countSubs := func(email string) int64 {
		var n int64
		db.Model(&models.Subscriber{}).Where("email = ?", email).Count(&n)
		return n
	}

	t.Run("anonym auf privater Seite: 404, kein Abo", func(t *testing.T) {
		w := subscribe("aussen@example.com", "intern-317", 0)
		requireStatusCode(t, w, http.StatusNotFound)
		if n := countSubs("aussen@example.com"); n != 0 {
			t.Errorf("expected no subscriber row, got %d", n)
		}
	})

	t.Run("eingeloggt ohne Rolle auf privater Seite: 404, kein Abo, keine Rolle vergeben", func(t *testing.T) {
		w := subscribe("outsider-317@example.com", "intern-317", outsider.ID)
		requireStatusCode(t, w, http.StatusNotFound)
		if n := countSubs("outsider-317@example.com"); n != 0 {
			t.Errorf("expected no subscriber row, got %d", n)
		}
		var roles int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", outsider.ID).Count(&roles)
		if roles != 0 {
			t.Errorf("expected no auto-assigned role, got %d", roles)
		}
	})

	t.Run("unbekannter User im Token auf privater Seite: 404", func(t *testing.T) {
		w := subscribe("ghost-317@example.com", "intern-317", 999999)
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("eingeloggt mit Rolle auf privater Seite: 200", func(t *testing.T) {
		w := subscribe("member-317@example.com", "intern-317", member.ID)
		requireStatusCode(t, w, http.StatusOK)
		if n := countSubs("member-317@example.com"); n != 1 {
			t.Errorf("expected 1 subscriber row, got %d", n)
		}
	})

	t.Run("Global Admin auf privater Seite: 200", func(t *testing.T) {
		w := subscribe("admin-317@example.com", "intern-317", admin.ID)
		requireStatusCode(t, w, http.StatusOK)
	})

	t.Run("Gegenprobe: anonym auf öffentlicher Seite weiterhin 200", func(t *testing.T) {
		w := subscribe("aussen@example.com", "offen-317", 0)
		requireStatusCode(t, w, http.StatusOK)
	})
}
