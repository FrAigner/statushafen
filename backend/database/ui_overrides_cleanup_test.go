package database

import (
	"path/filepath"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Leere Altfälle verschwinden, echte Overrides (auch anderer
// Seiten/Sprachen) bleiben, ein zweiter Lauf ändert nichts mehr.
func TestRemoveEmptyUiOverrides_KeepsRealOverrides(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ui.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.UiTranslationOverride{}))

	pageID := uint(1)
	rows := []models.UiTranslationOverride{
		{LanguageCode: "de", Key: "nav.profile", Value: ""},
		{LanguageCode: "en", Key: "nav.profile", Value: "  "},
		{LanguageCode: "de", Key: "nav.home", Value: "Start"},
		{StatusPageID: &pageID, LanguageCode: "de", Key: "nav.profile", Value: "Mein Profil"},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}

	out := captureLog(t)
	removeEmptyUiOverrides(db)
	removeEmptyUiOverrides(db)
	assert.NotContains(t, out.String(), "Failed to remove")

	var left []models.UiTranslationOverride
	require.NoError(t, db.Order("id").Find(&left).Error)
	require.Len(t, left, 2)
	assert.Equal(t, "Start", left[0].Value)
	assert.Equal(t, "Mein Profil", left[1].Value)
}

func TestRemoveEmptyUiOverrides_ErrorIsNotFatal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "empty.db")), &gorm.Config{})
	require.NoError(t, err)
	out := captureLog(t)
	removeEmptyUiOverrides(db)
	assert.Contains(t, out.String(), "Failed to remove empty ui overrides")
}
