package database

import (
	"log/slog"
	"slices"

	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// createLanguageUniqueIndexes legt den partiellen Unique-Index
// an, der sicherstellt, dass eine Statusseite höchstens eine
// Default-Sprache hat (die eigentliche Eindeutigkeit von (StatusPageID,
// LanguageCode) selbst übernimmt bereits createJoinTableUniqueIndexes).
// GORMs AutoMigrate kann keine partiellen Indizes erzeugen (siehe
// CreateSoftDeleteUniqueIndexes), daher Raw-SQL. Idempotent per IF NOT EXISTS.
func createLanguageUniqueIndexes(db *gorm.DB) {
	sql := `CREATE UNIQUE INDEX IF NOT EXISTS idx_statuspage_language_default ON status_page_languages (status_page_id) WHERE is_default = true AND deleted_at IS NULL`
	if err := db.Exec(sql).Error; err != nil {
		logging.Fatal("Failed to create idx_statuspage_language_default", "error", err)
	}
}

// ContentLanguageCodes are the only content languages (incidents, maintenance,
// notices, components, templates, notifications): German and English. Other
// languages are neither created nor offered nor delivered. The UI language of the
// interface (frontend/src/i18n, User.PreferredLanguage) is independent of this.
var ContentLanguageCodes = []string{"de", "en"}

// IsContentLanguage reports whether code is one of the content languages.
func IsContentLanguage(code string) bool {
	return slices.Contains(ContentLanguageCodes, code)
}

// defaultLanguages is the default language catalog: Name = native name, Code =
// ISO 639-1. Content exists only in German and English; the additional European
// languages that used to be seeded are gone.
var defaultLanguages = []models.Language{
	{Code: "en", Name: "English", IsActive: true},
	{Code: "de", Name: "Deutsch", IsActive: true},
}

// seedLanguageCatalog legt den globalen Sprachkatalog an (defaultLanguages).
// Idempotent: existiert ein Code bereits, wird er nicht erneut angelegt oder überschrieben
// (ein Admin könnte den Namen oder die Aktivierung zwischenzeitlich angepasst haben, auch
// eine abgeschaltete Sprache bleibt abgeschaltet, eine gelöschte bleibt gelöscht). Weitere Sprachen legt ein Admin über die
// /api/languages-Endpunkte an.
func seedLanguageCatalog(db *gorm.DB) {
	for _, lang := range defaultLanguages {
		var count int64
		// Unscoped, damit eine vom Admin gelöschte Sprache (Soft-Delete) nicht
		// bei jedem Start wiederkehrt.
		if err := db.Unscoped().Model(&models.Language{}).Where("code = ?", lang.Code).Count(&count).Error; err != nil {
			logging.Fatal("Failed to check for existing language", "language", lang.Code, "error", err)
		}
		if count > 0 {
			continue
		}
		if err := db.Create(&lang).Error; err != nil {
			logging.Fatal("Failed to seed language", "language", lang.Code, "error", err)
		}
	}
}

// backfillStatusPageLanguages aktiviert für jede Statusseite,
// die noch keine einzige StatusPageLanguage-Zeile hat, dieselben zwei
// Sprachen mit derselben Default-Wahl, die vorher implizit durch die
// hartcodierte NotificationTemplate-Konvention galt: de+en, de als Default
// (Subscriber-Mails liefen bisher immer auf Deutsch).
// Bewusst ohne Sonderbehandlung der System-Statusseite - sie ist ein Tenant
// wie jeder andere (siehe SeedSystemStatusPage).
func backfillStatusPageLanguages(db *gorm.DB) {
	var pages []models.StatusPage
	if err := db.Find(&pages).Error; err != nil {
		logging.Fatal("Failed to load status pages for language backfill", "error", err)
	}
	for _, page := range pages {
		var count int64
		if err := db.Model(&models.StatusPageLanguage{}).Where("status_page_id = ?", page.ID).Count(&count).Error; err != nil {
			logging.Fatal("Failed to check existing StatusPageLanguage", "status_page_id", page.ID, "error", err)
		}
		if count > 0 {
			continue
		}
		entries := []models.StatusPageLanguage{
			{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true},
			{StatusPageID: page.ID, LanguageCode: "en", IsDefault: false},
		}
		for _, entry := range entries {
			if err := db.Create(&entry).Error; err != nil {
				logging.Fatal("Failed to backfill StatusPageLanguage", "status_page_id", page.ID, "error", err)
			}
		}
	}
}

// DefaultStatusPageLanguages liefert die Sprachen, mit denen
// eine neu angelegte Statusseite ausgestattet wird - dieselbe de+en/de-
// Default-Kombination wie der Backfill für Bestandsseiten
// (backfillStatusPageLanguages), damit sich neue und bestehende Seiten
// direkt nach der Migration nicht unterscheiden.
func SeedDefaultStatusPageLanguages(db *gorm.DB, statusPageID uint) error {
	entries := []models.StatusPageLanguage{
		{StatusPageID: statusPageID, LanguageCode: "de", IsDefault: true},
		{StatusPageID: statusPageID, LanguageCode: "en", IsDefault: false},
	}
	for _, entry := range entries {
		if err := db.Create(&entry).Error; err != nil {
			return err
		}
	}
	return nil
}

// deactivateUnsupportedLanguages switches off, at startup, every catalog language
// that is no longer a content language (left over from the former 30-language
// catalog or created by an admin). Non-destructive: the rows, their status page
// assignments (StatusPageLanguage) and stored translations stay in the database but
// are no longer offered anywhere. Idempotent (only active rows are updated).
func deactivateUnsupportedLanguages(db *gorm.DB) {
	res := db.Model(&models.Language{}).Where("code NOT IN ? AND is_active = ?", ContentLanguageCodes, true).Update("is_active", false)
	if res.Error != nil {
		logging.Fatal("Failed to deactivate unsupported languages", "error", res.Error)
	}
	if res.RowsAffected > 0 {
		slog.Info("Language catalog: deactivated languages other than German and English", "count", res.RowsAffected)
	}
}

// fallbackUnsupportedDefaultLanguages switches status pages whose default language
// is no longer a content language to German. The previous assignment stays as an
// additional (not offered) language; a missing de assignment is created. One
// transaction per page so the "exactly one default" unique index is never violated.
// Idempotent: afterwards every page has a de or en default.
func fallbackUnsupportedDefaultLanguages(db *gorm.DB) {
	var rows []models.StatusPageLanguage
	if err := db.Where("is_default = ? AND language_code NOT IN ?", true, ContentLanguageCodes).Find(&rows).Error; err != nil {
		logging.Fatal("Failed to load unsupported default languages", "error", err)
	}
	for _, row := range rows {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.StatusPageLanguage{}).Where("id = ?", row.ID).Update("is_default", false).Error; err != nil {
				return err
			}
			var de models.StatusPageLanguage
			err := tx.Where("status_page_id = ? AND language_code = ?", row.StatusPageID, "de").Limit(1).Find(&de).Error
			if err != nil {
				return err
			}
			if de.ID == 0 {
				return tx.Create(&models.StatusPageLanguage{StatusPageID: row.StatusPageID, LanguageCode: "de", IsDefault: true}).Error
			}
			return tx.Model(&de).Update("is_default", true).Error
		})
		if err != nil {
			logging.Fatal("Failed to fall back to German default language", "status_page_id", row.StatusPageID, "error", err)
		}
		slog.Warn("Default language is no longer supported, status page falls back to German",
			"status_page_id", row.StatusPageID, "previous_language", row.LanguageCode)
	}
}
