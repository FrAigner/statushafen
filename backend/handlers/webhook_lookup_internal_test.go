package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ein Datenbankfehler beim Nachschlagen von Komponente oder Seite bricht den
// Webhook ab (fail closed): 500, ohne Status zu ändern und ohne zu verraten,
// ob die Komponente existiert.
func TestComponentStatusWebhook_DatenbankfehlerBrichtAb(t *testing.T) {
	cases := map[string]string{"Komponente": "components", "Statusseite": "status_pages"}
	for name, table := range cases {
		t.Run(name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			h := NewHandler(db)
			r := testutils.SetupRouter()
			r.POST("/api/webhooks/components/:component_id/status", h.ComponentStatusWebhookHandler)
			page := models.StatusPage{Name: "Acme", Slug: "acme-webhook-dbfail", WebhookTokenHash: models.HashToken("geheimes-token-nur-fuer-tests")}
			require.NoError(t, db.Create(&page).Error)
			comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
			require.NoError(t, db.Create(&comp).Error)

			remove := injectDBError(t, db, "query", table, 0)
			req := httptest.NewRequest(http.MethodPost, "/api/webhooks/components/"+strconv.Itoa(int(comp.ID))+"/status",
				strings.NewReader(`{"status":"major_outage"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Webhook-Token", "geheimes-token-nur-fuer-tests")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			remove()

			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"code":"server_error"`)
			var fresh models.Component
			require.NoError(t, db.First(&fresh, comp.ID).Error)
			assert.Equal(t, "operational", fresh.Status)
		})
	}
}
