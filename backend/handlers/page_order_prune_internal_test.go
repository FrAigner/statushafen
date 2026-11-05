package handlers

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneExpiredOrders(t *testing.T) {
	db := testutils.SetupTestDB()
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	mk := func(email, status, token string, expiry *time.Time) models.PageOrder {
		o := models.PageOrder{CompanyName: "X", Email: email, Status: status, ConfirmTokenHash: models.HashToken(token), ConfirmTokenExpiry: expiry}
		require.NoError(t, db.Create(&o).Error)
		return o
	}
	stale := mk("stale@example.com", "pending_confirmation", "t1", &past)
	noExpiry := mk("noexp@example.com", "pending_confirmation", "t2", nil)
	valid := mk("valid@example.com", "pending_confirmation", "t3", &future)
	confirmed := mk("conf@example.com", "confirmed", "", &past)
	active := mk("act@example.com", "active", "", nil)
	oldExpired := mk("old@example.com", "expired", "", nil)
	freshExpired := mk("fresh@example.com", "expired", "", nil)
	require.NoError(t, db.Model(&models.PageOrder{}).Where("id = ?", oldExpired.ID).
		UpdateColumn("updated_at", now.Add(-orderExpiredRetention-time.Hour)).Error)

	marked, deleted, err := pruneExpiredOrders(db, now)
	require.NoError(t, err)
	assert.EqualValues(t, 2, marked)
	assert.EqualValues(t, 1, deleted)

	status := func(id uint) string {
		var o models.PageOrder
		if err := db.Unscoped().First(&o, id).Error; err != nil {
			return "gelöscht"
		}
		return o.Status
	}
	assert.Equal(t, "expired", status(stale.ID))
	assert.Equal(t, "expired", status(noExpiry.ID))
	assert.Equal(t, "pending_confirmation", status(valid.ID), "gültige Bestellung bleibt")
	assert.Equal(t, "confirmed", status(confirmed.ID))
	assert.Equal(t, "active", status(active.ID))
	assert.Equal(t, "gelöscht", status(oldExpired.ID))
	assert.Equal(t, "expired", status(freshExpired.ID), "junge abgelaufene Bestellung bleibt zunächst")

	var token string
	require.NoError(t, db.Model(&models.PageOrder{}).Where("id = ?", stale.ID).Select("confirm_token").Scan(&token).Error)
	assert.Empty(t, token, "Bestätigungstoken wird mit dem Ablauf entwertet")

	// Idempotent: ein zweiter Lauf ändert nichts mehr.
	marked, deleted, err = pruneExpiredOrders(db, now)
	require.NoError(t, err)
	assert.Zero(t, marked+deleted)
}

func TestMaintenanceTick_PrunesExpiredOrders(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	past := time.Now().Add(-time.Hour)
	o := models.PageOrder{CompanyName: "X", Email: "tick@example.com", Status: "pending_confirmation", ConfirmTokenHash: models.HashToken("t"), ConfirmTokenExpiry: &past}
	require.NoError(t, db.Create(&o).Error)
	h.runMaintenanceTick()
	require.NoError(t, db.First(&o, o.ID).Error)
	assert.Equal(t, "expired", o.Status)
}

func TestPruneExpiredOrders_DatabaseErrorIsReported(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, db.Migrator().DropTable(&models.PageOrder{}))

	_, _, err := pruneExpiredOrders(db, time.Now())
	assert.Error(t, err)

	// Der Wartungs-Tick loggt den Fehler, statt abzubrechen.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	NewHandler(db).pruneExpiredPageOrders(time.Now())
	assert.Contains(t, buf.String(), "Bestellungen: Bereinigung fehlgeschlagen")
}
