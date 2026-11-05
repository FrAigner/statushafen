package database

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/logging/loggingtest"
	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Die früheren log.Fatalf-Pfade laufen jetzt über logging.Fatal
// (slog.Error + Exit 1). Diese Tests lösen jeden Pfad gezielt per injiziertem
// SQL-Fehler aus und prüfen die strukturierte Meldung, ohne den Testprozess
// zu beenden (loggingtest.ExpectFatal).

var errInjected = errors.New("injizierter Fehler")

// failOnSQL lässt jede Anweisung scheitern, deren SQL substr enthält. Die
// Hooks laufen nach dem eigentlichen Callback, weil erst dann das SQL von
// Query/Create/Update gebaut ist.
func failOnSQL(t *testing.T, db *gorm.DB, substr string) {
	t.Helper()
	hook := func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), substr) {
			_ = tx.AddError(errInjected)
		}
	}
	name := "test:fail:" + substr
	cb := db.Callback()
	require.NoError(t, cb.Query().After("gorm:query").Register(name, hook))
	require.NoError(t, cb.Create().After("gorm:create").Register(name, hook))
	require.NoError(t, cb.Update().After("gorm:update").Register(name, hook))
	require.NoError(t, cb.Row().After("gorm:row").Register(name, hook))
	require.NoError(t, cb.Raw().After("gorm:raw").Register(name, hook))
}

// rewriteSQL ersetzt Raw-/Exec-Anweisungen, die substr enthalten, vor der
// Ausführung durch replacement - z.B. Postgres-spezifisches SQL, das SQLite
// nicht kennt.
func rewriteSQL(t *testing.T, db *gorm.DB, substr, replacement string) {
	t.Helper()
	hook := func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), substr) {
			tx.Statement.SQL.Reset()
			tx.Statement.SQL.WriteString(replacement)
			tx.Statement.Vars = nil
		}
	}
	name := "test:rewrite:" + substr
	require.NoError(t, db.Callback().Raw().Before("gorm:raw").Register(name, hook))
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register(name, hook))
}

func newLoggingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logging.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.StatusPage{}, &models.Role{}, &models.User{}, &models.Component{},
		&models.Incident{}, &models.IncidentComponent{}, &models.Tag{},
		&models.ComponentTag{}, &models.IncidentTag{}, &models.UserTag{},
		&models.Language{}, &models.StatusPageLanguage{}, &models.UiTranslationOverride{},
		&models.IncidentUpdate{}, &models.IncidentUpdateTranslation{},
		// Tabellen aus createJoinTableUniqueIndexes.
		&models.ExternalSource{}, &models.ExternalComponent{}, &models.ExternalComponentLink{},
		&models.ExternalIncident{}, &models.ExternalIncidentUpdate{}, &models.ExternalIncidentComponent{},
		&models.UserStatusPageRole{},
		&models.PageInvitation{},
	))
	return db
}

