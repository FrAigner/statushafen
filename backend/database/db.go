package database

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"
	"github.com/fraigner/statushafen/statushistory"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

// openDialector builds the database driver for InitDB. A variable so that a
// test can run InitDB entirely against SQLite (new
// tables in the AutoMigrate list should be checked by the coverage gate,
// without Postgres in the test environment).
var openDialector = postgres.Open

// MaxPasswordBytes is the upper limit up to which bcrypt processes a password;
// longer input is rejected. The handlers reject it beforehand with
// password_too_long.
const MaxPasswordBytes = 72

// ErrPasswordTooLong: the password is longer than MaxPasswordBytes bytes.
var ErrPasswordTooLong = bcrypt.ErrPasswordTooLong

// HashPassword returns the bcrypt hash of the password. On error the
// hash is empty and the error is set: callers must abort instead of storing
// an empty hash (an account without a password would otherwise count as set).
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		slog.Error("Error hashing password", "error", err)
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(bytes), nil
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// dummyPasswordHash is a fixed bcrypt hash (same cost as HashPassword) of a
// text that is not an account password. It serves only for timing equalization.
const dummyPasswordHash = "$2a$10$miYhj4I6m./0VB7F8cfQw.oIcHxUOsrrHwFuONPi2/YDCsX4T3Iy."

// CheckPasswordConstantTime checks like CheckPasswordHash, but on an empty
// hash (unknown account, account without password) it runs a full
// bcrypt comparison against the dummy hash and returns false. This way
// the login runtime does not depend on whether the address has an account.
func CheckPasswordConstantTime(password, hash string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(password))
		return false
	}
	return CheckPasswordHash(password, hash)
}

// buildDSN returns DB_DSN if set, otherwise a Postgres DSN assembled from individual env vars
// (or defaults for local development).
// Extracted from InitDB() so it is testable without a real DB connection.
func buildDSN() string {
	if dsn := os.Getenv("DB_DSN"); dsn != "" {
		return dsn
	}
	dbHost := envOrDefault("DB_HOST", "localhost")
	dbUser := envOrDefault("DB_USER", "postgres")
	dbPass := envOrDefault("DB_PASSWORD", "localpassword") //nolint:gosec // dev-only fallback, overridden by DB_DSN in production
	dbName := envOrDefault("DB_NAME", "statushafen")
	dbPort := envOrDefault("DB_PORT", "5432")
	dbSSL := envOrDefault("DB_SSL_MODE", "disable") //nolint:gosec // dev default; use require/verify-full in prod via DB_DSN
	return "host=" + dbHost + " user=" + dbUser + " password=" + dbPass + " dbname=" + dbName + " port=" + dbPort + " sslmode=" + dbSSL + " TimeZone=UTC"
}

// developmentAppEnvs are the APP_ENV values in which seed and reset
// are allowed. Deliberately an allowlist: an empty, misspelled or
// unknown value (prod, live, staging) blocks them, instead of dropping the tables on a
// production system with a forgotten variable and
// creating default accounts.
var developmentAppEnvs = []string{"development", "dev", "test", "local"}

// IsDevelopmentAppEnv reports whether APP_ENV explicitly names a development environment.
// All development exceptions (seed, insecure OIDC issuers, internal
// domain check without token) depend on it and are off otherwise: an empty or
// misspelled value counts as production (fail closed). Comparisons with
// "production" are unsuitable for this, they open up for any other value.
func IsDevelopmentAppEnv() bool {
	return slices.Contains(developmentAppEnvs, strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))))
}

// seedAllowed decides on SEED_DB=true: only effective with a development APP_ENV.
// Otherwise SEED_DB is ignored and that is said in the log.
func seedAllowed() bool {
	if os.Getenv("SEED_DB") != "true" {
		return false
	}
	if IsDevelopmentAppEnv() {
		return true
	}
	env := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	slog.Warn("SEED_DB wird ignoriert: Seed und Reset sind nur mit APP_ENV=development, dev, test oder local erlaubt",
		"app_env", env)
	return false
}

