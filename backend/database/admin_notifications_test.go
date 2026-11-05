package database

import (
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Indizes des Nachrichtencenters (SQLite; Postgres siehe pruefeAdminNotifications).

func TestCreateAdminNotificationIndexes_EineOffeneMeldungJeSchluessel(t *testing.T) {
	db := newLoggingTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.AdminNotification{}, &models.NotificationRead{}))
	CreateAdminNotificationIndexes(db)
	CreateAdminNotificationIndexes(db) // idempotent

	now := time.Now()
	mk := func(key string) models.AdminNotification {
		return models.AdminNotification{Kind: "x", Severity: "info", TitleKey: "t", Params: "{}", DedupKey: key, Count: 1, FirstSeenAt: now, LastSeenAt: now}
	}
	a := mk("p1:k")
	require.NoError(t, db.Create(&a).Error)
	dup := mk("p1:k")
	assert.Error(t, db.Create(&dup).Error, "zweite offene Meldung mit gleichem Schlüssel")

	// Leerer Schlüssel (erledigt, abgelöst, ohne Zusammenfassung): beliebig oft.
	for i := 0; i < 3; i++ {
		e := mk("")
		require.NoError(t, db.Create(&e).Error)
	}
	// Nach dem Ablösen ist der Schlüssel wieder frei.
	require.NoError(t, db.Model(&a).Update("dedup_key", "").Error)
	again := mk("p1:k")
	require.NoError(t, db.Create(&again).Error)

	user := models.User{Email: "u@example.com"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&models.NotificationRead{NotificationID: a.ID, UserID: user.ID, ReadAt: now}).Error)
	assert.Error(t, db.Create(&models.NotificationRead{NotificationID: a.ID, UserID: user.ID, ReadAt: now}).Error, "ein Lesestatus je Nachricht und Benutzer")
}
