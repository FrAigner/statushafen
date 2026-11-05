package externalstatus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Aufbewahrung der Admin-Nachrichten (90 Tage seit dem letzten Auftreten).

func (f *incFixture) notif(t *testing.T, daysOld int) models.AdminNotification {
	t.Helper()
	seen := f.now.Add(-time.Duration(daysOld) * 24 * time.Hour)
	n := models.AdminNotification{Kind: "x", Severity: "info", TitleKey: "t", Params: "{}", Count: 1, FirstSeenAt: seen, LastSeenAt: seen}
	require.NoError(t, f.db.Create(&n).Error)
	return n
}

func TestPruneAdminNotifications_EntferntNurAlteSamtLesemarken(t *testing.T) {
	f := newIncFixture(t)
	old := f.notif(t, 91)
	f.notif(t, 89)
	f.notif(t, 0)
	user := models.User{Email: "a@example.com"}
	require.NoError(t, f.db.Create(&user).Error)
	require.NoError(t, f.db.Create(&models.NotificationRead{NotificationID: old.ID, UserID: user.ID, ReadAt: f.now}).Error)

	n, err := f.svc.PruneAdminNotifications(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.EqualValues(t, 2, f.countAll(t, &models.AdminNotification{}))
	assert.EqualValues(t, 0, f.countAll(t, &models.NotificationRead{}), "Lesemarken der gelöschten Nachricht")
}

func TestPruneAdminNotifications_ArbeitetInBatchesUndMeldetFehler(t *testing.T) {
	f := newIncFixture(t)
	for i := 0; i < pruneBatchSize+5; i++ {
		f.notif(t, 100)
	}
	n, err := f.svc.PruneAdminNotifications(context.Background())
	require.NoError(t, err)
	assert.Equal(t, pruneBatchSize+5, n)

	require.NoError(t, f.db.Migrator().DropTable(&models.AdminNotification{}))
	_, err = f.svc.PruneAdminNotifications(context.Background())
	assert.Error(t, err)
	assert.NotPanics(t, func() { f.svc.pruneAdminNotifications(context.Background()) })
}

func TestMaybePrune_RaeumtAuchAdminNachrichtenAuf(t *testing.T) {
	f := newIncFixture(t)
	f.notif(t, 120)
	f.svc.maybePrune(context.Background())
	assert.EqualValues(t, 0, f.countAll(t, &models.AdminNotification{}))
}

func TestPruneAdminNotifications_FehlerBeimLoeschenUndAbbruch(t *testing.T) {
	f := newIncFixture(t)
	f.notif(t, 100)

	// Abgebrochener Kontext: kein Durchlauf.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := f.svc.PruneAdminNotifications(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)

	// Löschen der Nachricht scheitert: Fehler wird gemeldet, nichts gezählt.
	require.NoError(t, f.db.Callback().Delete().Before("gorm:delete").Register("an_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "admin_notifications" {
			_ = tx.AddError(errors.New("boom"))
		}
	}))
	n, err = f.svc.PruneAdminNotifications(context.Background())
	assert.Error(t, err)
	assert.Zero(t, n)

	// Löschen der Lesemarken scheitert.
	require.NoError(t, f.db.Migrator().DropTable(&models.NotificationRead{}))
	_, err = f.svc.PruneAdminNotifications(context.Background())
	assert.Error(t, err)
}