func TestFatalPaths(t *testing.T) {
	cases := []struct {
		name    string
		failSQL string
		prepare func(t *testing.T, db *gorm.DB)
		run     func(db *gorm.DB)
		wantMsg string
	}{
		{"SeedSystemStatusPage: Count", "count(*)", nil, SeedSystemStatusPage, "Failed to check for existing system status page"},
		{"SeedSystemStatusPage: Create Page", "INSERT INTO `status_pages`", nil, SeedSystemStatusPage, "Failed to create system status page"},
		{"SeedSystemStatusPage: Create Component", "INSERT INTO `components`", nil, SeedSystemStatusPage, "Failed to create system status page component"},
		{"migrateIncidentComponents: Insert", "", func(t *testing.T, db *gorm.DB) {
			rewriteSQL(t, db, "information_schema", "SELECT 1")
		}, migrateIncidentComponents, "Failed to migrate incident components"},
		{"migrateIncidentComponents: Drop Column", "", func(t *testing.T, db *gorm.DB) {
			rewriteSQL(t, db, "information_schema", "SELECT 1")
			rewriteSQL(t, db, "INSERT INTO incident_components", "SELECT 1")
		}, migrateIncidentComponents, "Failed to drop legacy component_id column"},
		{"createJoinTableUniqueIndexes: Drop", "DROP INDEX IF EXISTS idx_incident_component", nil, createJoinTableUniqueIndexes, "Failed to drop legacy index"},
		{"createJoinTableUniqueIndexes: Create", "idx_incident_component_active", nil, createJoinTableUniqueIndexes, "Failed to create join table index"},
		{"CreateSoftDeleteUniqueIndexes: Drop", "DROP INDEX IF EXISTS idx_tags_global_name", nil, CreateSoftDeleteUniqueIndexes, "Failed to drop legacy index"},
		// Scheitert das Anlegen eines neuen Index, ist das kein Fatal
		// mehr (Warnung, Altindex bleibt) - siehe TestCreateSoftDeleteUniqueIndexes_KeepsLegacyOnFailure.
		{"SeedSystemStatusPage: Legacy Lookup", "slug = ", nil, SeedSystemStatusPage, "Failed to check for legacy system status page"},
		{"SeedSystemStatusPage: Legacy Mark", "UPDATE `status_pages`", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Create(&models.StatusPage{Name: "S", Slug: SystemStatusPageSlug}).Error)
		}, SeedSystemStatusPage, "Failed to mark legacy system status page"},
		{"findOrCreateTag: Lookup", "FROM `tags`", nil, func(db *gorm.DB) { findOrCreateTag(db, "DE", nil) }, "Failed to look up tag"},
		{"findOrCreateTag: Create", "INSERT INTO `tags`", nil, func(db *gorm.DB) { findOrCreateTag(db, "DE", nil) }, "Failed to create tag"},
		{"createComponentTagIfMissing", "INSERT INTO `component_tags`", nil, func(db *gorm.DB) { createComponentTagIfMissing(db, 1, 2) }, "Failed to create component_tag"},
		{"createIncidentTagIfMissing", "INSERT INTO `incident_tags`", nil, func(db *gorm.DB) { createIncidentTagIfMissing(db, 1, 2) }, "Failed to create incident_tag"},
		{"createUserTagIfMissing", "INSERT INTO `user_tags`", nil, func(db *gorm.DB) { createUserTagIfMissing(db, 1, 2) }, "Failed to create user_tag"},
		{"createLanguageUniqueIndexes", "idx_statuspage_language_default", nil, createLanguageUniqueIndexes, "Failed to create idx_statuspage_language_default"},
		{"seedLanguageCatalog: Count", "FROM `languages`", nil, seedLanguageCatalog, "Failed to check for existing language"},
		{"seedLanguageCatalog: Create", "INSERT INTO `languages`", nil, seedLanguageCatalog, "Failed to seed language"},
		{"backfillStatusPageLanguages: Pages", "FROM `status_pages`", nil, backfillStatusPageLanguages, "Failed to load status pages for language backfill"},
		{"backfillStatusPageLanguages: Count", "FROM `status_page_languages`", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Create(&models.StatusPage{Name: "P", Slug: "p"}).Error)
		}, backfillStatusPageLanguages, "Failed to check existing StatusPageLanguage"},
		{"backfillStatusPageLanguages: Create", "INSERT INTO `status_page_languages`", func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Create(&models.StatusPage{Name: "P", Slug: "p"}).Error)
		}, backfillStatusPageLanguages, "Failed to backfill StatusPageLanguage"},
		{"CreateUserStatusPageRoleUniqueIndex: Dedupe", "UPDATE user_status_page_roles", nil, CreateUserStatusPageRoleUniqueIndex, "Failed to dedupe user_status_page_roles"},
		{"CreateUserStatusPageRoleUniqueIndex: Create", "idx_user_status_page_role_active", nil, CreateUserStatusPageRoleUniqueIndex, "Failed to create idx_user_status_page_role_active"},
		{"CreateAdminNotificationIndexes", "idx_admin_notification_dedup", nil, CreateAdminNotificationIndexes, "Failed to create admin notification index"},
		{"CreatePageInvitationUniqueIndex: Dedupe", "DELETE FROM page_invitations", nil, CreatePageInvitationUniqueIndex, "Failed to dedupe page_invitations"},
		{"CreatePageInvitationUniqueIndex: Create", "idx_page_invitation_page_email", nil, CreatePageInvitationUniqueIndex, "Failed to create idx_page_invitation_page_email"},
		{"createUiOverrideUniqueIndexes", "idx_ui_override_page", nil, createUiOverrideUniqueIndexes, "Failed to create ui_translation_overrides unique index"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newLoggingTestDB(t)
			if tc.prepare != nil {
				tc.prepare(t, db)
			}
			if tc.failSQL != "" {
				failOnSQL(t, db, tc.failSQL)
			}
			got := loggingtest.ExpectFatal(t, func() { tc.run(db) })
			assert.Equal(t, 1, got.Code)
			assert.Contains(t, got.Message, tc.wantMsg)
			assert.NotNil(t, got.Attrs["error"], "Fehler muss als Attribut geloggt werden")
		})
	}
}

