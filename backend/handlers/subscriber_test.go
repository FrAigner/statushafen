package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func TestSubscriberHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/pages/:page_id/subscribers", h.GetSubscribersHandler)
	r.POST("/api/subscribe", h.SubscribeHandler)
	r.POST("/api/pages/:page_id/subscribers", h.CreateSubscriberAdminHandler)
	r.PUT("/api/pages/:page_id/subscribers/:id", h.UpdateSubscriberHandler)
	r.DELETE("/api/pages/:page_id/subscribers/:id", h.DeleteSubscriberHandler)

	page := models.StatusPage{Name: "Sub Page", Slug: "sub-page"}
	db.Create(&page)

	pid := strconv.Itoa(int(page.ID))
	var createdSubscriberID uint

	t.Run("Create Subscriber - Valid Email", func(t *testing.T) {
		payload := map[string]interface{}{
			"channel":       "email",
			"email":         "test@example.com",
			"component_ids": "0",
			"slug":          "sub-page",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/subscribe", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var dbSub models.Subscriber
		db.Where("email = ?", "test@example.com").First(&dbSub)
		createdSubscriberID = dbSub.ID

		if dbSub.Email != "test@example.com" {
			t.Errorf("Subscriber created incorrectly")
		}
	})

	t.Run("Create Subscriber - Invalid Webhook (missing URL)", func(t *testing.T) {
		payload := map[string]interface{}{
			"channel": "google_chat",
			// WebhookURL is missing
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/subscribe", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Admin Create Subscriber - Valid Webhook", func(t *testing.T) {
		payload := map[string]interface{}{
			"Channel":    "google_chat",
			"WebhookURL": "https://chat.googleapis.com/test",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+pid+"/subscribers", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusCreated)
	})

	t.Run("Admin Create Subscriber - Missing page_id", func(t *testing.T) {
		// Route without :page_id won't match, so we test with invalid id "abc"
		payload := map[string]interface{}{
			"Channel": "email",
			"Email":   "admin_add@example.com",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/abc/subscribers", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Get Subscribers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/subscribers", nil)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var subs []models.Subscriber
		_ = json.Unmarshal(w.Body.Bytes(), &subs)

		if len(subs) != 2 {
			t.Errorf("Expected 2 subscribers, got %d", len(subs))
		}
	})

	// page_id-Parse-Fehler wurde verworfen (tenantID=0), sodass
	// GetSubscribersHandler bei ungültigem page_id einfach eine leere Liste
	// statt eines klaren 400 zurückgab.
	t.Run("Get Subscribers mit ungültiger page_id gibt 400", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/not-a-number/subscribers", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Update Subscriber", func(t *testing.T) {
		payload := map[string]interface{}{
			"Email":                  "updated@example.com",
			"SubscribedComponentIDs": "all",
		}
		body, _ := json.Marshal(payload)
		url := "/api/pages/" + pid + "/subscribers/" + strconv.Itoa(int(createdSubscriberID))
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var sub models.Subscriber
		db.First(&sub, createdSubscriberID)
		if sub.Email != "updated@example.com" {
			t.Errorf("Subscriber not updated correctly")
		}
	})

	// Regression: UpdateSubReq markierte SubscribedComponentIDs früher als required
	// mit snake_case-JSON-Tags, die nie zum PascalCase-Payload des Frontends passten
	// (AdminSubscribers.vue sendet formData direkt) - jede Bearbeitung schlug mit
	// "Invalid fields" fehl, sobald das Feld leer war ("leer = alle Komponenten").
	t.Run("Update Subscriber mit leerem SubscribedComponentIDs (alle Komponenten)", func(t *testing.T) {
		payload := map[string]interface{}{
			"Channel":                "email",
			"Email":                  "updated-empty@example.com",
			"SubscribedComponentIDs": "",
		}
		body, _ := json.Marshal(payload)
		url := "/api/pages/" + pid + "/subscribers/" + strconv.Itoa(int(createdSubscriberID))
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var sub models.Subscriber
		db.First(&sub, createdSubscriberID)
		if sub.SubscribedComponentIDs != "" {
			t.Errorf("Expected empty SubscribedComponentIDs, got %q", sub.SubscribedComponentIDs)
		}
	})

	t.Run("Delete Subscriber", func(t *testing.T) {
		url := "/api/pages/" + pid + "/subscribers/" + strconv.Itoa(int(createdSubscriberID))
		req, _ := http.NewRequest("DELETE", url, nil)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var count int64
		db.Model(&models.Subscriber{}).Where("id = ?", createdSubscriberID).Count(&count)
		if count != 0 {
			t.Errorf("Subscriber was not deleted")
		}
	})
}

// TestSubscriberOwnership ist ein Regressionstest für den gemeldeten Bug: Ein
// Admin legt über die CMS-Oberfläche ein Google-Chat-Webhook-Abo ohne Email an.
// Vorher wurde dabei nirgends gespeichert, welcher User das Abo angelegt hat,
// wodurch dieser User es später nicht über /me/subscription wiederfinden bzw.
// bearbeiten konnte (der Abgleich lief rein über den Email-String, der bei
// Webhook-Abos aber leer bleibt).
func TestSubscriberOwnership(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	subAdmin := r.Group("/api/pages/:page_id/subscribers")
	subAdmin.Use(middleware.AuthMiddleware())
	subAdmin.POST("", h.CreateSubscriberAdminHandler)

	meGroup := r.Group("/api/me")
	meGroup.Use(middleware.AuthMiddleware())
	meGroup.GET("/subscription", h.GetMySubscriptionHandler)
	meGroup.PUT("/subscription", h.UpdateMySubscriptionHandler)

	page := models.StatusPage{Name: "Ownership Page", Slug: "ownership-page"}
	db.Create(&page)
	pid := strconv.Itoa(int(page.ID))

	admin := models.User{Email: "page-admin@example.com"}
	db.Create(&admin)
	adminToken, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", false)
	// Wie in Produktion (Route hinter RequirePermission) gehört der Ersteller
	// zur Seite; das Ersteller-Recht gilt nur, solange die Rolle besteht.
	adminRole := models.Role{Name: "Ownership Admin", Permissions: "subscribers:create"}
	db.Create(&adminRole)
	db.Create(&models.UserStatusPageRole{UserID: admin.ID, StatusPageID: page.ID, RoleID: adminRole.ID})

	t.Run("Admin-angelegtes Webhook-Abo ohne Email wird dem anlegenden User zugeordnet", func(t *testing.T) {
		payload := map[string]interface{}{
			"Channel":    "google_chat",
			"WebhookURL": "https://chat.googleapis.com/ownership-test",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+pid+"/subscribers", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var sub models.Subscriber
		db.Where("webhook_url = ?", "https://chat.googleapis.com/ownership-test").First(&sub)
		if sub.CreatedByUserID == nil || *sub.CreatedByUserID != admin.ID {
			t.Fatalf("Expected CreatedByUserID %d, got %v", admin.ID, sub.CreatedByUserID)
		}
	})

	t.Run("Ersteller findet sein Webhook-Abo ohne Email über /me/subscription", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me/subscription?slug=ownership-page", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var res map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["channel"] != "google_chat" {
			t.Errorf("Expected channel google_chat, got %v", res["channel"])
		}
	})

	t.Run("Ersteller kann sein Webhook-Abo ohne Email über /me/subscription bearbeiten", func(t *testing.T) {
		payload := map[string]interface{}{"component_ids": "1,2,3"}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", "/api/me/subscription?slug=ownership-page", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var sub models.Subscriber
		db.Where("webhook_url = ?", "https://chat.googleapis.com/ownership-test").First(&sub)
		if sub.SubscribedComponentIDs != "1,2,3" {
			t.Errorf("Expected updated component_ids, got %q", sub.SubscribedComponentIDs)
		}
	})

	t.Run("Anderer User findet fremdes Webhook-Abo nicht", func(t *testing.T) {
		other := models.User{Email: "someone-else@example.com"}
		db.Create(&other)
		otherToken, _ := middleware.GenerateToken(other.ID, other.Email, "", "", false)

		req, _ := http.NewRequest("GET", "/api/me/subscription?slug=ownership-page", nil)
		req.Header.Set("Authorization", "Bearer "+otherToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d", w.Code)
		}
	})

	// "exists := email != ''; if !exists || email == ''" war eine
	// redundante Tautologie (beide Bedingungen sind identisch) - vereinfacht
	// zu "if email == ''", ohne Verhaltensänderung. Ein gültiger Token ohne
	// Email-Claim (z.B. bestimmte SSO-Flows) muss weiterhin 401 auslösen.
	t.Run("Gültiger Token ohne Email gibt 401", func(t *testing.T) {
		noEmailUser := models.User{Email: "noemail-carrier@example.com"}
		db.Create(&noEmailUser)
		noEmailToken, _ := middleware.GenerateToken(noEmailUser.ID, "", "", "", false)

		req, _ := http.NewRequest("GET", "/api/me/subscription?slug=ownership-page", nil)
		req.Header.Set("Authorization", "Bearer "+noEmailToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusUnauthorized)

		putReq, _ := http.NewRequest("PUT", "/api/me/subscription?slug=ownership-page", bytes.NewBuffer([]byte(`{}`)))
		putReq.Header.Set("Content-Type", "application/json")
		putReq.Header.Set("Authorization", "Bearer "+noEmailToken)
		putW := httptest.NewRecorder()
		r.ServeHTTP(putW, putReq)
		requireStatusCode(t, putW, http.StatusUnauthorized)
	})
}

// TestSubscribeHandler_DedupIsScopedPerStatusPage: the
// existing-subscription lookup in SubscribeHandler previously ignored
// status_page_id entirely, so the same email/webhook could only ever be
// subscribed to ONE status page across the whole (multi-tenant) instance -
// subscribing to a second page incorrectly returned 409 "already subscribed"
// instead of creating an independent, page-scoped subscription.
func TestSubscribeHandler_DedupIsScopedPerStatusPage(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/subscribe", h.SubscribeHandler)

	pageAcme := models.StatusPage{Name: "Acme", Slug: "acme-dedup"}
	db.Create(&pageAcme)
	pageTechstart := models.StatusPage{Name: "Techstart", Slug: "techstart-dedup"}
	db.Create(&pageTechstart)

	doSubscribe := func(email, slug string) *httptest.ResponseRecorder {
		payload := map[string]interface{}{
			"channel":       "email",
			"email":         email,
			"component_ids": "all",
			"slug":          slug,
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/subscribe", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("Erstes Abo auf Seite acme erfolgreich", func(t *testing.T) {
		w := doSubscribe("a@b.de", "acme-dedup")
		requireStatusCode(t, w, http.StatusOK)
	})

	t.Run("Gleiche Adresse auf anderer Seite (techstart) ebenfalls erfolgreich, kein 409", func(t *testing.T) {
		w := doSubscribe("a@b.de", "techstart-dedup")
		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 (page-scoped subscription must not collide across tenants), got %d: %s", w.Code, w.Body.String())
		}

		var count int64
		db.Model(&models.Subscriber{}).Where("email = ?", "a@b.de").Count(&count)
		if count != 2 {
			t.Errorf("Expected 2 independent subscriber rows (one per page), got %d", count)
		}
	})

	t.Run("Erneutes Abo derselben Seite legt kein zweites an und antwortet wie ein neues", func(t *testing.T) {
		// Bewusst geändert (vorher 409): Bestätigte Abos antworten wie neue
		// (200, pending), damit die Antwort nicht verrät, ob die Adresse
		// abonniert ist.
		db.Model(&models.Subscriber{}).Where("email = ? AND status_page_id = ?", "a@b.de", pageAcme.ID).Update("status", models.SubscriberStatusActive)
		w := doSubscribe("a@b.de", "acme-dedup")
		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 for a duplicate subscription on the SAME page, got %d: %s", w.Code, w.Body.String())
		}
		var count int64
		db.Model(&models.Subscriber{}).Where("email = ? AND status_page_id = ?", "a@b.de", pageAcme.ID).Count(&count)
		if count != 1 {
			t.Errorf("Expected still exactly 1 subscriber on the page, got %d", count)
		}
	})
}

