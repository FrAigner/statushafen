package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
)

func TestIncidentDetailHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/pages/:page_id/incidents/:id", middleware.OptionalAuthMiddleware(), h.GetIncidentDetailHandler)

	incAdmin := r.Group("/api/pages/:page_id/incidents")
	incAdmin.Use(middleware.AuthMiddleware(), middleware.RequirePermission("incidents:write"))
	incAdmin.POST("/:id/updates", h.AddIncidentUpdateHandler)
	incAdmin.PUT("/:id/postmortem", h.UpsertPostMortemHandler)

	page := models.StatusPage{Name: "Detail Page", Slug: "detail-page"}
	db.Create(&page)
	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	db.Create(&comp)
	incident := models.Incident{StatusPageID: page.ID, Title: "Outage", Message: "initial", Status: "investigating"}
	db.Create(&incident)
	db.Create(&models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp.ID})

	adminUser := models.User{Email: "detail-admin@example.com", IsGlobalAdmin: true}
	db.Create(&adminUser)
	adminToken, _ := middleware.GenerateToken(adminUser.ID, adminUser.Email, "", "", true)

	pid := strconv.Itoa(int(page.ID))
	iid := strconv.Itoa(int(incident.ID))

	t.Run("Get detail - no updates, no post-mortem yet", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/incidents/"+iid, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"post_mortem":null`)
	})

	t.Run("Add incident update - moves status and appears in timeline", func(t *testing.T) {
		payload := map[string]string{"status": "identified", "message": "root cause found"}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+pid+"/incidents/"+iid+"/updates", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)

		var updated models.Incident
		db.First(&updated, incident.ID)
		assert.Equal(t, "identified", updated.Status)

		var auditCount int64
		db.Model(&models.AuditLog{}).Where("action = ?", "incident.update_added").Count(&auditCount)
		assert.Equal(t, int64(1), auditCount)
	})

	t.Run("Draft post-mortem hidden from anonymous, visible to authenticated", func(t *testing.T) {
		payload := map[string]interface{}{
			"summary":    "Draft summary",
			"root_cause": "Bad deploy",
			"publish":    false,
			"actions":    []map[string]string{{"text": "Add rollback safeguard", "owner": "ops"}},
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", "/api/pages/"+pid+"/incidents/"+iid+"/postmortem", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		anonReq, _ := http.NewRequest("GET", "/api/pages/"+pid+"/incidents/"+iid, nil)
		anonW := httptest.NewRecorder()
		r.ServeHTTP(anonW, anonReq)
		assert.Contains(t, anonW.Body.String(), `"post_mortem":null`)

		authReq, _ := http.NewRequest("GET", "/api/pages/"+pid+"/incidents/"+iid, nil)
		authReq.Header.Set("Authorization", "Bearer "+adminToken)
		authW := httptest.NewRecorder()
		r.ServeHTTP(authW, authReq)
		assert.Contains(t, authW.Body.String(), "Draft summary")
	})

	// Sicherheitsprüfung: vorher zeigte JEDER valide JWT den Entwurf,
	// unabhängig davon, ob der User überhaupt eine Rolle auf DIESER page_id
	// hat. Ein authentifizierter User ohne Rolle auf "detail-page" darf den
	// Entwurf nicht sehen.
	t.Run("Draft post-mortem hidden from authenticated user without role on this page", func(t *testing.T) {
		strangerRole := models.Role{Name: "Stranger Viewer", Permissions: "incidents:read"}
		db.Create(&strangerRole)
		otherPage := models.StatusPage{Name: "Other Page", Slug: "other-page-detail"}
		db.Create(&otherPage)
		stranger := models.User{Email: "stranger@example.com"}
		db.Create(&stranger)
		db.Create(&models.UserStatusPageRole{UserID: stranger.ID, StatusPageID: otherPage.ID, RoleID: strangerRole.ID})
		strangerToken, _ := middleware.GenerateToken(stranger.ID, stranger.Email, "", "", false)

		req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/incidents/"+iid, nil)
		req.Header.Set("Authorization", "Bearer "+strangerToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"post_mortem":null`)
	})

	t.Run("Publishing post-mortem sets published_at once (idempotent)", func(t *testing.T) {
		payload := map[string]interface{}{"summary": "Final summary", "publish": true}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", "/api/pages/"+pid+"/incidents/"+iid+"/postmortem", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var pm models.PostMortem
		db.Where("incident_id = ?", incident.ID).First(&pm)
		assert.NotNil(t, pm.PublishedAt)
		firstPublishedAt := pm.PublishedAt

		payload2 := map[string]interface{}{"summary": "Final summary v2", "publish": true}
		body2, _ := json.Marshal(payload2)
		req2, _ := http.NewRequest("PUT", "/api/pages/"+pid+"/incidents/"+iid+"/postmortem", bytes.NewBuffer(body2))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer "+adminToken)
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Code)

		var pm2 models.PostMortem
		db.Where("incident_id = ?", incident.ID).First(&pm2)
		assert.Equal(t, firstPublishedAt.Unix(), pm2.PublishedAt.Unix())

		anonReq, _ := http.NewRequest("GET", "/api/pages/"+pid+"/incidents/"+iid, nil)
		anonW := httptest.NewRecorder()
		r.ServeHTTP(anonW, anonReq)
		assert.Contains(t, anonW.Body.String(), "Final summary v2")
	})

	// page_id-Parse-Fehler wurden verworfen, sodass ein
	// ungültiges page_id stillschweigend zu tenantID=0 wurde, statt einen
	// klaren 400 zurückzugeben. GetIncidentDetailHandler ist die öffentliche
	// Route ohne RequirePermission-Middleware, die den page_id-Parse-Fehler
	// sonst schon vorher abfangen würde - hier greift also direkt der Fix im
	// Handler selbst.
	t.Run("Get detail mit ungültiger page_id gibt 400", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/not-a-number/incidents/"+iid, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	// AddIncidentUpdateHandler/UpsertPostMortemHandler hängen hinter
	// RequirePermission, das für normale User page_id selbst schon prüft -
	// für Global Admins (wie adminToken hier) wird dieser Check aber
	// übersprungen, sodass ein ungültiges page_id ungehindert bis in den
	// Handler durchreicht.
	t.Run("Add incident update mit ungültiger page_id gibt 400", func(t *testing.T) {
		payload := map[string]string{"status": "identified", "message": "x"}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/not-a-number/incidents/"+iid+"/updates", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Upsert post-mortem mit ungültiger page_id gibt 400", func(t *testing.T) {
		payload := map[string]interface{}{"summary": "x", "publish": false}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", "/api/pages/not-a-number/incidents/"+iid+"/postmortem", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// Auto-Join (bei einer Verifikation entdeckt): auf einer privaten Seite mit gesetzter
// DefaultRoleID legt pageIsAccessible per Auto-Join eine UserStatusPageRole-
// Zeile für den aufrufenden User an. Fragte die Postmortem-Prüfung danach
// erneut per userHasRoleOnPrivatePage ab, fand sie genau diese Zeile bereits
// vor - ein Newcomer sah den unveröffentlichten Entwurf schon beim
// allerersten Request, obwohl er zu Beginn des Requests keine Rolle hatte.
//
// Nachbesserung nach unabhängigem Review (Gemini): der erste Fix
// (Rollen-Status vor pageIsAccessible festhalten) schützte nur GENAU den
// ersten Request - die Auto-Join-Zeile landet ja tatsächlich in der DB, also
// liefert jede spätere Abfrage "hat irgendeine Rolle" beim zweiten Request
// bereits true. Der zweite GET-Request unten ist deshalb der eigentliche
// Kernfall, nicht nur eine Ergänzung. Der korrekte Fix prüft nicht mehr
// "irgendeine Rolle vorhanden", sondern dieselbe Berechtigung, die auch zum
// Schreiben eines Postmortems nötig ist (incidents:write) - ein reiner
// Auto-Join-Viewer bekommt diese Berechtigung nie, unabhängig davon, wie oft
// er die Seite aufruft.
//
// Eigene, isolierte Seite/Incident, damit der Test unabhängig von
// TestIncidentDetailHandlers läuft.
func TestDraftPostMortemHiddenDespiteAutoJoinOnFirstRequest(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/pages/:page_id/incidents/:id", middleware.OptionalAuthMiddleware(), h.GetIncidentDetailHandler)
	incAdmin := r.Group("/api/pages/:page_id/incidents")
	incAdmin.Use(middleware.AuthMiddleware(), middleware.RequirePermission("incidents:write"))
	incAdmin.PUT("/:id/postmortem", h.UpsertPostMortemHandler)

	defaultRole := models.Role{Name: "Auto-Join Viewer", Permissions: "incidents:read"}
	db.Create(&defaultRole)

	page := models.StatusPage{Name: "Auto-Join Detail Page", Slug: "auto-join-detail-page", IsPrivate: true, DefaultRoleID: &defaultRole.ID, AutoJoinEmailDomains: "example.com"}
	db.Create(&page)
	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	db.Create(&comp)
	incident := models.Incident{StatusPageID: page.ID, Title: "Outage", Message: "initial", Status: "investigating"}
	db.Create(&incident)
	db.Create(&models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp.ID})

	adminUser := models.User{Email: "auto-join-admin@example.com", IsGlobalAdmin: true}
	db.Create(&adminUser)
	adminToken, _ := middleware.GenerateToken(adminUser.ID, adminUser.Email, "", "", true)

	pid := strconv.Itoa(int(page.ID))
	iid := strconv.Itoa(int(incident.ID))

	pmPayload := map[string]interface{}{
		"summary":    "Draft summary auto-join",
		"root_cause": "Bad deploy",
		"publish":    false,
	}
	pmBody, _ := json.Marshal(pmPayload)
	pmReq, _ := http.NewRequest("PUT", "/api/pages/"+pid+"/incidents/"+iid+"/postmortem", bytes.NewBuffer(pmBody))
	pmReq.Header.Set("Content-Type", "application/json")
	pmReq.Header.Set("Authorization", "Bearer "+adminToken)
	pmW := httptest.NewRecorder()
	r.ServeHTTP(pmW, pmReq)
	assert.Equal(t, http.StatusOK, pmW.Code)

	newcomer := models.User{Email: "autojoin-newcomer@example.com"}
	db.Create(&newcomer)
	newcomerToken, _ := middleware.GenerateToken(newcomer.ID, newcomer.Email, "", "", false)

	var before int64
	db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", newcomer.ID, page.ID).Count(&before)
	assert.Equal(t, int64(0), before, "Testvoraussetzung: Newcomer darf vor dem Request noch keine Rolle auf dieser Seite haben")

	// Der eine, einzige Request: Auto-Join (Seitenzugriff) und Postmortem-Gate
	// laufen im selben Handler-Durchlauf.
	req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/incidents/"+iid, nil)
	req.Header.Set("Authorization", "Bearer "+newcomerToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"post_mortem":null`, "Entwurf darf beim ersten Auto-Join-Request nicht sichtbar sein")
	assert.NotContains(t, w.Body.String(), "Draft summary auto-join")

	var after int64
	db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", newcomer.ID, page.ID).Count(&after)
	assert.Equal(t, int64(1), after, "Auto-Join für den reinen Seitenzugriff soll weiterhin greifen")

	// Kernfall der Gemini-Nachbesserung: die Auto-Join-Zeile existiert jetzt
	// in der DB (s.o.). Ein zweiter Request desselben Newcomers unmittelbar
	// danach darf den Entwurf trotzdem NICHT zeigen - der Viewer-Rolle fehlt
	// incidents:write, egal wie oft die Seite aufgerufen wird.
	req2, _ := http.NewRequest("GET", "/api/pages/"+pid+"/incidents/"+iid, nil)
	req2.Header.Set("Authorization", "Bearer "+newcomerToken)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), `"post_mortem":null`, "Entwurf darf auch beim zweiten Request desselben Auto-Join-Viewers nicht sichtbar sein")
	assert.NotContains(t, w2.Body.String(), "Draft summary auto-join")
}