// Der Erfolgsfall von migrateIncidentComponents (Postgres-SQL per Rewrite
// neutralisiert) muss ohne Fatal durchlaufen und den Abschluss loggen.
func TestMigrateIncidentComponents_SuccessLogs(t *testing.T) {
	db := newLoggingTestDB(t)
	rewriteSQL(t, db, "information_schema", "SELECT 1")
	rewriteSQL(t, db, "INSERT INTO incident_components", "SELECT 1")
	rewriteSQL(t, db, "ALTER TABLE incidents", "SELECT 1")
	out := captureLog(t)
	migrateIncidentComponents(db)
	assert.Contains(t, out.String(), "incident_components migration completed")
}

// Fehlen die Legacy-Spalten nur bei incidents bzw. users, bricht die
// Tag-Migration weich ab und loggt den übersprungenen Schritt.
func TestMigrateTagsFromCountryStrings_SkipLogs(t *testing.T) {
	t.Run("incidents", func(t *testing.T) {
		db := newLoggingTestDB(t)
		rewriteSQL(t, db, "FROM components WHERE country", "SELECT 1 AS id, 'x' AS country, 1 AS status_page_id WHERE 1=0")
		out := captureLog(t)
		migrateTagsFromCountryStrings(db)
		assert.Contains(t, out.String(), "Skipping incident tag migration")
	})
	t.Run("users", func(t *testing.T) {
		db := newLoggingTestDB(t)
		rewriteSQL(t, db, "FROM components WHERE country", "SELECT 1 AS id, 'x' AS country, 1 AS status_page_id WHERE 1=0")
		rewriteSQL(t, db, "FROM incidents WHERE country", "SELECT 1 AS id, 'x' AS country, 1 AS status_page_id WHERE 1=0")
		out := captureLog(t)
		migrateTagsFromCountryStrings(db)
		assert.Contains(t, out.String(), "Skipping user tag migration")
	})
}

func TestHashPassword_ErrorIsLogged(t *testing.T) {
	out := captureLog(t)
	// bcrypt lehnt Passwörter über 72 Byte ab.
	hash, err := HashPassword(strings.Repeat("x", 73))
	assert.Empty(t, hash)
	assert.ErrorIs(t, err, ErrPasswordTooLong)
	assert.Contains(t, out.String(), "Error hashing password")
	assert.NotContains(t, out.String(), strings.Repeat("x", 73))
}

// faultDialector erlaubt InitDB-Tests, Callbacks auf der intern geöffneten
// DB zu registrieren bzw. das Öffnen scheitern zu lassen.
type faultDialector struct {
	gorm.Dialector
	setup func(db *gorm.DB) error
}

func (d faultDialector) Initialize(db *gorm.DB) error {
	if err := d.Dialector.Initialize(db); err != nil {
		return err
	}
	return d.setup(db)
}

func useFaultDialector(t *testing.T, seed string, setup func(db *gorm.DB) error) {
	t.Helper()
	prevDialector, prevDB := openDialector, DB
	t.Cleanup(func() { openDialector, DB = prevDialector, prevDB })
	openDialector = func(dsn string) gorm.Dialector {
		return faultDialector{Dialector: sqlite.Open(dsn), setup: setup}
	}
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "initdb.db"))
	t.Setenv("SEED_DB", seed)
	t.Setenv("APP_ENV", "test")
}

func TestInitDB_FatalPaths(t *testing.T) {
	t.Run("Verbindung", func(t *testing.T) {
		useFaultDialector(t, "", func(*gorm.DB) error { return errInjected })
		got := loggingtest.ExpectFatal(t, InitDB)
		assert.Equal(t, "Failed to connect to database", got.Message)
	})
	t.Run("Migration", func(t *testing.T) {
		useFaultDialector(t, "", func(db *gorm.DB) error {
			failOnSQL(t, db, "CREATE TABLE `status_pages`")
			return nil
		})
		got := loggingtest.ExpectFatal(t, InitDB)
		assert.Equal(t, "Migration failed", got.Message)
	})
}

// SEED_DB=true auf SQLite: Drop-Fehler wird nur geloggt, danach laufen
// Migration und SeedData komplett durch.
func TestInitDB_SeedLogs(t *testing.T) {
	useFaultDialector(t, "true", func(db *gorm.DB) error {
		failOnSQL(t, db, "DROP TABLE IF EXISTS `user_tags`")
		return nil
	})
	out := captureLog(t)
	InitDB()
	s := out.String()
	assert.Contains(t, s, "SEED_DB is true")
	assert.Contains(t, s, "Failed to drop tables for seed")
	assert.Contains(t, s, "Seeding initial data")
	assert.Contains(t, s, "Database seeding completed successfully")
	var users int64
	require.NoError(t, DB.Model(&models.User{}).Count(&users).Error)
	assert.Positive(t, users)
}