func InitDB() {
	dsn := buildDSN()

	var err error
	DB, err = gorm.Open(openDialector(dsn), &gorm.Config{Logger: NewGormLogger(os.Stdout)})
	if err != nil {
		logging.Fatal("Failed to connect to database", "error", err)
	}

	// Connection Pooling
	if sqlDB, err := DB.DB(); err == nil {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)
	}

	slog.Info("Database connection established. Running migrations...")

	seed := seedAllowed()
	if seed {
		slog.Info("SEED_DB is true. Dropping tables for a clean seed...")
		if err := DB.Migrator().DropTable(
			// message center, read status before message (FK).
			&models.NotificationRead{},
			&models.AdminNotification{},
			// external status pages, children before parents (FKs).
			&models.ExternalIncidentComponent{},
			&models.ExternalIncidentUpdate{},
			&models.ExternalIncident{},
			&models.ExternalComponentLink{},
			&models.ExternalComponent{},
			&models.ExternalSource{},
			&models.UserTag{},
			&models.ComponentTag{},
			&models.IncidentTag{},
			&models.Tag{},
			&models.IncidentTranslation{},
			&models.ComponentTranslation{},
			&models.GeneralNoticeTranslation{},
			&models.IncidentTemplate{},
			&models.IncidentTemplateTranslation{},
			&models.NotificationTemplate{},
			&models.UserStatusPageRole{},
			&models.Subscriber{},
			&models.GeneralNotice{},
			&models.ActionItem{},
			&models.PostMortem{},
			&models.IncidentUpdateTranslation{}, // before the update (FK)
			&models.IncidentUpdate{},
			&models.IncidentComponent{},
			&models.Incident{},
			&models.ComponentStatusChange{},
			&models.PageInvitation{},
			&models.Component{},
			&models.User{},
			&models.Role{},
			// StatusPageLanguage references StatusPage via FK (OnDelete:CASCADE) -
			// must be dropped before StatusPage, otherwise orphaned rows remain on reset
			// and the subsequent AutoMigrate FK
			// migration fails with "violates foreign key constraint"
			// (observed as a crash loop on staging since 2026-07-24, RestartCount
			// > 4600). Language (global catalog) is deliberately NOT included here - it
			// is meant to survive seeds, see seedLanguageCatalog.
			&models.StatusPageLanguage{},
			// UiTranslationOverride also references StatusPage via FK
			// (OnDelete:CASCADE) - drop before StatusPage for the same reason.
			&models.UiTranslationOverride{},
			&models.StatusPage{},
			&models.AuditLog{},
			&models.PageOrder{},
			// global system settings (key/value).
			&models.SystemSetting{},
		); err != nil {
			slog.Error("Failed to drop tables for seed", "error", err)
		}
	}

	err = DB.AutoMigrate(
		&models.StatusPage{},
		&models.Role{},
		&models.User{},
		&models.UserStatusPageRole{},
		&models.StatusPageSSOConfig{},
		&models.Component{},
		&models.ComponentTranslation{},
		&models.Incident{},
		&models.IncidentComponent{},
		&models.IncidentTranslation{},
		&models.GeneralNotice{},
		&models.GeneralNoticeTranslation{},
		&models.Subscriber{},
		&models.AuditLog{},
		&models.IncidentUpdate{},
		&models.IncidentUpdateTranslation{},
		&models.PostMortem{},
		&models.ActionItem{},
		&models.Tag{},
		&models.ComponentTag{},
		&models.IncidentTag{},
		&models.UserTag{},
		&models.GeneralNotice{},
		&models.IncidentTemplate{},
		&models.IncidentTemplateTranslation{},
		&models.NotificationTemplate{},
		&models.Language{},
		&models.StatusPageLanguage{},
		&models.PageOrder{},
		&models.UiTranslationOverride{},
		// OIDC login per status page.
		&models.OIDCLoginState{},
		&models.UserSSOIdentity{},
		// global system settings (key/value).
		&models.SystemSetting{},
		// external status pages (source, components, coupling).
		&models.ExternalSource{},
		&models.ExternalComponent{},
		&models.ExternalComponentLink{},
		// mirrored incidents and maintenances.
		&models.ExternalIncident{},
		&models.ExternalIncidentUpdate{},
		&models.ExternalIncidentComponent{},
		// admin message center (bell).
		&models.AdminNotification{},
		&models.NotificationRead{},
		// status history of the components.
		&models.ComponentStatusChange{},
		// invitations to status pages.
		&models.PageInvitation{},
	)
	if err != nil {
		logging.Fatal("Migration failed", "error", err)
	}

	migrateIncidentComponents(DB)
	dropLegacySSOIdentityIndex(DB)
	// Existing subscriptions from before double opt-in count as confirmed.
	backfillSubscriberStatus(DB)
	// Memberships from before joined_via count as legacy (custom domain login).
	backfillMembershipOrigin(DB)
	// Legacy value "completed" on incidents/maintenances becomes "resolved".
	migrateCompletedIncidentStatus(DB)
	migrateExternalSourceProvider(DB)
	// Webhook and order tokens are now stored only as hashes.
	migrateLegacyTokens(DB)

	createJoinTableUniqueIndexes(DB)
	// first identifiers for existing components, then the unique index.
	BackfillComponentSlugs(DB)
	CreateSoftDeleteUniqueIndexes(DB)
	removeOrphanedUserStatusPageRoles(DB)
	removeOrphanedRoleAndUserReferences(DB)
	// before the seed, so the freshly created role stays untouched.
	migrateRolePermissionsToCRUD(DB)
	seedPageOwnerRole(DB)
	migrateTagsFromCountryStrings(DB)
	createLanguageUniqueIndexes(DB)
	// one role per (account, page).
	CreateUserStatusPageRoleUniqueIndex(DB)
	// after the dedupe, so only active memberships count.
	removeOrphanedPageSSOIdentities(DB)
	CreateAdminNotificationIndexes(DB)
	// Review F3: one invitation row per (page, address).
	CreatePageInvitationUniqueIndex(DB)
	seedLanguageCatalog(DB)
	// Content languages are only de and en: switch off the rest of the catalog.
	deactivateUnsupportedLanguages(DB)
	// The system status page must be created before the language backfill,
	// otherwise on a fresh database it only gets its languages on the second start
	// (found by the Postgres migration test).
	SeedSystemStatusPage(DB)
	backfillStatusPageLanguages(DB)
	// After the backfill so every page has rows; pages with another default fall back to de.
	fallbackUnsupportedDefaultLanguages(DB)
	createUiOverrideUniqueIndexes(DB)
	// empty overrides hide texts, remove legacy cases.
	removeEmptyUiOverrides(DB)

	if seed {
		SeedData()
	}
	// Existing components without history (including freshly seeded ones and
	// ones created by SyncSystemCapabilities) get their current state;
	// last, so all seeds before it are captured. Idempotent.
	backfillStatusHistory(DB)
}

// backfillStatusHistory backfills the status history; an error
// is logged and does not prevent startup (availability then falls back
// to incidents, the next start repeats the backfill).
func backfillStatusHistory(db *gorm.DB) {
	if err := statushistory.Backfill(db, time.Now()); err != nil {
		slog.Error("Statushistorie: Nachtrag fehlgeschlagen", "error", err)
	}
}

// SystemStatusPageSlug is the reserved slug of the status page that
// represents the state of the SaaS system itself ("dogfooding").
const SystemStatusPageSlug = "system"

