package database

import (
	"path/filepath"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupLanguageTestDB baut eine Minimal-DB nur mit den für den Sprachkatalog
// nötigen Modellen auf - analog zu setupJoinTableTestDB.
func setupLanguageTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbName := filepath.Join(t.TempDir(), "language.db")
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(&models.StatusPage{}, &models.Language{}, &models.StatusPageLanguage{})
	require.NoError(t, err)
	createLanguageUniqueIndexes(db)
	return db
}

func TestSeedLanguageCatalog_IdempotentAndPreservesEdits(t *testing.T) {
	db := setupLanguageTestDB(t)

	seedLanguageCatalog(db)
	var count int64
	require.NoError(t, db.Model(&models.Language{}).Count(&count).Error)
	if count != int64(len(defaultLanguages)) {
		t.Fatalf("expected %d seeded languages, got %d", len(defaultLanguages), count)
	}

	// Ein Admin benennt "en" um - ein zweiter seedLanguageCatalog-Lauf (wie
	// bei jedem InitDB()) darf das nicht überschreiben.
	require.NoError(t, db.Model(&models.Language{}).Where("code = ?", "en").Update("name", "Custom English Name").Error)
	seedLanguageCatalog(db)

	var lang models.Language
	require.NoError(t, db.Where("code = ?", "en").First(&lang).Error)
	if lang.Name != "Custom English Name" {
		t.Fatalf("expected seedLanguageCatalog to preserve admin edit, got name %q", lang.Name)
	}
	require.NoError(t, db.Model(&models.Language{}).Count(&count).Error)
	if count != int64(len(defaultLanguages)) {
		t.Fatalf("expected seedLanguageCatalog to stay idempotent, got %d languages", count)
	}
}

// Die Betreiber-Liste (31.10.2026): alle 30 Sprachen mit Eigenbezeichnung und ISO-639-1-Code,
// jeweils aktiv; ein vorhandener Katalog (nur de/en wie in Prod) wird nur ergänzt.
func TestSeedLanguageCatalog_ContainsOperatorList(t *testing.T) {
	want := map[string]string{
		"bs": "bosanski", "bg": "български", "da": "dansk", "de": "Deutsch", "en": "English", "et": "eesti",
		"fi": "suomi", "fr": "Français", "el": "ελληνικά", "ga": "Gaeilge", "it": "Italiano", "hr": "hrvatski",
		"lv": "latviešu", "lt": "lietuvių", "lb": "Lëtzebuergesch", "mt": "Malti", "mk": "македонски",
		"nl": "Nederlands", "pl": "polski", "pt": "português", "rm": "rumantsch", "ro": "română", "sv": "svenska",
		"sr": "српски", "sk": "slovenčina", "sl": "slovenščina", "es": "español", "cs": "čeština", "tr": "Türkçe", "hu": "magyar",
	}
	db := setupLanguageTestDB(t)
	require.NoError(t, db.Create(&models.Language{Code: "de", Name: "Deutsch", IsActive: true}).Error)
	require.NoError(t, db.Create(&models.Language{Code: "en", Name: "English", IsActive: true}).Error)

	seedLanguageCatalog(db)

	var langs []models.Language
	require.NoError(t, db.Find(&langs).Error)
	got := map[string]models.Language{}
	for _, l := range langs {
		got[l.Code] = l
	}
	require.Len(t, got, len(want))
	for code, name := range want {
		l, ok := got[code]
		require.True(t, ok, "Sprache %s fehlt im Katalog", code)
		require.Equal(t, name, l.Name, code)
		require.True(t, l.IsActive, code)
	}
}

// Eine vom Admin abgeschaltete Sprache wird beim Start nicht wieder eingeschaltet.
func TestSeedLanguageCatalog_KeepsDisabledLanguageDisabled(t *testing.T) {
	db := setupLanguageTestDB(t)
	seedLanguageCatalog(db)
	require.NoError(t, db.Model(&models.Language{}).Where("code = ?", "fi").Update("is_active", false).Error)

	seedLanguageCatalog(db)

	var l models.Language
	require.NoError(t, db.Where("code = ?", "fi").First(&l).Error)
	require.False(t, l.IsActive)
}

func TestBackfillStatusPageLanguages(t *testing.T) {
	db := setupLanguageTestDB(t)

	page := models.StatusPage{Name: "Bestandsseite", Slug: "bestandsseite"}
	require.NoError(t, db.Create(&page).Error)

	backfillStatusPageLanguages(db)

	var entries []models.StatusPageLanguage
	require.NoError(t, db.Where("status_page_id = ?", page.ID).Order("language_code asc").Find(&entries).Error)
	if len(entries) != 2 {
		t.Fatalf("expected de+en backfilled for existing page, got %+v", entries)
	}
	for _, e := range entries {
		if e.LanguageCode == "de" && !e.IsDefault {
			t.Errorf("expected de to be the default language (matches the old subscriber-mail convention), got %+v", e)
		}
		if e.LanguageCode == "en" && e.IsDefault {
			t.Errorf("expected en to NOT be default, got %+v", e)
		}
	}

	// Ein zweiter Lauf darf eine bereits konfigurierte Seite nicht anfassen -
	// sonst würde ein Admin, der die Sprachauswahl inzwischen manuell
	// geändert hat, bei jedem Serverstart zurückgesetzt.
	require.NoError(t, db.Where("status_page_id = ? AND language_code = ?", page.ID, "en").Delete(&models.StatusPageLanguage{}).Error)
	backfillStatusPageLanguages(db)
	var afterCount int64
	require.NoError(t, db.Model(&models.StatusPageLanguage{}).Where("status_page_id = ?", page.ID).Count(&afterCount).Error)
	if afterCount != 1 {
		t.Fatalf("expected backfill to skip a page that already has language rows, got %d rows", afterCount)
	}
}

func TestSeedDefaultStatusPageLanguages(t *testing.T) {
	db := setupLanguageTestDB(t)
	page := models.StatusPage{Name: "Neue Seite", Slug: "neue-seite"}
	require.NoError(t, db.Create(&page).Error)

	require.NoError(t, SeedDefaultStatusPageLanguages(db, page.ID))

	var entries []models.StatusPageLanguage
	require.NoError(t, db.Where("status_page_id = ?", page.ID).Find(&entries).Error)
	if len(entries) != 2 {
		t.Fatalf("expected de+en for a newly created page, got %+v", entries)
	}
}

// Eine vom Admin gelöschte (soft-gelöschte) Sprache darf der Seed beim nächsten
// Start nicht wieder anlegen.
func TestSeedLanguageCatalog_BringtGeloeschteSpracheNichtZurueck(t *testing.T) {
	db := setupLanguageTestDB(t)
	seedLanguageCatalog(db)
	require.NoError(t, db.Where("code = ?", "tr").Delete(&models.Language{}).Error)

	seedLanguageCatalog(db)

	var active int64
	require.NoError(t, db.Model(&models.Language{}).Where("code = ?", "tr").Count(&active).Error)
	require.Zero(t, active, "gelöschte Sprache darf nicht wiederkehren")
	var all int64
	require.NoError(t, db.Unscoped().Model(&models.Language{}).Where("code = ?", "tr").Count(&all).Error)
	require.EqualValues(t, 1, all, "keine zweite Zeile")
}
