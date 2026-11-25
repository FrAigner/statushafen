package database

import (
	"path/filepath"
	"strings"
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

// Content exists only in German and English: the seed creates exactly these two,
// both active and with their native names.
func TestSeedLanguageCatalog_OnlyGermanAndEnglish(t *testing.T) {
	db := setupLanguageTestDB(t)

	seedLanguageCatalog(db)

	var langs []models.Language
	require.NoError(t, db.Order("code").Find(&langs).Error)
	require.Len(t, langs, 2)
	require.Equal(t, "de", langs[0].Code)
	require.Equal(t, "Deutsch", langs[0].Name)
	require.True(t, langs[0].IsActive)
	require.Equal(t, "en", langs[1].Code)
	require.Equal(t, "English", langs[1].Name)
	require.True(t, langs[1].IsActive)
}

// Eine vom Admin abgeschaltete Sprache wird beim Start nicht wieder eingeschaltet.
func TestSeedLanguageCatalog_KeepsDisabledLanguageDisabled(t *testing.T) {
	db := setupLanguageTestDB(t)
	seedLanguageCatalog(db)
	require.NoError(t, db.Model(&models.Language{}).Where("code = ?", "en").Update("is_active", false).Error)

	seedLanguageCatalog(db)

	var l models.Language
	require.NoError(t, db.Where("code = ?", "en").First(&l).Error)
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
	require.NoError(t, db.Where("code = ?", "en").Delete(&models.Language{}).Error)

	seedLanguageCatalog(db)

	var active int64
	require.NoError(t, db.Model(&models.Language{}).Where("code = ?", "en").Count(&active).Error)
	require.Zero(t, active, "gelöschte Sprache darf nicht wiederkehren")
	var all int64
	require.NoError(t, db.Unscoped().Model(&models.Language{}).Where("code = ?", "en").Count(&all).Error)
	require.EqualValues(t, 1, all, "keine zweite Zeile")
}

func TestIsContentLanguage(t *testing.T) {
	require.True(t, IsContentLanguage("de"))
	require.True(t, IsContentLanguage("en"))
	require.False(t, IsContentLanguage("fr"))
	require.False(t, IsContentLanguage(""))
}

// Other catalog languages (from the former 30-language seed or created by an admin)
// are switched off at startup without deleting rows: status page assignments stay.
// Running it twice changes nothing more.
func TestDeactivateUnsupportedLanguages(t *testing.T) {
	db := setupLanguageTestDB(t)
	seedLanguageCatalog(db)
	require.NoError(t, db.Create(&models.Language{Code: "fr", Name: "Français", IsActive: true}).Error)
	require.NoError(t, db.Create(&models.Language{Code: "hr", Name: "hrvatski", IsActive: true}).Error)
	page := models.StatusPage{Name: "P", Slug: "p"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "fr"}).Error)

	deactivateUnsupportedLanguages(db)
	deactivateUnsupportedLanguages(db)

	var langs []models.Language
	require.NoError(t, db.Order("code").Find(&langs).Error)
	active := map[string]bool{}
	for _, l := range langs {
		active[l.Code] = l.IsActive
	}
	require.Equal(t, map[string]bool{"de": true, "en": true, "fr": false, "hr": false}, active)
	var rows int64
	require.NoError(t, db.Model(&models.StatusPageLanguage{}).Where("language_code = ?", "fr").Count(&rows).Error)
	require.EqualValues(t, 1, rows, "assignment is kept")
}

// A status page whose default language is no longer a content language falls back
// to German. The old assignment stays as an additional language, a missing de
// assignment is created, and the switch is logged.
func TestFallbackUnsupportedDefaultLanguages(t *testing.T) {
	db := setupLanguageTestDB(t)
	mk := func(slug string, rows ...models.StatusPageLanguage) uint {
		p := models.StatusPage{Name: slug, Slug: slug}
		require.NoError(t, db.Create(&p).Error)
		for _, r := range rows {
			r.StatusPageID = p.ID
			require.NoError(t, db.Create(&r).Error)
		}
		return p.ID
	}
	withDe := mk("mit-de", models.StatusPageLanguage{LanguageCode: "fr", IsDefault: true}, models.StatusPageLanguage{LanguageCode: "de"})
	withoutDe := mk("ohne-de", models.StatusPageLanguage{LanguageCode: "hr", IsDefault: true}, models.StatusPageLanguage{LanguageCode: "en"})
	english := mk("englisch", models.StatusPageLanguage{LanguageCode: "en", IsDefault: true}, models.StatusPageLanguage{LanguageCode: "fr"})

	out := captureLog(t)
	fallbackUnsupportedDefaultLanguages(db)
	fallbackUnsupportedDefaultLanguages(db)

	defaults := func(pageID uint) map[string]bool {
		var rows []models.StatusPageLanguage
		require.NoError(t, db.Where("status_page_id = ?", pageID).Find(&rows).Error)
		m := map[string]bool{}
		for _, r := range rows {
			m[r.LanguageCode] = r.IsDefault
		}
		return m
	}
	require.Equal(t, map[string]bool{"de": true, "fr": false}, defaults(withDe))
	require.Equal(t, map[string]bool{"de": true, "hr": false, "en": false}, defaults(withoutDe))
	require.Equal(t, map[string]bool{"en": true, "fr": false}, defaults(english))
	log := out.String()
	require.Equal(t, 2, strings.Count(log, "Default language is no longer supported"), log)
	require.Contains(t, log, "hr")
}