// SeedSystemStatusPage creates the status page of the system
// itself once, exactly like any other customer tenant: a completely normal
// StatusPage with completely normal Components, without any special logic in the
// public handler path. Normal tenant isolation (filtering by
// status_page_id) therefore applies automatically here as well.
//
// Runs on every InitDB() call, independent of SEED_DB/APP_ENV, because it is
// not demo test data but a real, permanently needed
// status page. Idempotent: if a StatusPage with the
// reserved slug already exists, the function is a no-op - neither the page nor its
// Components are created again or duplicated.
//
// The check runs Unscoped, so it also counts a soft-deleted
// system page. Previously the count did not see it, creating failed on the
// slug index and logging.Fatal aborted the backend start. A deleted
// system page (from the time before the deletion lock in DeleteStatusPageHandler)
// stays deleted instead of silently reappearing on every start: the seed
// does not override an admin decision.
//
// The page is identified via StatusPage.IsSystem, not via the
// slug. If an admin changed the slug, the seed used to find no page with slug
// "system" and created a second, empty one on the next start. Existing data
// without the marker: the page with the reserved slug receives it once
// (backfill, afterwards it is unambiguous even with a changed slug). A backfill
// is only possible while the slug is unchanged; for already renamed
// pages the seed cannot guess the system page (see report).
func SeedSystemStatusPage(db *gorm.DB) {
	var count int64
	if err := db.Unscoped().Model(&models.StatusPage{}).Where("is_system = ?", true).Count(&count).Error; err != nil {
		logging.Fatal("Failed to check for existing system status page", "error", err)
	}
	if count > 0 {
		var page models.StatusPage
		if err := db.Where("is_system = ?", true).First(&page).Error; err == nil {
			SyncSystemCapabilities(db, page.ID) // add new capabilities
		}
		return
	}

	var legacy []models.StatusPage
	if err := db.Unscoped().Where("slug = ?", SystemStatusPageSlug).Find(&legacy).Error; err != nil {
		logging.Fatal("Failed to check for legacy system status page", "error", err)
	}
	if len(legacy) > 0 {
		// Oldest entry first (Find without Order is undefined), preferring an active page.
		chosen := legacy[0]
		for _, p := range legacy {
			if (!p.DeletedAt.Valid && chosen.DeletedAt.Valid) || (p.DeletedAt.Valid == chosen.DeletedAt.Valid && p.ID < chosen.ID) {
				chosen = p
			}
		}
		if err := db.Unscoped().Model(&models.StatusPage{}).Where("id = ?", chosen.ID).UpdateColumn("is_system", true).Error; err != nil {
			logging.Fatal("Failed to mark legacy system status page", "error", err)
		}
		return
	}

	slog.Info("Seeding system status page...")

	page := models.StatusPage{
		Name:            "System Status",
		Slug:            SystemStatusPageSlug,
		IsSystem:        true,
		Description:     "Öffentliche Statusseite des Statushafen-Systems selbst",
		IsPublicIndexed: true,
		IsPrivate:       false,
	}
	if err := db.Create(&page).Error; err != nil {
		logging.Fatal("Failed to create system status page", "error", err)
	}

	componentNames := []string{"API", "Datenbank", "Frontend", "E-Mail-Versand"}
	for i, name := range componentNames {
		component := models.Component{
			StatusPageID: page.ID,
			Name:         name,
			Status:       "operational",
			SortOrder:    i,
		}
		if err := db.Create(&component).Error; err != nil {
			logging.Fatal("Failed to create system status page component", "component", name, "error", err)
		}
	}

	SyncSystemCapabilities(db, page.ID)
	slog.Info("System status page seeding completed.")
}

