package handlers

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func TestCreateOrderHandler_RejectsInvalidSlug(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	c, w := pageTestContext(http.MethodPost, `{"company_name":"Acme","email":"owner@example.com","slug":"Kein Slug!"}`, nil)
	h.CreateOrderHandler(c)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"slug_invalid"`)
	var n int64
	db.Model(&models.PageOrder{}).Count(&n)
	assert.Zero(t, n, "keine Bestellung gespeichert")
}

// Eine Empfängeradresse, die sich nicht parsen lässt, bricht den Versand ab,
// ohne dass eine SMTP-Verbindung aufgebaut wird (der Server in der Konfiguration
// existiert nicht).
func TestSendOrderConfirmationEmail_InvalidRecipientIsSkipped(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := NewHandler(testutils.SetupTestDB())
	start := time.Now()
	h.sendOrderConfirmationEmail(smtpConfig{Host: "mail.invalid", Port: "25", From: "alerts@example.com"},
		models.PageOrder{Email: "keine adresse", CompanyName: "Acme"}, "geheimes-token-example")

	assert.Less(t, time.Since(start), time.Second)
	assert.Contains(t, buf.String(), "invalid recipient address")
	assert.NotContains(t, buf.String(), "geheimes-token-example")
}

func TestConfirmOrderHandler_StorageErrorsAreRolledBack(t *testing.T) {
	cases := []struct {
		name      string
		table     string
		wantCode  int
		wantPages int64
	}{
		{"Statusseite anlegen scheitert", "status_pages", http.StatusInternalServerError, 0},
		{"Rollenzuordnung scheitert", "user_status_page_roles", http.StatusInternalServerError, 0},
		{"Standardsprachen scheitern, Bestellung gelingt trotzdem", "status_page_languages", http.StatusOK, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			h := NewHandler(db)
			require.NoError(t, db.Create(&models.Role{Name: "Page Admin", Permissions: "pages:read"}).Error)
			expiry := time.Now().Add(time.Hour)
			order := models.PageOrder{CompanyName: "Acme", Email: "owner-tx@example.com", Slug: "acme-tx", Status: "pending_confirmation",
				ConfirmTokenHash: models.HashToken("bestaetigung-token-example"), ConfirmTokenExpiry: &expiry}
			require.NoError(t, db.Create(&order).Error)
			remove := injectDBError(t, db, "create", tc.table, 0)

			c, w := pageTestContext(http.MethodPost, `{"token":"bestaetigung-token-example"}`, nil)
			h.ConfirmOrderHandler(c)
			remove()

			assert.Equal(t, tc.wantCode, w.Code, w.Body.String())
			assertTxOutcome(t, db, tc.wantPages)
			var reloaded models.PageOrder
			require.NoError(t, db.First(&reloaded, order.ID).Error)
			if tc.wantPages == 0 {
				assert.Equal(t, "pending_confirmation", reloaded.Status, "Bestellung bleibt unverändert")
				assert.Equal(t, models.HashToken("bestaetigung-token-example"), reloaded.ConfirmTokenHash)
			} else {
				assert.Equal(t, "confirmed", reloaded.Status)
			}
		})
	}
}

// assertTxOutcome prüft, dass bei einem Fehler weder Seite noch Konto zurückbleiben.
func assertTxOutcome(t *testing.T, db *gorm.DB, wantPages int64) {
	t.Helper()
	var pages, users int64
	require.NoError(t, db.Model(&models.StatusPage{}).Count(&pages).Error)
	require.NoError(t, db.Model(&models.User{}).Count(&users).Error)
	assert.Equal(t, wantPages, pages)
	assert.Equal(t, wantPages, users)
}
