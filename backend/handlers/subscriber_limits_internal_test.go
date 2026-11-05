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

// Scheitert eine der beiden Zählabfragen, wird das Anlegen abgelehnt (fail
// closed): Der Fehler kommt als gewöhnlicher Fehler zurück, nicht als
// Ratenbegrenzung und nicht als stilles "erlaubt".
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

// Gegenprobe zu den Fehlerpfaden: Ohne Fehler und unter den Grenzen ist das
// Anlegen erlaubt; je Empfänger zählen nur unbestätigte Abos der letzten 24 Stunden.
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

	// Außerhalb des 24-Stunden-Fensters zählen sie nicht mehr.
	require.NoError(t, h.checkAdminSubscriberLimits(1, "reader@example.org", now.Add(25*time.Hour)))
}

// Gelöschte Zeilen zählen mit: Sonst ließe sich die Grenze durch Anlegen und
// sofortiges Löschen von Abos umgehen, und jedes Anlegen hätte eine
// Bestätigungsmail an eine fremde Adresse ausgelöst.
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