// Kontrolle zur Gemini-Nachbesserung oben: das Gate darf nicht pauschal jede
// Rolle sperren, sondern nur Rollen ohne incidents:write. Ein User mit
// Editor-artiger Rolle muss den Entwurf weiterhin sehen - sonst wäre das Gate
// nur enger, nicht korrekt. Seit der Umstellung (Review-Befund 1) darf eine Rolle
// mit Schreibrechten nicht mehr per Auto-Join vergeben werden, deshalb hier
// über eine manuelle Zuweisung.
func TestDraftPostMortemVisibleForManuallyAssignedUserWithWritePermission(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/pages/:page_id/incidents/:id", middleware.OptionalAuthMiddleware(), h.GetIncidentDetailHandler)
	incAdmin := r.Group("/api/pages/:page_id/incidents")
	incAdmin.Use(middleware.AuthMiddleware(), middleware.RequirePermission("incidents:write"))
	incAdmin.PUT("/:id/postmortem", h.UpsertPostMortemHandler)

	writeRole := models.Role{Name: "Auto-Join Editor", Permissions: "incidents:read,incidents:write"}
	db.Create(&writeRole)

	page := models.StatusPage{Name: "Auto-Join Editor Page", Slug: "auto-join-editor-page", IsPrivate: true}
	db.Create(&page)
	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	db.Create(&comp)
	incident := models.Incident{StatusPageID: page.ID, Title: "Outage", Message: "initial", Status: "investigating"}
	db.Create(&incident)
	db.Create(&models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp.ID})

	adminUser := models.User{Email: "auto-join-editor-admin@example.com", IsGlobalAdmin: true}
	db.Create(&adminUser)
	adminToken, _ := middleware.GenerateToken(adminUser.ID, adminUser.Email, "", "", true)

	pid := strconv.Itoa(int(page.ID))
	iid := strconv.Itoa(int(incident.ID))

	pmPayload := map[string]interface{}{
		"summary":    "Draft summary visible to editor",
		"root_cause": "Bad deploy",
		"publish":    false,
	}
	pmBody, _ := json.Marshal(pmPayload)
	pmReq, _ := http.NewRequest("PUT", "/api/pages/"+pid+"/incidents/"+iid+"/postmortem", bytes.NewBuffer(pmBody))
	pmReq.Header.Set("Content-Type", "application/json")
	pmReq.Header.Set("Authorization", "Bearer "+adminToken)
	pmW := httptest.NewRecorder()
	r.ServeHTTP(pmW, pmReq)
	assert.Equal(t, http.StatusOK, pmW.Code)

	editor := models.User{Email: "autojoin-editor@example.com"}
	db.Create(&editor)
	db.Create(&models.UserStatusPageRole{UserID: editor.ID, StatusPageID: page.ID, RoleID: writeRole.ID})
	editorToken, _ := middleware.GenerateToken(editor.ID, editor.Email, "", "", false)

	req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/incidents/"+iid, nil)
	req.Header.Set("Authorization", "Bearer "+editorToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Draft summary visible to editor", "User mit incidents:write (manuell zugewiesen) muss den Entwurf sehen")
}

