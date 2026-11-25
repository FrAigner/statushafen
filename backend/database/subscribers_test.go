package database

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Existing subscriptions from the time before double opt-in (table without
// status column) must be active after the migration, otherwise they lose
// their notifications.
func TestMigrateSubscriberStatus_LegacyRowsBecomeActive(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy-subs.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE subscribers (id integer primary key autoincrement, created_at datetime, updated_at datetime, deleted_at datetime, status_page_id integer not null, email text, webhook_url text, channel text not null default 'email', subscribed_component_ids text not null default 'all', created_by_user_id integer)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO subscribers (status_page_id, email, channel) VALUES (1, 'alt@example.com', 'email')`).Error)

	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.User{}, &models.Subscriber{}))
	backfillSubscriberStatus(db)

	var statuses []string
	require.NoError(t, db.Model(&models.Subscriber{}).Pluck("status", &statuses).Error)
	assert.Equal(t, []string{models.SubscriberStatusActive}, statuses)
}

// captureLog redirects the slog default logger for the duration of the test into a
// buffer (database only logs via slog now).
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// If the table is missing (or the update fails otherwise), the backfill only logs.
func TestBackfillSubscriberStatus_ErrorIsNotFatal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "empty.db")), &gorm.Config{})
	require.NoError(t, err)
	out := captureLog(t)
	backfillSubscriberStatus(db)
	assert.Contains(t, out.String(), "Failed to backfill subscriber status")
}

// The backfill is idempotent and leaves unconfirmed (pending) subscriptions alone -
// otherwise every restart would undermine double opt-in.
func TestBackfillSubscriberStatus_IdempotentAndKeepsPending(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "subs.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.User{}, &models.Subscriber{}))

	expiry := time.Now().Add(time.Hour)
	legacy := models.Subscriber{StatusPageID: 1, Channel: "email", Email: "legacy@example.com", SubscribedComponentIDs: "all"}
	pending := models.Subscriber{StatusPageID: 1, Channel: "email", Email: "pending@example.com", SubscribedComponentIDs: "all",
		Status: models.SubscriberStatusPending, ConfirmTokenHash: "abc", ConfirmTokenExpiry: &expiry}
	require.NoError(t, db.Create(&legacy).Error)
	require.NoError(t, db.Create(&pending).Error)
	// Recreate legacy data with empty or NULL status (e.g. before a default).
	require.NoError(t, db.Exec(`UPDATE subscribers SET status = '' WHERE id = ?`, legacy.ID).Error)

	out := captureLog(t)
	backfillSubscriberStatus(db)
	backfillSubscriberStatus(db)
	assert.NotContains(t, out.String(), "Failed to backfill")

	var gotLegacy, gotPending models.Subscriber
	require.NoError(t, db.First(&gotLegacy, legacy.ID).Error)
	assert.Equal(t, models.SubscriberStatusActive, gotLegacy.Status)
	require.NoError(t, db.First(&gotPending, pending.ID).Error)
	assert.Equal(t, models.SubscriberStatusPending, gotPending.Status)
	assert.Equal(t, "abc", gotPending.ConfirmTokenHash)
}
