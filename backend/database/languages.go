package database

import (
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

// defaultLanguages ist der Standard-Sprachkatalog: Name = Eigenbezeichnung, Code =
// ISO 639-1. Inhaltssprachen gibt es nur noch auf Deutsch und Englisch; die
// früher zusätzlich geseedeten europäischen Sprachen entfallen.
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