// Ein Update darf die Erstmeldung (Incident.Message) nicht
// überschreiben; die Benachrichtigung enthält die Update-Nachricht. Fremde
// Mandanten erhalten 404 und lassen Vorfall und Updates unverändert.
func TestAddIncidentUpdateKeepsInitialMessage(t *testing.T) {
	handlers.SetWebhookLoopbackAllowedForTests(t.Cleanup)
	received := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		received <- string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("GOOGLE_CHAT_WEBHOOK_URL", srv.URL)

	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	incAdmin := r.Group("/api/pages/:page_id/incidents")
	incAdmin.Use(middleware.AuthMiddleware(), middleware.RequirePermission("incidents:write"))
	incAdmin.POST("/:id/updates", h.AddIncidentUpdateHandler)
	r.GET("/api/pages/:page_id/incidents/:id", middleware.OptionalAuthMiddleware(), h.GetIncidentDetailHandler)

	page := models.StatusPage{Name: "Erst A", Slug: "erst-a"}
	db.Create(&page)
	other := models.StatusPage{Name: "Erst B", Slug: "erst-b"}
	db.Create(&other)
	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	db.Create(&comp)
	inc := models.Incident{StatusPageID: page.ID, Title: "Ausfall", Message: "Erstmeldung Text", Status: "investigating"}
	db.Create(&inc)
	db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID})
	admin := models.User{Email: "erst-admin@example.com", IsGlobalAdmin: true}
	db.Create(&admin)
	token, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)

	post := func(pageID uint) int {
		body, _ := json.Marshal(map[string]string{"status": "identified", "message": "Update Text"})
		req, _ := http.NewRequest("POST", "/api/pages/"+strconv.Itoa(int(pageID))+"/incidents/"+strconv.Itoa(int(inc.ID))+"/updates", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	// Mandantentrennung: Vorfall einer anderen Seite -> 404, nichts geändert.
	assert.Equal(t, http.StatusNotFound, post(other.ID))
	var n int64
	db.Model(&models.IncidentUpdate{}).Count(&n)
	assert.Zero(t, n)

	assert.Equal(t, http.StatusCreated, post(page.ID))

	var after models.Incident
	db.First(&after, inc.ID)
	assert.Equal(t, "Erstmeldung Text", after.Message)
	assert.Equal(t, "identified", after.Status)

	// Verspätete Webhooks anderer Tests können ebenfalls ankommen; gesucht wird
	// die Meldung zu diesem Vorfall.
	deadline := time.After(5 * time.Second)
	for found := false; !found; {
		select {
		case payload := <-received:
			if strings.Contains(payload, "**Incident:** Ausfall") {
				found = true
				assert.Contains(t, payload, "Update Text")
				assert.NotContains(t, payload, "Erstmeldung Text")
			}
		case <-deadline:
			t.Fatal("kein Webhook für das Update empfangen")
		}
	}

	req, _ := http.NewRequest("GET", "/api/pages/"+strconv.Itoa(int(page.ID))+"/incidents/"+strconv.Itoa(int(inc.ID)), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Contains(t, w.Body.String(), "Erstmeldung Text")
	assert.Contains(t, w.Body.String(), "Update Text")
}