// migrateIncidentComponents migrates the former 1:1 FK
// incidents.component_id to the new join table incident_components and
// then removes the old column. Idempotent: checks via raw SQL against
// information_schema whether the column still exists - after the first
// successful run the function is a no-op.
func migrateIncidentComponents(db *gorm.DB) {
	var columnExists int
	db.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_name = 'incidents' AND column_name = 'component_id'`).Scan(&columnExists)
	if columnExists == 0 {
		return
	}

	slog.Info("Migrating incidents.component_id into incident_components...")
	if err := db.Exec(`
		INSERT INTO incident_components (incident_id, component_id, sort_order, created_at, updated_at)
		SELECT id, component_id, 0, now(), now() FROM incidents
		WHERE component_id IS NOT NULL
		  AND id NOT IN (SELECT incident_id FROM incident_components)
	`).Error; err != nil {
		logging.Fatal("Failed to migrate incident components", "error", err)
	}

	if err := db.Exec(`ALTER TABLE incidents DROP COLUMN IF EXISTS component_id`).Error; err != nil {
		logging.Fatal("Failed to drop legacy component_id column", "error", err)
	}
	slog.Info("incident_components migration completed.")
}

// createJoinTableUniqueIndexes creates partial unique indexes for the four soft-delete-
// capable many-to-many join tables (IncidentComponent, ComponentTag,
// IncidentTag, UserTag) that cover only non-deleted
// rows (deleted_at IS NULL). Without this restriction a "replace" operation
// (delete + create of the same assignment, e.g. saving a form again
// without content changes) collides with the soft-deleted
// legacy record on a regular unique index and fails with 500,
// although it should be an idempotent no-op. GORM's AutoMigrate cannot create
// partial indexes (see CreateSoftDeleteUniqueIndexes), hence raw SQL.
// A regular unique index of the same name possibly originating from an older AutoMigrate version
// is removed beforehand. Idempotent via IF NOT
// EXISTS, hence safe to run again on every InitDB run.
func createJoinTableUniqueIndexes(db *gorm.DB) {
	joinTables := []struct {
		table       string
		legacyIndex string
		newIndex    string
		columns     string
	}{
		{"incident_components", "idx_incident_component", "idx_incident_component_active", "incident_id, component_id"},
		{"component_tags", "idx_component_tag", "idx_component_tag_active", "component_id, tag_id"},
		{"incident_tags", "idx_incident_tag", "idx_incident_tag_active", "incident_id, tag_id"},
		{"user_tags", "idx_user_tag", "idx_user_tag_active", "user_id, tag_id"},
		// one translation per (update, language).
		{"incident_update_translations", "idx_incident_update_translation", "idx_incident_update_translation_active", "update_id, language_code"},
		{"status_page_languages", "idx_statuspage_language", "idx_statuspage_language_active", "status_page_id, language_code"},
		// Coupling of own and external component and the external
		// component itself (one row per external ID and source). No
		// legacy indexes exist, DROP IF EXISTS is then a no-op.
		{"external_component_links", "idx_external_component_link", "idx_external_component_link_active", "component_id, external_component_id"},
		{"external_components", "idx_external_component", "idx_external_component_active", "external_source_id, external_id"},
		// mirrored incidents/maintenances, their updates and affected components.
		{"external_incidents", "idx_external_incident", "idx_external_incident_active", "external_source_id, external_id"},
		{"external_incident_updates", "idx_external_incident_update", "idx_external_incident_update_active", "external_incident_id, external_id"},
		{"external_incident_components", "idx_external_incident_component", "idx_external_incident_component_active", "external_incident_id, component_external_id"},
	}
	for _, jt := range joinTables {
		if err := db.Exec(`DROP INDEX IF EXISTS ` + jt.legacyIndex).Error; err != nil {
			logging.Fatal("Failed to drop legacy index", "index", jt.legacyIndex, "error", err)
		}
		sql := `CREATE UNIQUE INDEX IF NOT EXISTS ` + jt.newIndex + ` ON ` + jt.table + ` (` + jt.columns + `) WHERE deleted_at IS NULL`
		if err := db.Exec(sql).Error; err != nil {
			logging.Fatal("Failed to create join table index", "index", jt.newIndex, "error", err)
		}
	}
}

// dropLegacySSOIdentityIndex removes the unique index
// idx_user_sso_identity on (status_page_id, issuer, subject). At this point AutoMigrate has
// already created the column protocol (default "oidc", so all
// existing links count as OIDC) and the new index
// idx_user_sso_identity_protocol, but never deletes old indexes itself.
// Idempotent via IF EXISTS. An error is not fatal: if the old index
// remains, only the rare linking fails in which the OIDC issuer and
// SAML entity ID including subject match.
//
// Likewise the intermediate state idx_user_sso_identity_protocol
// (without NameID format); replaced by idx_user_sso_identity_key. Existing rows
// keep subject_format = ” (OIDC). A backfill for SAML rows is omitted:
// their format was not stored so far, an assumption would be a guess; the
// few accounts link anew (SAML was not yet in operation).
func dropLegacySSOIdentityIndex(db *gorm.DB) {
	_ = db.Exec(`DROP INDEX IF EXISTS idx_user_sso_identity`).Error
	_ = db.Exec(`DROP INDEX IF EXISTS idx_user_sso_identity_protocol`).Error
}

// softDeleteUniqueIndexes are the partial unique indexes for
// names and slugs of soft-delete-capable models. They cover only active rows
// (deleted_at IS NULL): a deleted tag or a deleted status page
// otherwise blocks its name or slug forever, because gorm.Model only sets
// deleted_at and the row stays in the table. The handlers check
// duplicates via GORM (which hides soft-deleted rows), but the old index
// still saw them: the result was a 500 instead of a successful create.
//
// Tags: a normal unique index on (name, status_page_id) would not
// prevent duplicate global tags (status_page_id IS NULL), since NULL in
// Postgres is pairwise distinct. Hence two indexes, case-insensitive
// via LOWER(name).
//
// Status pages: the slug previously had the gorm tag uniqueIndex (index
// idx_status_pages_slug). AutoMigrate cannot create partial indexes and
// never removes old indexes itself, so the tag is removed and the
// index is replaced here.
//
// legacyIndex is the earlier, non-partial index of the same purpose. It is
// removed via DROP INDEX IF EXISTS, the new one is created under its own name via IF NOT
// EXISTS (pattern: createJoinTableUniqueIndexes). Both are a no-op from the
// second run on. Reusing the old names would not be
// idempotent: IF NOT EXISTS only checks the name, not the definition.
var softDeleteUniqueIndexes = []struct {
	legacyIndex string
	newIndex    string
	definition  string
}{
	{"idx_tags_global_name", "idx_tags_global_name_active", "tags (LOWER(name)) WHERE status_page_id IS NULL AND deleted_at IS NULL"},
	{"idx_tags_scoped_name", "idx_tags_scoped_name_active", "tags (LOWER(name), status_page_id) WHERE status_page_id IS NOT NULL AND deleted_at IS NULL"},
	{"idx_status_pages_slug", "idx_status_pages_slug_active", "status_pages (slug) WHERE deleted_at IS NULL"},
	// at most one active system page. No legacy index.
	{"", "idx_status_pages_system_active", "status_pages (is_system) WHERE is_system = true AND deleted_at IS NULL"},
	// Names, emails and translations of deleted rows become
	// free again. The legacy indexes come from the earlier gorm tags
	// (uniqueIndex or index:...,unique). User.Email: a deleted address may be
	// created anew; the address of the deleted row is kept as an audit trail.
	// Deliberately NOT switched: Language.Code and SystemSetting.Key
	// (no soft delete in operation, languages are deactivated; the upsert of the
	// settings needs a full index via ON CONFLICT (key)).
	{"idx_roles_name", "idx_roles_name_active", "roles (name) WHERE deleted_at IS NULL"},
	{"idx_users_email", "idx_users_email_active", "users (email) WHERE deleted_at IS NULL"},
	{"idx_notification_template", "idx_notification_template_active", "notification_templates (status_page_id, kind, audience, language) WHERE deleted_at IS NULL"},
	{"idx_incident_translation", "idx_incident_translation_active", "incident_translations (incident_id, language_code) WHERE deleted_at IS NULL"},
	{"idx_incident_template_translation", "idx_incident_template_translation_active", "incident_template_translations (incident_template_id, language_code) WHERE deleted_at IS NULL"},
	{"idx_component_translation", "idx_component_translation_active", "component_translations (component_id, language_code) WHERE deleted_at IS NULL"},
	// Identifier of the component, unique per status page among active rows.
	// Empty identifiers (legacy rows before the backfill) are excluded,
	// so the backfill runs BEFORE this index (BackfillComponentSlugs).
	{"", "idx_components_slug_active", "components (status_page_id, slug) WHERE deleted_at IS NULL AND slug <> ''"},
	// A domain belongs to at most one page once it is verified;
	// unproven entries (pending) may overlap. No legacy index.
	{"", "idx_status_pages_custom_domain_live", "status_pages (LOWER(custom_domain)) WHERE custom_domain_status IN ('verified','active') AND deleted_at IS NULL"},
	{"idx_notice_translation", "idx_notice_translation_active", "general_notice_translations (general_notice_id, language_code) WHERE deleted_at IS NULL"},
}

// CreateSoftDeleteUniqueIndexes creates the indexes from softDeleteUniqueIndexes
// and replaces the earlier, non-partial indexes. Exported so
// handler tests on SQLite have the same indexes as production
// (testutils.SetupTestDB only knows AutoMigrate).
//
// Order: new index first, then legacy index. If creating
// fails (e.g. because active duplicates exist in a deviating existing database),
// the legacy index stays and there is only a warning instead of a
// startup abort - the legacy index is stricter and keeps protecting. Nothing
// is ever deleted except the replaced index.
func CreateSoftDeleteUniqueIndexes(db *gorm.DB) {
	for _, idx := range softDeleteUniqueIndexes {
		if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS ` + idx.newIndex + ` ON ` + idx.definition).Error; err != nil {
			slog.Warn("Unique-Index konnte nicht angelegt werden, Altindex bleibt bestehen (vermutlich aktive Duplikate, bitte manuell prüfen)",
				"index", idx.newIndex, "legacy_index", idx.legacyIndex, "error", err)
			continue
		}
		if idx.legacyIndex == "" {
			continue
		}
		if err := db.Exec(`DROP INDEX IF EXISTS ` + idx.legacyIndex).Error; err != nil {
			logging.Fatal("Failed to drop legacy index", "index", idx.legacyIndex, "error", err)
		}
	}
}

