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

// die Translation-Endpunkte liefen über globale IDs
// (incident_id/component_id/notice_id) ohne status_page_id-Join. Wer die ID
// eines Kind-Objekts einer privaten Statusseite kannte/erriet, bekam dessen
// Translations trotzdem, ohne Rolle auf der Seite.
func TestTranslationEndpointsRespectIsPrivate(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/incidents/:incident_id/translations", middleware.OptionalAuthMiddleware(), h.GetIncidentTranslationsHandler)
	r.GET("/api/components/:component_id/translations", middleware.OptionalAuthMiddleware(), h.GetComponentTranslationsHandler)
	r.GET("/api/notices/:notice_id/translations", middleware.OptionalAuthMiddleware(), h.GetGeneralNoticeTranslationsHandler)

	privatePage := models.StatusPage{Name: "Private Translation Page", Slug: "private-translation-page", IsPrivate: true}
	db.Create(&privatePage)
	incident := models.Incident{StatusPageID: privatePage.ID, Title: "Outage", Message: "m", Status: "investigating"}
	db.Create(&incident)
	comp := models.Component{StatusPageID: privatePage.ID, Name: "API"}
	db.Create(&comp)
	notice := models.GeneralNotice{StatusPageID: privatePage.ID, Title: "Notice", Message: "m", Level: "info", IsActive: true}
	db.Create(&notice)

	role := models.Role{Name: "Translation Viewer", Permissions: "incidents:read"}
	db.Create(&role)

	insider := models.User{Email: "trans-insider@example.com"}
	db.Create(&insider)
	db.Create(&models.UserStatusPageRole{UserID: insider.ID, StatusPageID: privatePage.ID, RoleID: role.ID})
	insiderToken, _ := middleware.GenerateToken(insider.ID, insider.Email, "", "", false)

	outsider := models.User{Email: "trans-outsider@example.com"}
	db.Create(&outsider)
	otherPage := models.StatusPage{Name: "Other Translation Page", Slug: "other-translation-page"}
	db.Create(&otherPage)
	db.Create(&models.UserStatusPageRole{UserID: outsider.ID, StatusPageID: otherPage.ID, RoleID: role.ID})
	outsiderToken, _ := middleware.GenerateToken(outsider.ID, outsider.Email, "", "", false)

	// StatusPageID zeigt bewusst auf keine existierende Seite (verwaiste
	// Daten) - deckt den "page not found"-Zweig ab, den ein aufgeräumtes
	// Datenmodell praktisch nie erreicht, den der Code aber trotzdem prüft.
	const missingPageID = 999999
	orphanIncident := models.Incident{StatusPageID: missingPageID, Title: "Orphan", Message: "m", Status: "investigating"}
	db.Create(&orphanIncident)
	orphanComp := models.Component{StatusPageID: missingPageID, Name: "Orphan"}
	db.Create(&orphanComp)
	orphanNotice := models.GeneralNotice{StatusPageID: missingPageID, Title: "Orphan", Message: "m", Level: "info", IsActive: true}
	db.Create(&orphanNotice)

	routes := []struct {
		name       string
		path       string
		orphanPath string
	}{
		{"incident translations", "/api/incidents/" + strconv.Itoa(int(incident.ID)) + "/translations", "/api/incidents/" + strconv.Itoa(int(orphanIncident.ID)) + "/translations"},
		{"component translations", "/api/components/" + strconv.Itoa(int(comp.ID)) + "/translations", "/api/components/" + strconv.Itoa(int(orphanComp.ID)) + "/translations"},
		{"notice translations", "/api/notices/" + strconv.Itoa(int(notice.ID)) + "/translations", "/api/notices/" + strconv.Itoa(int(orphanNotice.ID)) + "/translations"},
	}

	for _, rt := range routes {
		t.Run(rt.name+": Insider mit Rolle bekommt 200", func(t *testing.T) {
			req, _ := http.NewRequest("GET", rt.path, nil)
			req.Header.Set("Authorization", "Bearer "+insiderToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run(rt.name+": Outsider mit fremder Rolle bekommt 404 wie bei einem fehlenden Objekt", func(t *testing.T) {
			req, _ := http.NewRequest("GET", rt.path, nil)
			req.Header.Set("Authorization", "Bearer "+outsiderToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})

		t.Run(rt.name+": Unauthentifiziert bekommt 404 wie bei einem fehlenden Objekt", func(t *testing.T) {
			req, _ := http.NewRequest("GET", rt.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})

		t.Run(rt.name+": verwaistes Elternobjekt (unbekannte Seite) gibt 404", func(t *testing.T) {
			req, _ := http.NewRequest("GET", rt.orphanPath, nil)
			req.Header.Set("Authorization", "Bearer "+insiderToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}
