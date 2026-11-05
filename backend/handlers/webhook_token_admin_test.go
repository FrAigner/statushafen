package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

// Fehlerpfade der Admin-Handler zum Erzeugen/Widerrufen des
// Webhook-Tokens (ungültige ID, unbekannte Seite, Speicherfehler).

func doWebhookTokenRequest(r http.Handler, method, pageID string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(method, "/api/admin/pages/"+pageID+"/webhook-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestWebhookTokenAdminHandlers_ErrorPaths(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/admin/pages/:page_id/webhook-token", h.GenerateWebhookTokenHandler)
	r.DELETE("/api/admin/pages/:page_id/webhook-token", h.RevokeWebhookTokenHandler)

	page := models.StatusPage{Name: "Acme", Slug: "acme", WebhookTokenHash: models.HashToken("bestehend")}
	db.Create(&page)
	pageIDStr := strconv.FormatUint(uint64(page.ID), 10)

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method+": nicht numerische page_id liefert 400", func(t *testing.T) {
			requireStatusCode(t, doWebhookTokenRequest(r, method, "abc"), http.StatusBadRequest)
		})
		t.Run(method+": unbekannte Statusseite liefert 404", func(t *testing.T) {
			requireStatusCode(t, doWebhookTokenRequest(r, method, "99999"), http.StatusNotFound)
		})
	}

	// Speicherfehler erzwingen: ein Update-Callback, der immer scheitert.
	const cbName = "test:webhook_token_save_error"
	if err := db.Callback().Update().Before("gorm:update").Register(cbName, func(tx *gorm.DB) {
		_ = tx.AddError(errors.New("simulierter Speicherfehler"))
	}); err != nil {
		t.Fatalf("Callback konnte nicht registriert werden: %v", err)
	}

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method+": Speicherfehler liefert 500 und lässt den Token unverändert", func(t *testing.T) {
			requireStatusCode(t, doWebhookTokenRequest(r, method, pageIDStr), http.StatusInternalServerError)

			var fresh models.StatusPage
			db.First(&fresh, page.ID)
			if fresh.WebhookTokenHash != models.HashToken("bestehend") {
				t.Errorf("Token sollte unverändert bleiben, ist aber %q", fresh.WebhookTokenHash)
			}
		})
	}
}
