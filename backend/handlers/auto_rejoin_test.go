package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
)

// checkPrivatePageAccess fragt UserStatusPageRole
// ohne Unscoped() ab - ein Soft-Delete durch RemoveStatusPageUserHandler
// (gezielte, korrekt auditierte Admin-Entscheidung) ist für diese Query nicht
// von "hatte nie eine Rolle" unterscheidbar. autoAssignDefaultRole vergibt die
// Rolle deshalb nach einem einfachen Seitenaufruf erneut, OHNE jeden
// Audit-Eintrag - eine explizite Entfernung wird stillschweigend rückgängig
// gemacht.
//
// Seit der Umstellung nutzt pageIsAccessible denselben checkPrivatePageAccess-Pfad
// für sieben zusätzliche Endpunkte (Components, Incidents, Notices,
// Translations x3), nicht mehr nur die Dashboard-Route - der Fix
// muss also in checkPrivatePageAccess/autoAssignDefaultRole selbst sitzen,
// nicht nur im Dashboard-Handler. Dieser Test deckt deshalb neben dem
// Dashboard auch zwei der sieben Batch-1-Endpunkte ab (components, incidents).
func TestSoftDeletedRoleDoesNotAutoRejoin(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	publicGroup := r.Group("/api/public")
	publicGroup.Use(middleware.OptionalAuthMiddleware())
	publicGroup.GET("/pages/:slug/dashboard", h.GetPublicDashboardHandler)

	r.GET("/api/pages/:page_id/components", middleware.OptionalAuthMiddleware(), h.GetComponentsHandler)
	r.GET("/api/pages/:page_id/incidents", middleware.OptionalAuthMiddleware(), h.GetIncidentsHandler)

	pageUsers := r.Group("/api/admin/pages/:page_id/users")
	pageUsers.Use(middleware.AuthMiddleware(), middleware.RequirePermission("pages:write"))
	pageUsers.DELETE("/:uid", h.RemoveStatusPageUserHandler)

	role := models.Role{Name: "Default Viewer", Permissions: "incidents:read"}
	db.Create(&role)

	page := models.StatusPage{Name: "Auto-Rejoin Page", Slug: "auto-rejoin-page", IsPrivate: true, DefaultRoleID: &role.ID, AutoJoinEmailDomains: "example.com"}
	db.Create(&page)
	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	db.Create(&comp)
	pid := strconv.Itoa(int(page.ID))

	admin := models.User{Email: "rejoin-admin@example.com", IsGlobalAdmin: true}
	db.Create(&admin)
	adminToken, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)

	removedUser := models.User{Email: "removed-user@example.com"}
	db.Create(&removedUser)
	removedUserToken, _ := middleware.GenerateToken(removedUser.ID, removedUser.Email, "", "", false)

	// Schritt 1: erstmaliger Auto-Join über das Dashboard, damit überhaupt
	// eine Rolle existiert, die anschließend gezielt entfernt werden kann.
	joinReq, _ := http.NewRequest("GET", "/api/public/pages/"+page.Slug+"/dashboard", nil)
	joinReq.Header.Set("Authorization", "Bearer "+removedUserToken)
	joinW := httptest.NewRecorder()
	r.ServeHTTP(joinW, joinReq)
	assert.Equal(t, http.StatusOK, joinW.Code)

	var joined models.UserStatusPageRole
	assert.NoError(t, db.Where("user_id = ? AND status_page_id = ?", removedUser.ID, page.ID).First(&joined).Error)

	// Schritt 2: Admin entfernt den Nutzer explizit (Soft-Delete, auditiert).
	removeReq, _ := http.NewRequest("DELETE", "/api/admin/pages/"+pid+"/users/"+strconv.Itoa(int(removedUser.ID)), nil)
	removeReq.Header.Set("Authorization", "Bearer "+adminToken)
	removeW := httptest.NewRecorder()
	r.ServeHTTP(removeW, removeReq)
	assert.Equal(t, http.StatusOK, removeW.Code)

	var afterRemoval models.UserStatusPageRole
	scopedErr := db.Where("user_id = ? AND status_page_id = ?", removedUser.ID, page.ID).First(&afterRemoval).Error
	assert.Error(t, scopedErr, "die Zeile darf für normale (gescopte) Queries nicht mehr sichtbar sein")

	var stillThereUnscoped models.UserStatusPageRole
	assert.NoError(t, db.Unscoped().Where("user_id = ? AND status_page_id = ?", removedUser.ID, page.ID).First(&stillThereUnscoped).Error,
		"Soft-Delete: die Zeile existiert per Unscoped() weiterhin, nur als gelöscht markiert")
	assert.NotNil(t, stillThereUnscoped.DeletedAt)

	var removalAuditCount int64
	db.Model(&models.AuditLog{}).Where("action = ?", "statuspage.user_role_removed").Count(&removalAuditCount)
	assert.Equal(t, int64(1), removalAuditCount, "die Entfernung selbst war schon vorher auditiert")

	routes := []struct {
		name string
		path string
	}{
		{"Dashboard", "/api/public/pages/" + page.Slug + "/dashboard"},
		{"components (Batch 1)", "/api/pages/" + pid + "/components"},
		{"incidents (Batch 1)", "/api/pages/" + pid + "/incidents"},
	}

	for _, rt := range routes {
		t.Run(rt.name+": entfernter Nutzer wird NICHT automatisch wieder aufgenommen", func(t *testing.T) {
			req, _ := http.NewRequest("GET", rt.path, nil)
			req.Header.Set("Authorization", "Bearer "+removedUserToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code, "explizit entfernter Nutzer darf nicht automatisch wieder Zugriff bekommen (404 wie bei einer fehlenden Seite)")

			var count int64
			db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", removedUser.ID, page.ID).Count(&count)
			assert.Equal(t, int64(0), count, "keine neue aktive UserStatusPageRole-Zeile darf entstanden sein")
		})
	}

	// Gegenprobe: ein Nutzer, der NIE eine Rolle hatte (keine Soft-Delete-Zeile
	// vorhanden), wird weiterhin ganz normal per Auto-Join zugelassen - sonst
	// wäre Auto-Join komplett abgeschafft statt nur die Soft-Delete-Lücke
	// geschlossen. Prüft zugleich den neuen Audit-Trail für automatische
	// Rollenvergaben.
	t.Run("Gegenprobe: Nutzer ohne jede Vorgeschichte wird weiterhin normal auto-gejoint", func(t *testing.T) {
		newcomer := models.User{Email: "rejoin-newcomer@example.com"}
		db.Create(&newcomer)
		newcomerToken, _ := middleware.GenerateToken(newcomer.ID, newcomer.Email, "", "", false)

		var before int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", newcomer.ID, page.ID).Count(&before)
		assert.Equal(t, int64(0), before)

		req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/components", nil)
		req.Header.Set("Authorization", "Bearer "+newcomerToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var after int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", newcomer.ID, page.ID).Count(&after)
		assert.Equal(t, int64(1), after)

		var autoAssignAuditCount int64
		db.Model(&models.AuditLog{}).Where("action = ?", "statuspage.user_role_auto_assigned").Count(&autoAssignAuditCount)
		// 2: der initiale Auto-Join von removedUser oben (Schritt 1) UND
		// dieser hier - beide automatischen Rollenvergaben müssen jetzt
		// auditiert sein, nicht nur der spätere manuelle Entzug.
		assert.Equal(t, int64(2), autoAssignAuditCount, "auch automatische Rollenvergaben müssen jetzt im Audit-Log erfasst sein")
	})
}
