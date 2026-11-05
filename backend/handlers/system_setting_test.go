package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Tests für die globalen Systemeinstellungen (Self-Service-
// Bestellung an/aus). Abgedeckt: Default "an" bei fehlendem Key, Umschalten
// per PUT (Anlegen + Aktualisieren), öffentlicher Flag-Endpunkt, Global-Admin-
// Gating und der 403-Pfad in CreateOrderHandler bei deaktivierter Bestellung.

// setupSettingsAdminRouter baut einen Router mit derselben Middleware-Kette wie
// main.go (RequireGlobalAdmin). isAdmin steuert, ob der injizierte Nutzer ein
// Global Admin ist - so lässt sich das Gating prüfen.
func setupSettingsAdminRouter(t *testing.T, db *gorm.DB, h *handlers.Handler, isAdmin bool) *gin.Engine {
	t.Helper()
	role := models.Role{Name: "Settings Role (Test)", Permissions: ""}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	user := models.User{Email: "settings-user@example.com", IsGlobalAdmin: isAdmin, RoleID: role.ID}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api")
	authed.Use(func(c *gin.Context) {
		c.Set("userID", user.ID)
		c.Set("email", user.Email)
		c.Next()
	})
	settingsAdmin := authed.Group("/settings")
	settingsAdmin.Use(middleware.RequireGlobalAdmin())
	settingsAdmin.GET("", h.GetSettingsHandler)
	settingsAdmin.PUT("", h.UpdateSettingsHandler)
	return r
}

func TestSettingsHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupSettingsAdminRouter(t, db, h, true)

	t.Run("GET defaults to orders_enabled=true when no setting exists", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/settings", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusOK)

		var resp map[string]bool
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["orders_enabled"] != true {
			t.Errorf("expected orders_enabled true by default, got %v", resp["orders_enabled"])
		}
	})

	t.Run("PUT disables orders, GET reflects it, PUT re-enables (create + update path)", func(t *testing.T) {
		w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"orders_enabled": false})
		requireStatusCode(t, w, http.StatusOK)
		requireBodyContains(t, w, `"orders_enabled":false`)

		// Ein Audit-Eintrag muss geschrieben worden sein.
		var auditCount int64
		db.Model(&models.AuditLog{}).Where("action = ?", "system_setting.updated").Count(&auditCount)
		if auditCount == 0 {
			t.Error("expected an audit log entry for the settings change")
		}

		w = doStatusPageRequest(r, "GET", "/api/settings", nil)
		requireBodyContains(t, w, `"orders_enabled":false`)

		// Zweites PUT trifft den Update-Zweig des Upserts.
		w = doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"orders_enabled": true})
		requireStatusCode(t, w, http.StatusOK)
		requireBodyContains(t, w, `"orders_enabled":true`)

		w = doStatusPageRequest(r, "GET", "/api/settings", nil)
		requireBodyContains(t, w, `"orders_enabled":true`)

		// Es darf nur eine Zeile für den Key existieren (Upsert, kein Duplikat).
		var rows int64
		db.Model(&models.SystemSetting{}).Where("key = ?", "orders.enabled").Count(&rows)
		if rows != 1 {
			t.Errorf("expected exactly one SystemSetting row, got %d", rows)
		}
	})

	t.Run("PUT with missing orders_enabled returns 400", func(t *testing.T) {
		w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("PUT with invalid JSON returns 400", func(t *testing.T) {
		req, _ := http.NewRequest("PUT", "/api/settings", nil)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusBadRequest)
	})
}

func TestUpdateSettingsHandler_SaveErrorReturns500(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupSettingsAdminRouter(t, db, h, true)

	// Tabelle droppen: der Upsert in setSystemSettingBool scheitert dann, der
	// Handler muss mit 500 antworten (deckt den Fehlerpfad ab).
	if err := db.Migrator().DropTable(&models.SystemSetting{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	w := doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"orders_enabled": false})
	requireStatusCode(t, w, http.StatusInternalServerError)
}

func TestSettingsHandlers_NonAdminForbidden(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupSettingsAdminRouter(t, db, h, false)

	w := doStatusPageRequest(r, "GET", "/api/settings", nil)
	requireStatusCode(t, w, http.StatusForbidden)

	w = doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"orders_enabled": false})
	requireStatusCode(t, w, http.StatusForbidden)
}

func TestGetPublicOrdersEnabledHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/orders/enabled", h.GetPublicOrdersEnabledHandler)

	t.Run("defaults to enabled=true", func(t *testing.T) {
		w := doStatusPageRequest(r, "GET", "/api/public/orders/enabled", nil)
		requireStatusCode(t, w, http.StatusOK)
		requireBodyContains(t, w, `"enabled":true`)
	})

	t.Run("reports enabled=false when explicitly disabled", func(t *testing.T) {
		if err := db.Create(&models.SystemSetting{Key: "orders.enabled", Value: "false"}).Error; err != nil {
			t.Fatalf("seed setting: %v", err)
		}
		w := doStatusPageRequest(r, "GET", "/api/public/orders/enabled", nil)
		requireBodyContains(t, w, `"enabled":false`)
	})
}

func TestCreateOrderHandler_DisabledReturns403(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupOrderRouter(h)

	// Default (kein Setting): Bestellung funktioniert weiterhin.
	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Enabled GmbH",
		"email":        "enabled@example.com",
	})
	requireStatusCode(t, w, http.StatusCreated)

	// Explizit deaktiviert: 403, und es entsteht keine neue Bestellung.
	if err := db.Create(&models.SystemSetting{Key: "orders.enabled", Value: "false"}).Error; err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	var before int64
	db.Model(&models.PageOrder{}).Count(&before)

	w = doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Disabled GmbH",
		"email":        "disabled@example.com",
	})
	requireStatusCode(t, w, http.StatusForbidden)

	var after int64
	db.Model(&models.PageOrder{}).Count(&after)
	if after != before {
		t.Errorf("expected no new PageOrder when orders are disabled, before=%d after=%d", before, after)
	}
}
