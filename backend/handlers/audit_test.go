package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuditLogHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	auditAdmin := authGroup.Group("/admin/audit")
	auditAdmin.Use(middleware.RequireGlobalAdmin())
	auditAdmin.GET("", h.GetAuditLogsHandler)

	adminUser := models.User{Email: "audit-admin@example.com", IsGlobalAdmin: true}
	db.Create(&adminUser)
	adminToken, _ := middleware.GenerateToken(adminUser.ID, adminUser.Email, "", "", true)

	regularUser := models.User{Email: "audit-regular@example.com"}
	db.Create(&regularUser)
	regularToken, _ := middleware.GenerateToken(regularUser.ID, regularUser.Email, "", "", false)

	db.Create(&models.AuditLog{Action: "incident.created", Category: "incident", ActorEmail: "a@example.com", TargetName: "INC-1", IP: "1.1.1.1"})
	db.Create(&models.AuditLog{Action: "user.created", Category: "user", ActorEmail: "b@example.com", TargetName: "New User", IP: "2.2.2.2"})
	db.Create(&models.AuditLog{Action: "auth.login_failed", Category: "auth", ActorEmail: "attacker@evil.com", TargetName: "victim@example.com", IP: "3.3.3.3"})

	t.Run("No auth -> 401", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/audit", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Non-admin -> 403", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/audit", nil)
		req.Header.Set("Authorization", "Bearer "+regularToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("Global admin lists all entries", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/audit", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"total":3`)
	})

	t.Run("Category filter", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/audit?category=auth", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"total":1`)
		assert.Contains(t, w.Body.String(), "auth.login_failed")
	})

	t.Run("Case-insensitive search across actor/target/ip", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/audit?q=VICTIM@example.com", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"total":1`)
	})

	t.Run("Pagination limit", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/audit?limit=2&page=1", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"total":3`)
		assert.Contains(t, w.Body.String(), `"limit":2`)
	})

	// Filter für die Kategorien Abonnent, Ankündigung, Wartung, System.
	t.Run("Filter per neue Kategorie liefert nur deren Einträge", func(t *testing.T) {
		for cat, action := range map[string]string{
			"subscriber": "subscriber.deleted", "notice": "notice.created",
			"maintenance": "maintenance.window_started", "system": "selfmonitor.component_degraded",
		} {
			db.Create(&models.AuditLog{Action: action, Category: cat, ActorEmail: "c@example.com", IP: "4.4.4.4"})
			req, _ := http.NewRequest("GET", "/api/admin/audit?category="+cat, nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, cat)
			assert.Contains(t, w.Body.String(), `"total":1`, cat)
			assert.Contains(t, w.Body.String(), action, cat)
		}
	})

	t.Run("Unbekannte Kategorie -> 400 invalid_category", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/audit?category=billing", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_category")
	})
}

// Fehlgeschlagene Logins und der Honeypot der Bestellung speichern
// Nutzereingaben im Audit-Log. Sie dürfen dort nicht unbegrenzt landen.
func TestAudit_UntrustedInputStoredBounded(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)
	r.POST("/api/public/orders", h.CreateOrderHandler)

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	huge := strings.Repeat("a", 1<<20)

	w := post("/api/login", `{"email":"`+huge+`@example.com","password":"egal-egal"}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var login models.AuditLog
	assert.NoError(t, db.Where("action = ?", "auth.login_failed").First(&login).Error)
	assert.Len(t, login.TargetName, 254)

	w = post("/api/public/orders", `{"company_name":"X","email":"`+huge+`@example.com","website":"bot"}`)
	assert.Equal(t, http.StatusCreated, w.Code, "Honeypot antwortet unverändert")
	var honey models.AuditLog
	assert.NoError(t, db.Where("action = ?", "pageorder.honeypot_triggered").First(&honey).Error)
	assert.Len(t, honey.TargetName, 254)

	// Gegenprobe: eine normale Adresse wird unverändert gespeichert.
	post("/api/login", `{"email":"normal@example.com","password":"egal-egal"}`)
	var normal models.AuditLog
	assert.NoError(t, db.Where("action = ? AND target_name = ?", "auth.login_failed", "normal@example.com").First(&normal).Error)
}

// Eine Adresse über der RFC-Grenze kann kein Konto haben: Der Login beantwortet
// sie wie jede unbekannte Adresse, ohne die Konten-Tabelle abzufragen (kein
// Suchlauf mit Megabyte-Parametern).
func TestLogin_OversizedEmailDoesNotQueryUsers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)

	userQueries := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:count_user_queries", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			userQueries++
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove("test:count_user_queries") })

	post := func(email string) int {
		body := `{"email":"` + email + `","password":"egal-egal-egal"}`
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusUnauthorized, post(strings.Repeat("a", 300)+"@example.com"))
	assert.Zero(t, userQueries, "überlange Adresse: keine Kontensuche")

	// Gegenprobe: eine normale unbekannte Adresse wird gesucht.
	assert.Equal(t, http.StatusUnauthorized, post("unbekannt@example.com"))
	assert.Positive(t, userQueries)
}
