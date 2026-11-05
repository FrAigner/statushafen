package database

import (
	"context"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pruefeAdvisoryLock läuft im Postgres-Migrationstest: Nur eine
// Instanz bekommt die Sperre, nach Unlock die nächste; eine abgerissene
// Verbindung wird neu aufgebaut; Fehler beim Verbindungsaufbau melden false.
func pruefeAdvisoryLock(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	a := NewAdvisoryLock(DB, 4242)
	b := NewAdvisoryLock(DB, 4242)

	ok, err := a.TryLock(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "erste Instanz bekommt die Sperre")
	ok, err = a.TryLock(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "Leader behält die Sperre")
	ok, err = b.TryLock(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "zweite Instanz bekommt sie nicht")
	assert.Nil(t, b.conn, "Verlierer gibt die Verbindung zurück")

	a.Unlock()
	a.Unlock() // zweites Unlock ist ein No-Op
	ok, err = b.TryLock(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "nach Unlock übernimmt die zweite Instanz")

	// Abgerissene Verbindung: Postgres gibt die Sperre frei, TryLock baut neu auf.
	require.NoError(t, b.conn.Close())
	ok, err = b.TryLock(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	b.Unlock()

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	ok, err = NewAdvisoryLock(DB, 4243).TryLock(canceled)
	assert.Error(t, err)
	assert.False(t, ok)
}

// pruefeExternalStatusIndexes: Die partiellen Unique-Indizes für Kopplungen
// und externe Komponenten greifen auf Postgres und lassen ein Neuanlegen
// nach Soft-Delete zu.
func pruefeExternalStatusIndexes(t *testing.T) {
	t.Helper()
	page := models.StatusPage{Name: "Ext", Slug: "ext-507"}
	require.NoError(t, DB.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	require.NoError(t, DB.Create(&comp).Error)
	src := models.ExternalSource{StatusPageID: page.ID, Name: "S", BaseURL: "https://example.com", Provider: "summaryv2", PollIntervalSeconds: 120, Active: true}
	require.NoError(t, DB.Create(&src).Error)
	// neue Ausfall-Spalten - Altzeilen/Inserts ohne Wert bekommen den Standard.
	var stored models.ExternalSource
	require.NoError(t, DB.First(&stored, src.ID).Error)
	assert.Equal(t, 60, stored.OutageAfterMinutes)
	assert.Nil(t, stored.OutageSince)
	assert.Nil(t, stored.OutageNotifiedAt)
	assert.Nil(t, stored.OutageRemindedAt)
	ext := models.ExternalComponent{ExternalSourceID: src.ID, ExternalID: "x", Name: "X"}
	require.NoError(t, DB.Create(&ext).Error)
	assert.Error(t, DB.Create(&models.ExternalComponent{ExternalSourceID: src.ID, ExternalID: "x", Name: "X2"}).Error, "doppelte externe ID")

	link := models.ExternalComponentLink{StatusPageID: page.ID, ComponentID: comp.ID, ExternalComponentID: ext.ID}
	require.NoError(t, DB.Create(&link).Error)
	assert.Error(t, DB.Create(&models.ExternalComponentLink{StatusPageID: page.ID, ComponentID: comp.ID, ExternalComponentID: ext.ID}).Error, "doppelte Kopplung")
	require.NoError(t, DB.Delete(&link).Error)
	require.NoError(t, DB.Create(&models.ExternalComponentLink{StatusPageID: page.ID, ComponentID: comp.ID, ExternalComponentID: ext.ID}).Error, "nach Soft-Delete wieder koppelbar")

	// gespiegelte Vorfälle - dieselbe Garantie für alle drei Tabellen.
	inc := models.ExternalIncident{StatusPageID: page.ID, ExternalSourceID: src.ID, ExternalID: "i1", Title: "T", LastSeenAt: time.Now()}
	require.NoError(t, DB.Create(&inc).Error)
	assert.Error(t, DB.Create(&models.ExternalIncident{StatusPageID: page.ID, ExternalSourceID: src.ID, ExternalID: "i1", Title: "doppelt", LastSeenAt: time.Now()}).Error, "doppelte Meldung je Quelle")
	upd := models.ExternalIncidentUpdate{ExternalIncidentID: inc.ID, ExternalID: "u1", DisplayAt: time.Now()}
	require.NoError(t, DB.Create(&upd).Error)
	assert.Error(t, DB.Create(&models.ExternalIncidentUpdate{ExternalIncidentID: inc.ID, ExternalID: "u1", DisplayAt: time.Now()}).Error, "doppeltes Update")
	icomp := models.ExternalIncidentComponent{ExternalIncidentID: inc.ID, ComponentExternalID: "vm"}
	require.NoError(t, DB.Create(&icomp).Error)
	assert.Error(t, DB.Create(&models.ExternalIncidentComponent{ExternalIncidentID: inc.ID, ComponentExternalID: "vm"}).Error, "doppelte Komponente")
	require.NoError(t, DB.Delete(&icomp).Error)
	require.NoError(t, DB.Create(&models.ExternalIncidentComponent{ExternalIncidentID: inc.ID, ComponentExternalID: "vm"}).Error, "nach Soft-Delete wieder anlegbar")
	require.NoError(t, DB.Delete(&inc).Error)
	require.NoError(t, DB.Create(&models.ExternalIncident{StatusPageID: page.ID, ExternalSourceID: src.ID, ExternalID: "i1", Title: "neu", LastSeenAt: time.Now()}).Error, "nach Soft-Delete wieder anlegbar")
	var stored2 models.ExternalSource
	require.NoError(t, DB.First(&stored2, src.ID).Error)
	assert.Nil(t, stored2.IncidentsSyncedAt, "Bestandsquellen starten still")
	assert.Empty(t, stored2.IncidentsError)
}

// pruefeAdminNotifications: Nachrichtencenter auf echtem Postgres.
// Partieller Unique-Index (offene Meldung je Schlüssel), Lesestatus je Benutzer
// und die FK-Cascades (Seite/Benutzer gelöscht).
func pruefeAdminNotifications(t *testing.T) {
	t.Helper()
	page := models.StatusPage{Name: "Glocke", Slug: "glocke-644"}
	require.NoError(t, DB.Create(&page).Error)
	now := time.Now()
	mk := func(pageID *uint, key string) models.AdminNotification {
		return models.AdminNotification{StatusPageID: pageID, Kind: "x", Severity: "warning", TitleKey: "t", Params: `{"a":1}`, DedupKey: key, GroupKey: key, Count: 1, FirstSeenAt: now, LastSeenAt: now}
	}
	n := mk(&page.ID, "p1:k")
	require.NoError(t, DB.Create(&n).Error)
	dup := mk(&page.ID, "p1:k")
	assert.Error(t, DB.Create(&dup).Error, "zweite offene Meldung je Schlüssel")
	for _, e := range []models.AdminNotification{mk(&page.ID, ""), mk(nil, ""), mk(nil, "")} {
		require.NoError(t, DB.Create(&e).Error, "leerer Schlüssel beliebig oft")
	}

	user := models.User{Email: "glocke-644@example.com"}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&models.NotificationRead{NotificationID: n.ID, UserID: user.ID, ReadAt: now}).Error)
	assert.Error(t, DB.Create(&models.NotificationRead{NotificationID: n.ID, UserID: user.ID, ReadAt: now}).Error, "ein Lesestatus je Nachricht und Benutzer")

	require.NoError(t, DB.Unscoped().Delete(&user).Error)
	var reads int64
	require.NoError(t, DB.Model(&models.NotificationRead{}).Where("notification_id = ?", n.ID).Count(&reads).Error)
	assert.EqualValues(t, 0, reads, "Benutzer gelöscht: Lesemarken weg (ON DELETE CASCADE)")
	require.NoError(t, DB.Unscoped().Delete(&page).Error)
	var left int64
	require.NoError(t, DB.Model(&models.AdminNotification{}).Where("status_page_id = ?", page.ID).Count(&left).Error)
	assert.EqualValues(t, 0, left, "Seite gelöscht: Nachrichten weg (ON DELETE CASCADE)")
}
