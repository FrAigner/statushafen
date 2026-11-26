package handlers

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// If one of the two count queries fails, creation is rejected (fail
// closed): the error is returned as an ordinary error, not as a
// rate limit and not as a silent "allowed".
func TestCheckAdminSubscriberLimits_DBErrorsFailClosed(t *testing.T) {
	for name, skip := range map[string]int{"Zählung je Seite": 0, "Zählung je Empfänger": 1} {
		t.Run(name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			h := NewHandler(db)
			injectDBError(t, db, "query", "subscribers", skip)

			err := h.checkAdminSubscriberLimits(1, "reader@example.org", time.Now())

			require.Error(t, err)
			assert.Equal(t, "", TemplateErrorCode(err), "kein Ratenlimit-Fehlercode")
			assert.NotContains(t, err.Error(), "reader@example.org")
		})
	}
}

// Counter-check to the error paths: without an error and under the limits,
// creation is allowed; per recipient only unconfirmed subscriptions of the last 24 hours count.
func TestCheckAdminSubscriberLimits_PerRecipientLimit(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	now := time.Now()
	require.NoError(t, h.checkAdminSubscriberLimits(1, "reader@example.org", now))

	for i := 0; i < maxPendingSubscriptionsPerRecipient; i++ {
		require.NoError(t, db.Create(&models.Subscriber{
			StatusPageID: uint(i + 10), Channel: "email", Email: "Reader@Example.org",
			Status: models.SubscriberStatusPending,
		}).Error)
	}
	err := h.checkAdminSubscriberLimits(1, "reader@example.org", now)
	require.Error(t, err)
	assert.Equal(t, "subscriber_rate_limited", TemplateErrorCode(err))

	// Outside the 24-hour window they no longer count.
	require.NoError(t, h.checkAdminSubscriberLimits(1, "reader@example.org", now.Add(25*time.Hour)))
}

// Deleted rows count too: otherwise the limit could be bypassed by creating and
// immediately deleting subscriptions, and every creation would have triggered a
// confirmation mail to a third-party address.
func TestCheckAdminSubscriberLimits_SoftDeletedRowsStillCount(t *testing.T) {
	now := time.Now()

	t.Run("je Seite", func(t *testing.T) {
		db := testutils.SetupTestDB()
		h := NewHandler(db)
		for i := 0; i < MaxAdminSubscriberCreatesPerHour; i++ {
			sub := models.Subscriber{StatusPageID: 1, Channel: "email", Email: fmt.Sprintf("leser%d@example.org", i), Status: models.SubscriberStatusActive}
			require.NoError(t, db.Create(&sub).Error)
			require.NoError(t, db.Delete(&sub).Error)
		}
		err := h.checkAdminSubscriberLimits(1, "neu@example.org", now)
		require.Error(t, err)
		assert.Equal(t, "subscriber_rate_limited", TemplateErrorCode(err))
	})

	t.Run("je Empfänger", func(t *testing.T) {
		db := testutils.SetupTestDB()
		h := NewHandler(db)
		for i := 0; i < maxPendingSubscriptionsPerRecipient; i++ {
			sub := models.Subscriber{StatusPageID: uint(i + 10), Channel: "email", Email: "leser@example.org", Status: models.SubscriberStatusPending}
			require.NoError(t, db.Create(&sub).Error)
			require.NoError(t, db.Delete(&sub).Error)
		}
		err := h.checkAdminSubscriberLimits(1, "leser@example.org", now)
		require.Error(t, err)
		assert.Equal(t, "subscriber_rate_limited", TemplateErrorCode(err))
	})
}