// removeOrphanedUserStatusPageRoles cleans up assignments once
// whose user or role was deleted via soft delete. Soft delete
// never triggers the FK OnDelete, the handlers only clean up themselves since the changeover.
// Hard delete of the join rows as in replaceUserPageRoles; the rows were
// ineffective anyway (the role or user is no longer loadable).
// Idempotent: after the first run the statement matches nothing anymore.
func removeOrphanedUserStatusPageRoles(db *gorm.DB) {
	res := db.Exec(`DELETE FROM user_status_page_roles
		WHERE user_id IN (SELECT id FROM users WHERE deleted_at IS NOT NULL)
		   OR role_id IN (SELECT id FROM roles WHERE deleted_at IS NOT NULL)`)
	if res.Error != nil {
		slog.Warn("Verwaiste user_status_page_roles konnten nicht bereinigt werden", "error", res.Error)
		return
	}
	if res.RowsAffected > 0 {
		slog.Info("Verwaiste user_status_page_roles entfernt", "count", res.RowsAffected)
	}
}

// removeOrphanedRoleAndUserReferences cleans up legacy leftovers once:
// StatusPage.DefaultRoleID pointing to a deleted role is set to
// NULL; UserTag and UserSSOIdentity of deleted users are removed
// (hard delete of the join rows as in me_delete). The handlers clean up themselves since the changeover.
// Idempotent: after the first run no statement matches anymore.
func removeOrphanedRoleAndUserReferences(db *gorm.DB) {
	stmts := []struct{ name, sql string }{
		{"status_pages.default_role_id", `UPDATE status_pages SET default_role_id = NULL
			WHERE default_role_id IS NOT NULL
			  AND default_role_id IN (SELECT id FROM roles WHERE deleted_at IS NOT NULL)`},
		{"user_tags", `DELETE FROM user_tags WHERE user_id IN (SELECT id FROM users WHERE deleted_at IS NOT NULL)`},
		{"user_sso_identities", `DELETE FROM user_sso_identities WHERE user_id IN (SELECT id FROM users WHERE deleted_at IS NOT NULL)`},
	}
	for _, st := range stmts {
		res := db.Exec(st.sql)
		if res.Error != nil {
			slog.Warn("Verwaiste Verweise konnten nicht bereinigt werden", "target", st.name, "error", res.Error)
			continue
		}
		if res.RowsAffected > 0 {
			slog.Info("Verwaiste Verweise bereinigt", "target", st.name, "count", res.RowsAffected)
		}
	}
}

// PageOwnerRoleName is the name of the default page role with pages:write
// .
const PageOwnerRoleName = "Page Owner"

// pageOwnerSeededKey marks (SystemSetting) that the role was created once.
const pageOwnerSeededKey = "seed.page_owner_role"

// PageOwnerPermissions are the permissions of the role "Page Owner": all permissions of
// "Page Admin" plus pages:write (users of the page, SSO, domain, mail templates,
// languages). "Page Admin" deliberately stays unchanged.
//
// derived from the old permission list (permissions.MigrateLegacy),
// so the role keeps exactly its former effect - i.e. create/read/update/
// delete on pages, components, external sources, incidents, maintenance,
// incident templates, notices, subscribers, mail templates, languages, UI texts and SSO.
var PageOwnerPermissions = permissions.MigrateLegacy(pageAdminLegacyPermissions + ",pages:write")

// pageAdminLegacyPermissions is the permission list of the role "Page Admin" before the changeover.
const pageAdminLegacyPermissions = "components:write,incidents:write,notices:write,subscribers:write," + permissions.AdminDashboard

// seedPageOwnerRole creates the default role "Page Owner". Since the
// permissions reform "Global Admin" can no longer be assigned as a page role and was the
// only role in the seed with pages:write; anyone who wanted a page admin with
// user management had to build their own role. Runs on every
// start, independent of SEED_DB (the role is also needed in production),
// and is idempotent and admin-safe: the check runs Unscoped, so if a role of the same name
// already exists (even one adapted by the operator
// or deliberately deleted), nothing is created or overwritten.
func seedPageOwnerRole(db *gorm.DB) {
	// Stable marker instead of name: once the seed has run,
	// it counts as done - even if the operator renames or
	// deletes the role. Otherwise a new role would be created on start after every rename.
	var done int64
	if err := db.Unscoped().Model(&models.SystemSetting{}).Where("key = ?", pageOwnerSeededKey).Count(&done).Error; err != nil {
		slog.Warn("Seed-Merker der Rolle Page Owner konnte nicht gelesen werden", "error", err)
		return
	}
	if done > 0 {
		return
	}
	var count int64
	if err := db.Unscoped().Model(&models.Role{}).Where(whereNameEquals, PageOwnerRoleName).Count(&count).Error; err != nil {
		slog.Warn("Rolle Page Owner konnte nicht geprüft werden", "error", err)
		return
	}
	if count == 0 {
		if err := db.Create(&models.Role{Name: PageOwnerRoleName, Permissions: PageOwnerPermissions}).Error; err != nil {
			slog.Warn("Rolle Page Owner konnte nicht angelegt werden", "error", err)
			return
		}
	}
	if err := createSettingIfMissing(db, pageOwnerSeededKey, "1"); err != nil {
		slog.Warn("Seed-Merker der Rolle Page Owner konnte nicht gesetzt werden", "error", err)
	}
}