// TestSubscriberLifecycle_RecordsAuditEntries: Subscribe,
// Admin-Update, Admin-Delete und der Token-basierte Unsubscribe-Flow müssen
// jeweils einen Audit-Log-Eintrag hinterlassen, damit Subscriber-Änderungen
// im Audit-Log nachvollziehbar sind (vorher gab es dort keinen einzigen
// Eintrag für den gesamten Subscriber-Lebenszyklus).
func TestSubscriberLifecycle_RecordsAuditEntries(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.POST("/api/subscribe", h.SubscribeHandler)
	r.PUT("/api/pages/:page_id/subscribers/:id", h.UpdateSubscriberHandler)
	r.DELETE("/api/pages/:page_id/subscribers/:id", h.DeleteSubscriberHandler)
	r.POST("/api/public/subscribers/unsubscribe", h.UnsubscribeHandler)

	page := models.StatusPage{Name: "Audit Lifecycle Page", Slug: "audit-lifecycle-page"}
	db.Create(&page)
	pid := strconv.Itoa(int(page.ID))

	countAudit := func(action string) int64 {
		var count int64
		db.Model(&models.AuditLog{}).Where("action = ?", action).Count(&count)
		return count
	}

	t.Run("SubscribeHandler schreibt subscriber.created", func(t *testing.T) {
		payload := map[string]interface{}{
			"channel":       "email",
			"email":         "audit-sub@example.com",
			"component_ids": "all",
			"slug":          "audit-lifecycle-page",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/subscribe", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)
		if got := countAudit("subscriber.created"); got != 1 {
			t.Errorf("Expected 1 subscriber.created audit entry, got %d", got)
		}
	})

	var sub models.Subscriber
	db.Where("email = ?", "audit-sub@example.com").First(&sub)
	sid := strconv.Itoa(int(sub.ID))

	t.Run("UpdateSubscriberHandler schreibt subscriber.updated", func(t *testing.T) {
		payload := map[string]interface{}{
			"Channel":                "email",
			"Email":                  "audit-sub@example.com",
			"SubscribedComponentIDs": "0",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", "/api/pages/"+pid+"/subscribers/"+sid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)
		if got := countAudit("subscriber.updated"); got != 1 {
			t.Errorf("Expected 1 subscriber.updated audit entry, got %d", got)
		}
	})

	t.Run("UnsubscribeHandler schreibt subscriber.unsubscribed", func(t *testing.T) {
		token, err := middleware.GenerateSubscriberToken(sub.ID, sub.StatusPageID)
		if err != nil {
			t.Fatalf("failed to generate subscriber token: %v", err)
		}
		req, _ := http.NewRequest("POST", "/api/public/subscribers/unsubscribe?token="+token, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)
		if got := countAudit("subscriber.unsubscribed"); got != 1 {
			t.Errorf("Expected 1 subscriber.unsubscribed audit entry, got %d", got)
		}
	})

	t.Run("DeleteSubscriberHandler schreibt subscriber.deleted", func(t *testing.T) {
		other := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "audit-sub-2@example.com", SubscribedComponentIDs: "all"}
		db.Create(&other)
		otherID := strconv.Itoa(int(other.ID))

		req, _ := http.NewRequest("DELETE", "/api/pages/"+pid+"/subscribers/"+otherID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)
		if got := countAudit("subscriber.deleted"); got != 1 {
			t.Errorf("Expected 1 subscriber.deleted audit entry, got %d", got)
		}
	})
}
