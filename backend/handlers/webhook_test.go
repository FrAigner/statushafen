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
)

// doWebhookStatusPost postet einen Status-Update ans Component-Status-Webhook;
// token wird nur als X-Webhook-Token-Header gesetzt, wenn nicht leer.
func doWebhookStatusPost(r http.Handler, compIDStr, status, token string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"status": status})
	req, _ := http.NewRequest("POST", "/api/webhooks/components/"+compIDStr+"/status", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Webhook-Token", token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestComponentStatusWebhook: gültiger Token setzt den
// Status korrekt, ungültiger/fehlender Token wird mit 401 abgelehnt.
func TestComponentStatusWebhook(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.POST("/api/admin/pages/:page_id/webhook-token", h.GenerateWebhookTokenHandler)
	r.DELETE("/api/admin/pages/:page_id/webhook-token", h.RevokeWebhookTokenHandler)
	r.POST("/api/webhooks/components/:component_id/status", h.ComponentStatusWebhookHandler)

	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	db.Create(&page)

	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	db.Create(&comp)

	pageIDStr := strconv.FormatUint(uint64(page.ID), 10)
	compIDStr := strconv.FormatUint(uint64(comp.ID), 10)

	t.Run("Fehlender Token wird mit 401 abgelehnt, bevor überhaupt ein Token generiert wurde", func(t *testing.T) {
		w := doWebhookStatusPost(r, compIDStr, "major_outage", "")

		requireStatusCode(t, w, http.StatusUnauthorized)

		var fresh models.Component
		db.First(&fresh, comp.ID)
		if fresh.Status != "operational" {
			t.Errorf("Status sollte unverändert bleiben, ist aber %q", fresh.Status)
		}
	})

	// Token generieren
	req, _ := http.NewRequest("POST", "/api/admin/pages/"+pageIDStr+"/webhook-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusOK)
	var genResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &genResp); err != nil {
		t.Fatalf("Konnte Antwort nicht parsen: %v", err)
	}
	if genResp.Token == "" {
		t.Fatalf("Erwartete einen nicht-leeren Token")
	}
	validToken := genResp.Token

	t.Run("Gültiger Token (Header) setzt den Status korrekt", func(t *testing.T) {
		w := doWebhookStatusPost(r, compIDStr, "major_outage", validToken)

		requireStatusCode(t, w, http.StatusOK)

		var fresh models.Component
		db.First(&fresh, comp.ID)
		if fresh.Status != "major_outage" {
			t.Errorf("Expected status major_outage, got %q", fresh.Status)
		}
	})

	t.Run("Gültiger Token (Query-Param) setzt den Status korrekt", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"status": "operational"})
		req, _ := http.NewRequest("POST", "/api/webhooks/components/"+compIDStr+"/status?token="+validToken, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var fresh models.Component
		db.First(&fresh, comp.ID)
		if fresh.Status != "operational" {
			t.Errorf("Expected status operational, got %q", fresh.Status)
		}
	})

	t.Run("Ungültiger Token wird mit 401 abgelehnt", func(t *testing.T) {
		w := doWebhookStatusPost(r, compIDStr, "major_outage", "not-the-right-token")

		requireStatusCode(t, w, http.StatusUnauthorized)
	})

	t.Run("Unbekannter Status wird mit 400 abgelehnt", func(t *testing.T) {
		w := doWebhookStatusPost(r, compIDStr, "totally_invalid", validToken)

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	// Keine Enumeration: eine unbekannte ID antwortet exakt wie eine bekannte
	// ID mit falschem Token (Status und Body), auch mit einem gültigen Token
	// einer anderen Seite.
	t.Run("Unbekannte Component-ID antwortet wie falscher Token", func(t *testing.T) {
		unknown := doWebhookStatusPost(r, "999999", "major_outage", validToken)
		wrong := doWebhookStatusPost(r, compIDStr, "major_outage", "not-the-right-token")

		requireStatusCode(t, unknown, http.StatusUnauthorized)
		requireStatusCode(t, wrong, http.StatusUnauthorized)
		if unknown.Body.String() != wrong.Body.String() {
			t.Errorf("Antworten unterscheiden sich: %q vs %q", unknown.Body.String(), wrong.Body.String())
		}
		noToken := doWebhookStatusPost(r, "999999", "major_outage", "")
		requireStatusCode(t, noToken, http.StatusUnauthorized)
		if noToken.Body.String() != wrong.Body.String() {
			t.Errorf("Antwort ohne Token unterscheidet sich: %q", noToken.Body.String())
		}
	})

	// Token widerrufen -> vorher gültiger Token darf nicht mehr funktionieren.
	req, _ = http.NewRequest("DELETE", "/api/admin/pages/"+pageIDStr+"/webhook-token", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusOK)

	t.Run("Widerrufener Token wird mit 401 abgelehnt", func(t *testing.T) {
		w := doWebhookStatusPost(r, compIDStr, "major_outage", validToken)

		requireStatusCode(t, w, http.StatusUnauthorized)
	})

	t.Run("Regenerierter Token invalidiert den vorherigen Token", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/admin/pages/"+pageIDStr+"/webhook-token", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusOK)
		var resp struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Token == "" || resp.Token == validToken {
			t.Fatalf("Erwartete einen neuen, nicht-leeren Token, bekam %q", resp.Token)
		}

		// Alter Token darf nicht mehr funktionieren
		w2 := doWebhookStatusPost(r, compIDStr, "major_outage", validToken)
		requireStatusCode(t, w2, http.StatusUnauthorized)

		// Neuer Token funktioniert
		w3 := doWebhookStatusPost(r, compIDStr, "major_outage", resp.Token)
		requireStatusCode(t, w3, http.StatusOK)
	})
}