// migrateTagsFromCountryStrings migrates (step 1/4 -
// Country → Tags) the former Country strings additively into the tag data model,
// analogous to migrateIncidentComponents(): raw SQL check of the existing data +
// insert, provided the assignment does not exist yet.
//
// Since step 4/4 the Go fields Component.Country,
// Incident.Country and User.AllowedCountries no longer exist - AutoMigrate therefore does
// not create their DB columns (country/allowed_countries) on any new
// database anymore (tests, fresh deployments). On already running
// installations (where the columns from an earlier
// model version still physically exist, two-phase cutover) this function stays effective as before. Instead of a
// hard log.Fatalf on a missing column (which would crash the entire server start on
// every new DB), a query error here is treated as "nothing
// to migrate" and the function softly aborts the affected step
// - the migration is in any case already done for every existing installation
// once it was deployed.
func migrateTagsFromCountryStrings(db *gorm.DB) {
	slog.Info("Migrating country strings into tags (no-op if legacy columns are already gone)...")

	type entityCountry struct {
		ID           uint
		Country      string
		StatusPageID uint
	}

	// 1. Components: distinct, non-"global" country values per status page
	// become one status-page-bound tag each.
	var components []entityCountry
	if err := db.Raw(`SELECT id, country, status_page_id FROM components WHERE country IS NOT NULL AND country <> '' AND country <> 'global'`).Scan(&components).Error; err != nil {
		slog.Info("Skipping component tag migration (legacy 'country' column likely absent)", "error", err)
		return
	}
	for _, comp := range components {
		statusPageID := comp.StatusPageID
		tag := findOrCreateTag(db, comp.Country, &statusPageID)
		createComponentTagIfMissing(db, comp.ID, tag.ID)
	}

	// 2. Incidents: same procedure as for components.
	var incidents []entityCountry
	if err := db.Raw(`SELECT id, country, status_page_id FROM incidents WHERE country IS NOT NULL AND country <> '' AND country <> 'global'`).Scan(&incidents).Error; err != nil {
		slog.Info("Skipping incident tag migration (legacy 'country' column likely absent)", "error", err)
		return
	}
	for _, inc := range incidents {
		statusPageID := inc.StatusPageID
		tag := findOrCreateTag(db, inc.Country, &statusPageID)
		createIncidentTagIfMissing(db, inc.ID, tag.ID)
	}

	// 3. Users: comma-separated allowed_countries (incl. "global") become
	// global tags (StatusPageID = nil), since AllowedCountries applies
	// across pages.
	type userRow struct {
		ID               uint
		AllowedCountries string
	}
	var users []userRow
	if err := db.Raw(`SELECT id, allowed_countries FROM users`).Scan(&users).Error; err != nil {
		slog.Info("Skipping user tag migration (legacy 'allowed_countries' column likely absent)", "error", err)
		return
	}
	for _, u := range users {
		for _, raw := range strings.Split(u.AllowedCountries, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			tag := findOrCreateTag(db, name, nil)
			createUserTagIfMissing(db, u.ID, tag.ID)
		}
	}

	slog.Info("Tag migration from country strings completed.")
}

// findOrCreateTag finds an existing tag via case-insensitive
// name comparison in the given scope (nil = global) or creates it -
// consistent with the partial unique indexes from CreateSoftDeleteUniqueIndexes.
func findOrCreateTag(db *gorm.DB, name string, statusPageID *uint) models.Tag {
	var tag models.Tag
	query := db.Where("LOWER(name) = LOWER(?)", name)
	if statusPageID == nil {
		query = query.Where("status_page_id IS NULL")
	} else {
		query = query.Where("status_page_id = ?", *statusPageID)
	}
	err := query.First(&tag).Error
	if err == nil {
		return tag
	}
	if err != gorm.ErrRecordNotFound {
		logging.Fatal("Failed to look up tag", "tag", name, "error", err)
	}

	tag = models.Tag{Name: name, StatusPageID: statusPageID}
	if err := db.Create(&tag).Error; err != nil {
		logging.Fatal("Failed to create tag", "tag", name, "error", err)
	}
	return tag
}

func createComponentTagIfMissing(db *gorm.DB, componentID, tagID uint) {
	var count int64
	db.Model(&models.ComponentTag{}).Where("component_id = ? AND tag_id = ?", componentID, tagID).Count(&count)
	if count > 0 {
		return
	}
	if err := db.Create(&models.ComponentTag{ComponentID: componentID, TagID: tagID}).Error; err != nil {
		logging.Fatal("Failed to create component_tag", "component_id", componentID, "tag_id", tagID, "error", err)
	}
}

func createIncidentTagIfMissing(db *gorm.DB, incidentID, tagID uint) {
	var count int64
	db.Model(&models.IncidentTag{}).Where("incident_id = ? AND tag_id = ?", incidentID, tagID).Count(&count)
	if count > 0 {
		return
	}
	if err := db.Create(&models.IncidentTag{IncidentID: incidentID, TagID: tagID}).Error; err != nil {
		logging.Fatal("Failed to create incident_tag", "incident_id", incidentID, "tag_id", tagID, "error", err)
	}
}

