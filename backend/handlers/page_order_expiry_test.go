package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A never-confirmed order releases slug and address again after the
// confirmation link expires, even if nobody redeems the link.
func TestOrderCreate_ExpiredPendingOrderReleasesSlug(t *testing.T) {
	db, r := newInputValidationRouter(t)
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&models.PageOrder{CompanyName: "Alt", Email: "alt@example.com", Slug: "alter-slug",
		Status: "pending_confirmation", ConfirmTokenHash: models.HashToken("t-alt"), ConfirmTokenExpiry: &past}).Error)
	require.NoError(t, db.Create(&models.PageOrder{CompanyName: "Frisch", Email: "frisch@example.com", Slug: "frischer-slug",
		Status: "pending_confirmation", ConfirmTokenHash: models.HashToken("t-frisch"), ConfirmTokenExpiry: &future}).Error)

	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]any{"company_name": "Neu", "email": "neu@example.com", "slug": "alter-slug"})
	assert.Equal(t, http.StatusCreated, w.Code, "abgelaufene Bestellung sperrt den Slug nicht mehr: %s", w.Body.String())

	// Control case: a valid open order continues to block.
	w = doStatusPageRequest(r, "POST", "/api/public/orders", map[string]any{"company_name": "Neu", "email": "neu2@example.com", "slug": "frischer-slug"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "slug_taken_order")
}

func TestOrderCreate_ExpiredPendingOrdersDoNotCountTowardsCap(t *testing.T) {
	db, r := newInputValidationRouter(t)
	past := time.Now().Add(-time.Hour)
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Create(&models.PageOrder{CompanyName: "Alt", Email: "opfer@example.com",
			Status: "pending_confirmation", ConfirmTokenHash: models.HashToken("t-abgelaufen-" + itoa(uint(i))), ConfirmTokenExpiry: &past}).Error)
	}
	// An order without an expiry time counts as expired, as in the confirmation handler.
	require.NoError(t, db.Create(&models.PageOrder{CompanyName: "Ohne", Email: "opfer@example.com", Status: "pending_confirmation", ConfirmTokenHash: models.HashToken("t-ohne")}).Error)

	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]any{"company_name": "Neu", "email": "opfer@example.com"})
	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	// Control case: three valid open orders reach the limit.
	future := time.Now().Add(time.Hour)
	for i := 0; i < 2; i++ { // the one just created is the first
		require.NoError(t, db.Create(&models.PageOrder{CompanyName: "Offen", Email: "opfer@example.com",
			Status: "pending_confirmation", ConfirmTokenHash: models.HashToken("t-offen-" + itoa(uint(i))), ConfirmTokenExpiry: &future}).Error)
	}
	w = doStatusPageRequest(r, "POST", "/api/public/orders", map[string]any{"company_name": "Neu", "email": "opfer@example.com"})
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "too_many_orders")
}