func createUserTagIfMissing(db *gorm.DB, userID, tagID uint) {
	var count int64
	db.Model(&models.UserTag{}).Where("user_id = ? AND tag_id = ?", userID, tagID).Count(&count)
	if count > 0 {
		return
	}
	if err := db.Create(&models.UserTag{UserID: userID, TagID: tagID}).Error; err != nil {
		logging.Fatal("Failed to create user_tag", "user_id", userID, "tag_id", tagID, "error", err)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func ptr(id uint) *uint { return &id }

// Wiederholte Query-Literale (go:S1192).
const (
	whereNameEquals  = "name = ?"
	whereEmailEquals = "email = ?"
)

// seedHashPassword is HashPassword; a variable so a test can trigger
// the abort of the seed on a hash error.
var seedHashPassword = HashPassword

func SeedData() {
	slog.Info("Seeding initial data...")

	// --- Roles ---
	roles := []models.Role{
		// derived from the earlier lists (same effect as before).
		{Name: "Global Admin", Permissions: permissions.FullAccess()},
		{Name: "Page Admin", Permissions: permissions.MigrateLegacy(pageAdminLegacyPermissions)},
		{Name: "Editor", Permissions: permissions.MigrateLegacy("components:read,incidents:write,notices:write")},
		{Name: "Viewer", Permissions: permissions.MigrateLegacy("components:read,incidents:read,notices:read")},
		{Name: "Subscriber Manager", Permissions: permissions.MigrateLegacy("subscribers:write")},
	}
	for i := range roles {
		DB.Create(&roles[i])
	}

	var globalAdminRole, pageAdminRole, editorRole, viewerRole models.Role
	DB.Where(whereNameEquals, "Global Admin").First(&globalAdminRole)
	DB.Where(whereNameEquals, "Page Admin").First(&pageAdminRole)
	DB.Where(whereNameEquals, "Editor").First(&editorRole)
	DB.Where(whereNameEquals, "Viewer").First(&viewerRole)

	// --- Users ---
	adminHash, adminErr := seedHashPassword("admin123")
	editorHash, editorErr := seedHashPassword("editor123")
	viewerHash, viewerErr := seedHashPassword("viewer123")
	if err := errors.Join(adminErr, editorErr, viewerErr); err != nil {
		slog.Error("Demo-Benutzer konnten nicht angelegt werden", "error", err)
		return
	}
	users := []models.User{
		{Email: "admin@statushafen.local", PasswordHash: adminHash, RoleID: globalAdminRole.ID, IsGlobalAdmin: true},
		{Email: "editor@statushafen.local", PasswordHash: editorHash, RoleID: editorRole.ID},
		{Email: "viewer@statushafen.local", PasswordHash: viewerHash, RoleID: viewerRole.ID},
	}
	for i := range users {
		DB.Create(&users[i])
	}

	var adminUser, editorUser, viewerUser models.User
	DB.Where(whereEmailEquals, "admin@statushafen.local").First(&adminUser)
	DB.Where(whereEmailEquals, "editor@statushafen.local").First(&editorUser)
	DB.Where(whereEmailEquals, "viewer@statushafen.local").First(&viewerUser)

	// --- StatusPages ---
	// Name deliberately without "Status" suffix: the H1 on the public page appends
	// "Status" itself ({{ pageInfo.Name }} Status) – with "Acme Corp Status" as
	// name it would otherwise be "Acme Corp Status Status".
	page1 := models.StatusPage{Name: "Acme Corp", Slug: "acme", Description: "Offizielle Statusseite der Acme Corporation", IsPublicIndexed: true}
	page2 := models.StatusPage{Name: "TechStart GmbH", Slug: "techstart", Description: "Statusseite für TechStart GmbH Produkte", IsPublicIndexed: true}
	DB.Create(&page1)
	DB.Create(&page2)

	DB.Create(&models.UserStatusPageRole{UserID: adminUser.ID, StatusPageID: page1.ID, RoleID: globalAdminRole.ID})
	DB.Create(&models.UserStatusPageRole{UserID: adminUser.ID, StatusPageID: page2.ID, RoleID: globalAdminRole.ID})
	DB.Create(&models.UserStatusPageRole{UserID: editorUser.ID, StatusPageID: page1.ID, RoleID: editorRole.ID})
	// Deliberately deviating from the global role: "viewer" is only "Viewer" globally,
	// but has "Page Admin" on Acme Corp Status – reproduces the reported bug,
	// where the UI still showed the global (outdated) role instead of the
	// page-related role.
	DB.Create(&models.UserStatusPageRole{UserID: viewerUser.ID, StatusPageID: page1.ID, RoleID: pageAdminRole.ID})

	// Google Chat subscription without email, assigned to admin via CreatedByUserID – fixture
	// for the e2e "Edit my subscription" (covers subscriptions that cannot be found via the
	// email string, only via the account link).
	DB.Create(&models.Subscriber{
		StatusPageID:           page1.ID,
		Channel:                "google_chat",
		WebhookURL:             "https://chat.googleapis.com/v1/spaces/SEED-DEMO/messages",
		SubscribedComponentIDs: "all",
		CreatedByUserID:        &adminUser.ID,
	})

	// ============================================================
	// Acme Corp: nested component structure (folder tree)
	// ============================================================
	//
	// 📁 Core Infrastructure
	//    📁 API Services
	//       🟢 API Gateway (DE)
	//       🟡 API Gateway (US)
	//    📁 Databases
	//       🟢 PostgreSQL Primary
	//       🟢 Redis Cache
	// 📁 External services
	//    🟢 Payment Processor (Stripe)
	//    🟢 E-Mail Service (Mailgun)
	// 📁 Frontend & CDN
	//    🟠 Storage & CDN
	//    🟢 Web Application

	// Level 1: root groups
	grpInfra := models.Component{StatusPageID: page1.ID, Name: "Core Infrastructure", Description: "Kerninfrastruktur und Backend-Dienste", Status: "group", SortOrder: 1}
	grpExternal := models.Component{StatusPageID: page1.ID, Name: "Externe Dienste", Description: "Drittanbieter-Integrationen", Status: "group", SortOrder: 2}
	grpFrontend := models.Component{StatusPageID: page1.ID, Name: "Frontend & Delivery", Description: "Web-Applikation und Content Delivery", Status: "group", SortOrder: 3}
	DB.Create(&grpInfra)
	DB.Create(&grpExternal)
	DB.Create(&grpFrontend)

	// Level 2: subgroups
	grpAPI := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpInfra.ID), Name: "API Services", Description: "REST und GraphQL API Gateways", Status: "group", SortOrder: 1}
	grpDB := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpInfra.ID), Name: "Datenbanken", Description: "Primäre und Cache-Datenbanken", Status: "group", SortOrder: 2}
	DB.Create(&grpAPI)
	DB.Create(&grpDB)

	// Level 3: real components under API Services
	apiDE := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpAPI.ID), Name: "API Gateway (DE)", Description: "Zentraler API-Knotenpunkt Frankfurt (EU-West)", Status: "operational", SortOrder: 1}
	apiUS := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpAPI.ID), Name: "API Gateway (US)", Description: "Zentraler API-Knotenpunkt US East (Virginia)", Status: "degraded_performance", SortOrder: 2}
	DB.Create(&apiDE)
	DB.Create(&apiUS)

	// Level 3: real components under databases
	pgPrimary := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpDB.ID), Name: "PostgreSQL Primary", Description: "Primäre relationale Datenbank", Status: "operational", SortOrder: 1}
	redisCache := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpDB.ID), Name: "Redis Cache", Description: "In-Memory Cache & Session Store", Status: "operational", SortOrder: 2}
	DB.Create(&pgPrimary)
	DB.Create(&redisCache)

	// Level 2: individual external services (no subgroup)
	stripe := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpExternal.ID), Name: "Payment Processor (Stripe)", Description: "Externe Zahlungsabwicklung via Stripe", Status: "operational", SortOrder: 1}
	mailgun := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpExternal.ID), Name: "E-Mail Service (Mailgun)", Description: "Transaktionale E-Mails und Newsletter", Status: "operational", SortOrder: 2}
	DB.Create(&stripe)
	DB.Create(&mailgun)

	// Level 2: Frontend & CDN
	cdn := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpFrontend.ID), Name: "Storage & CDN", Description: "Globales Asset Delivery Network", Status: "partial_outage", SortOrder: 1}
	webapp := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpFrontend.ID), Name: "Web Application", Description: "Haupt-Webanwendung (Vue.js Frontend)", Status: "operational", SortOrder: 2}
	DB.Create(&cdn)
	DB.Create(&webapp)

	// Incidents for Acme (component assignment now via
	// IncidentComponent instead of a direct FK)
	acmeIncidents := []struct {
		Incident     models.Incident
		ComponentIDs []uint
	}{
		{models.Incident{StatusPageID: page1.ID, Title: "Erhöhte Latenz am US Gateway", Message: "Wir verzeichnen aktuell erhöhte Antwortzeiten (bis zu 2000ms) beim US-API-Gateway. Unsere Techniker skalieren die Instanzen hoch.", Status: "investigating"}, []uint{apiUS.ID}},
		{models.Incident{StatusPageID: page1.ID, Title: "Bild-Assets laden teilweise nicht", Message: "Aufgrund eines Routing-Fehlers beim CDN-Provider können Bild-Assets in einigen Regionen nur verzögert ausgeliefert werden.", Status: "identified"}, []uint{cdn.ID}},
	}
	for i := range acmeIncidents {
		DB.Create(&acmeIncidents[i].Incident)
		for sortOrder, compID := range acmeIncidents[i].ComponentIDs {
			DB.Create(&models.IncidentComponent{IncidentID: acmeIncidents[i].Incident.ID, ComponentID: compID, SortOrder: sortOrder})
		}
	}

	// ============================================================
	// TechStart GmbH: simpler, flatter structure
	// ============================================================
	grpProduct := models.Component{StatusPageID: page2.ID, Name: "Produkt", Description: "Kern-Produktkomponenten", Status: "group", SortOrder: 1}
	grpInfra2 := models.Component{StatusPageID: page2.ID, Name: "Infrastruktur", Description: "Server und Datenbanken", Status: "group", SortOrder: 2}
	DB.Create(&grpProduct)
	DB.Create(&grpInfra2)

	techWeb := models.Component{StatusPageID: page2.ID, ParentID: ptr(grpProduct.ID), Name: "Web Application", Description: "Haupt-Webanwendung", Status: "operational", SortOrder: 1}
	techAPI := models.Component{StatusPageID: page2.ID, ParentID: ptr(grpProduct.ID), Name: "Mobile API", Description: "REST API für iOS & Android", Status: "operational", SortOrder: 2}
	techDB := models.Component{StatusPageID: page2.ID, ParentID: ptr(grpInfra2.ID), Name: "Database Cluster", Description: "PostgreSQL Primary + Replicas", Status: "major_outage", SortOrder: 1}
	DB.Create(&techWeb)
	DB.Create(&techAPI)
	DB.Create(&techDB)

	techDBIncident := models.Incident{StatusPageID: page2.ID, Title: "Datenbankausfall in Frankfurt", Message: "Der primäre Datenbankserver ist nicht erreichbar. Failover auf den Replica läuft.", Status: "identified"}
	DB.Create(&techDBIncident)
	DB.Create(&models.IncidentComponent{IncidentID: techDBIncident.ID, ComponentID: techDB.ID})

	// --- Tags ---
	// Replaces the earlier Country free-text values ("global"/"DE"/"US") of the
	// demo data above with real tag assignments - the same distribution as
	// before, only via the new model instead of a string. Deliberately uses
	// the same helpers (findOrCreateTag/create*TagIfMissing) that
	// migrateTagsFromCountryStrings uses.
	globalTag := findOrCreateTag(DB, "global", nil)
	page1DETag := findOrCreateTag(DB, "DE", &page1.ID)
	page1USTag := findOrCreateTag(DB, "US", &page1.ID)
	page2DETag := findOrCreateTag(DB, "DE", &page2.ID)

	// Admin/Editor/Viewer: global (full access, as AllowedCountries="global" before)
	for _, u := range []models.User{adminUser, editorUser, viewerUser} {
		createUserTagIfMissing(DB, u.ID, globalTag.ID)
	}

	// Acme Corp: global components + the two regional API gateways
	for _, compID := range []uint{grpInfra.ID, grpExternal.ID, grpFrontend.ID, grpAPI.ID, grpDB.ID, redisCache.ID, stripe.ID, mailgun.ID, cdn.ID, webapp.ID} {
		createComponentTagIfMissing(DB, compID, globalTag.ID)
	}
	createComponentTagIfMissing(DB, apiDE.ID, page1DETag.ID)
	createComponentTagIfMissing(DB, pgPrimary.ID, page1DETag.ID)
	createComponentTagIfMissing(DB, apiUS.ID, page1USTag.ID)

	createIncidentTagIfMissing(DB, acmeIncidents[0].Incident.ID, page1USTag.ID) // US gateway latency
	createIncidentTagIfMissing(DB, acmeIncidents[1].Incident.ID, globalTag.ID)  // CDN image assets

	// TechStart GmbH: product global, infrastructure DE
	for _, compID := range []uint{grpProduct.ID, techAPI.ID} {
		createComponentTagIfMissing(DB, compID, globalTag.ID)
	}
	for _, compID := range []uint{grpInfra2.ID, techWeb.ID, techDB.ID} {
		createComponentTagIfMissing(DB, compID, page2DETag.ID)
	}
	createIncidentTagIfMissing(DB, techDBIncident.ID, page2DETag.ID)

	slog.Info("Database seeding completed successfully.")
}
