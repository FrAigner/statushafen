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

// openDialector baut den Datenbanktreiber für InitDB. Als Variable, damit ein
// Test InitDB komplett gegen SQLite laufen lassen kann (neue
// Tabellen in der AutoMigrate-Liste sollen vom Coverage-Gate geprüft werden,
// ohne Postgres in der Testumgebung).
var openDialector = postgres.Open

// MaxPasswordBytes ist die Obergrenze, bis zu der bcrypt ein Passwort
// verarbeitet; längere Eingaben lehnt es ab. Die Handler weisen sie vorher mit
// password_too_long ab.
const MaxPasswordBytes = 72

// ErrPasswordTooLong: das Passwort ist länger als MaxPasswordBytes Byte.
var ErrPasswordTooLong = bcrypt.ErrPasswordTooLong

// HashPassword liefert den bcrypt-Hash des Passworts. Bei einem Fehler ist der
// Hash leer und der Fehler gesetzt: Aufrufer müssen abbrechen, statt einen
// leeren Hash zu speichern (ein Konto ohne Passwort gälte sonst als gesetzt).
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

// dummyPasswordHash ist ein fester bcrypt-Hash (Kosten wie HashPassword) eines
// Textes, der kein Konto-Passwort ist. Er dient nur dem Zeitausgleich.
const dummyPasswordHash = "$2a$10$miYhj4I6m./0VB7F8cfQw.oIcHxUOsrrHwFuONPi2/YDCsX4T3Iy."

// CheckPasswordConstantTime prüft wie CheckPasswordHash, rechnet bei leerem
// Hash (unbekanntes Konto, Konto ohne Passwort) aber einen vollen
// bcrypt-Vergleich gegen den Dummy-Hash und liefert false. So unterscheidet
// sich die Laufzeit des Logins nicht danach, ob die Adresse ein Konto hat.
func CheckPasswordConstantTime(password, hash string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(password))
		return false
	}
	return CheckPasswordHash(password, hash)
}

// buildDSN liefert DB_DSN, falls gesetzt, sonst eine aus einzelnen Env-Vars
// (oder Defaults für lokale Entwicklung) zusammengesetzte Postgres-DSN.
// Ausgelagert aus InitDB(), damit sie ohne echte DB-Verbindung testbar ist.
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

// developmentAppEnvs sind die APP_ENV-Werte, in denen Seed und Reset erlaubt
// sind. Bewusst eine Allowlist: Ein leerer, falsch geschriebener oder
// unbekannter Wert (prod, live, staging) sperrt, statt auf einem
// Produktivsystem mit vergessener Variable die Tabellen zu löschen und
// Standardkonten anzulegen.
var developmentAppEnvs = []string{"development", "dev", "test", "local"}

// IsDevelopmentAppEnv meldet, ob APP_ENV ausdrücklich eine Entwicklungsumgebung
// benennt. Alle Entwicklungsausnahmen (Seed, unsichere OIDC-Aussteller, interner
// Domain-Check ohne Token) hängen daran und sind sonst aus: Ein leerer oder
// falsch geschriebener Wert gilt als Produktion (fail closed). Vergleiche mit
// "production" sind dafür ungeeignet, sie öffnen bei jedem anderen Wert.
func IsDevelopmentAppEnv() bool {
	return slices.Contains(developmentAppEnvs, strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))))
}

// seedAllowed entscheidet über SEED_DB=true: nur mit Entwicklungs-APP_ENV
// wirksam. Andernfalls wird SEED_DB ignoriert und das im Log gesagt.
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
			// Nachrichtencenter, Lesestatus vor Nachricht (FK).
			&models.NotificationRead{},
			&models.AdminNotification{},
			// externe Statusseiten, Kinder vor Eltern (FKs).
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
			&models.IncidentUpdateTranslation{}, // vor dem Update (FK)
			&models.IncidentUpdate{},
			&models.IncidentComponent{},
			&models.Incident{},
			&models.ComponentStatusChange{},
			&models.PageInvitation{},
			&models.Component{},
			&models.User{},
			&models.Role{},
			// StatusPageLanguage referenziert StatusPage per FK (OnDelete:CASCADE) -
			// muss vor StatusPage gedroppt werden, sonst bleiben beim Reset
			// verwaiste Zeilen liegen und die anschliessende AutoMigrate-FK-
			// Migration schlaegt mit "violates foreign key constraint" fehl
			// (beobachtet als Crash-Loop auf Staging ab 2026-07-24, RestartCount
			// > 4600). Language (globaler Katalog) bewusst NICHT hier drin - der
			// soll Seeds ueberleben, s. seedLanguageCatalog.
			&models.StatusPageLanguage{},
			// UiTranslationOverride referenziert StatusPage ebenfalls per FK
			// (OnDelete:CASCADE) - aus demselben Grund vor StatusPage droppen.
			&models.UiTranslationOverride{},
			&models.StatusPage{},
			&models.AuditLog{},
			&models.PageOrder{},
			// globale Systemeinstellungen (Key/Value).
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
		// OIDC-Login je Statusseite.
		&models.OIDCLoginState{},
		&models.UserSSOIdentity{},
		// SAML-Login je Statusseite.
		&models.SAMLLoginState{},
		&models.SAMLAssertionUse{},
		// globale Systemeinstellungen (Key/Value).
		&models.SystemSetting{},
		// externe Statusseiten (Quelle, Komponenten, Kopplung).
		&models.ExternalSource{},
		&models.ExternalComponent{},
		&models.ExternalComponentLink{},
		// gespiegelte Vorfälle und Wartungen.
		&models.ExternalIncident{},
		&models.ExternalIncidentUpdate{},
		&models.ExternalIncidentComponent{},
		// Admin-Nachrichtencenter (Glocke).
		&models.AdminNotification{},
		&models.NotificationRead{},
		// Statushistorie der Komponenten.
		&models.ComponentStatusChange{},
		// Einladungen auf Statusseiten.
		&models.PageInvitation{},
	)
	if err != nil {
		logging.Fatal("Migration failed", "error", err)
	}

	migrateIncidentComponents(DB)
	dropLegacySSOIdentityIndex(DB)
	// Bestandsabos vor dem Double-Opt-in gelten als bestätigt.
	backfillSubscriberStatus(DB)
	// Altwert "completed" bei Vorfällen/Wartungen auf "resolved".
	migrateCompletedIncidentStatus(DB)
	migrateExternalSourceProvider(DB)
	// Webhook- und Bestell-Token liegen nur noch als Hash vor.
	migrateLegacyTokens(DB)

	createJoinTableUniqueIndexes(DB)
	// erst Kennungen für Bestandskomponenten, dann der Unique-Index.
	BackfillComponentSlugs(DB)
	CreateSoftDeleteUniqueIndexes(DB)
	removeOrphanedUserStatusPageRoles(DB)
	removeOrphanedRoleAndUserReferences(DB)
	// vor dem Seed, damit die frisch angelegte Rolle unberührt bleibt.
	migrateRolePermissionsToCRUD(DB)
	seedPageOwnerRole(DB)
	migrateTagsFromCountryStrings(DB)
	createLanguageUniqueIndexes(DB)
	// eine Rolle je (Konto, Seite).
	CreateUserStatusPageRoleUniqueIndex(DB)
	// nach dem Dedupe, damit nur aktive Mitgliedschaften zählen.
	removeOrphanedPageSSOIdentities(DB)
	CreateAdminNotificationIndexes(DB)
	// Review F3: eine Einladungszeile je (Seite, Adresse).
	CreatePageInvitationUniqueIndex(DB)
	seedLanguageCatalog(DB)
	// Die System-Statusseite muss vor dem Sprach-Backfill entstehen,
	// sonst bekommt sie auf einer frischen Datenbank erst beim zweiten Start ihre
	// Sprachen (vom Postgres-Migrationstest gefunden).
	SeedSystemStatusPage(DB)
	backfillStatusPageLanguages(DB)
	createUiOverrideUniqueIndexes(DB)
	// leere Overrides blenden Texte aus, Altfälle entfernen.
	removeEmptyUiOverrides(DB)

	if seed {
		SeedData()
	}
	// Bestandskomponenten ohne Historie (auch frisch geseedete und
	// von SyncSystemCapabilities angelegte) bekommen ihren aktuellen Stand;
	// zuletzt, damit alle Seeds davor erfasst sind. Idempotent.
	backfillStatusHistory(DB)
}

// backfillStatusHistory trägt die Statushistorie nach; ein Fehler
// wird geloggt und verhindert den Start nicht (die Verfügbarkeit fällt dann
// auf Vorfälle zurück, der nächste Start wiederholt den Nachtrag).
func backfillStatusHistory(db *gorm.DB) {
	if err := statushistory.Backfill(db, time.Now()); err != nil {
		slog.Error("Statushistorie: Nachtrag fehlgeschlagen", "error", err)
	}
}

// SystemStatusPageSlug ist der reservierte Slug der Statusseite, die
// den Zustand des eigenen SaaS-Systems selbst abbildet ("Dogfooding").
const SystemStatusPageSlug = "system"

// SeedSystemStatusPage legt einmalig die Statusseite des Systems
// selbst an, exakt wie jeden anderen Kunden-Tenant: eine ganz normale
// StatusPage mit ganz normalen Components, ohne jede Sonderlogik im
// öffentlichen Handler-Pfad. Die normale Tenant-Isolation (Filterung nach
// status_page_id) greift dadurch automatisch auch hier.
//
// Läuft bei jedem InitDB()-Aufruf, unabhängig von SEED_DB/APP_ENV, da es sich
// nicht um Demo-Testdaten, sondern um eine reale, dauerhaft benötigte
// Statusseite handelt. Idempotent: Existiert bereits eine StatusPage mit dem
// reservierten Slug, ist die Funktion ein No-Op - weder die Seite noch ihre
// Components werden erneut angelegt oder dupliziert.
//
// Die Prüfung läuft Unscoped, zählt also auch eine soft-gelöschte
// Systemseite. Vorher sah der Count sie nicht, das Anlegen scheiterte am
// Slug-Index und logging.Fatal brach den Backend-Start ab. Eine gelöschte
// Systemseite (aus der Zeit vor der Löschsperre in DeleteStatusPageHandler)
// bleibt gelöscht, statt bei jedem Start still neu zu entstehen: Der Seed
// überschreibt keine Admin-Entscheidung.
//
// Die Seite wird über StatusPage.IsSystem erkannt, nicht über den
// Slug. Änderte ein Admin den Slug, fand der Seed früher keine Seite mit Slug
// "system" und legte beim nächsten Start eine zweite, leere an. Bestandsdaten
// ohne Kennzeichen: die Seite mit dem reservierten Slug erhält es einmalig
// (Backfill, danach ist sie auch bei geändertem Slug eindeutig). Ein Backfill
// ist nur möglich, solange der Slug unverändert ist; bei bereits umbenannten
// Seiten kann der Seed die Systemseite nicht erraten (siehe Bericht).
func SeedSystemStatusPage(db *gorm.DB) {
	var count int64
	if err := db.Unscoped().Model(&models.StatusPage{}).Where("is_system = ?", true).Count(&count).Error; err != nil {
		logging.Fatal("Failed to check for existing system status page", "error", err)
	}
	if count > 0 {
		var page models.StatusPage
		if err := db.Where("is_system = ?", true).First(&page).Error; err == nil {
			SyncSystemCapabilities(db, page.ID) // neue Funktionen nachtragen
		}
		return
	}

	var legacy []models.StatusPage
	if err := db.Unscoped().Where("slug = ?", SystemStatusPageSlug).Find(&legacy).Error; err != nil {
		logging.Fatal("Failed to check for legacy system status page", "error", err)
	}
	if len(legacy) > 0 {
		// Ältester Eintrag zuerst (Find ohne Order ist undefiniert), bevorzugt eine aktive Seite.
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

// migrateIncidentComponents überträgt die frühere 1:1-FK
// incidents.component_id in die neue Join-Tabelle incident_components und
// entfernt die alte Spalte danach. Idempotent: prüft per Raw-SQL gegen
// information_schema, ob die Spalte noch existiert - nach dem ersten
// erfolgreichen Lauf ist die Funktion ein No-Op.
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

// createJoinTableUniqueIndexes legt für die vier soft-delete-
// fähigen many-to-many-Join-Tabellen (IncidentComponent, ComponentTag,
// IncidentTag, UserTag) partielle Unique-Indizes an, die nur nicht-gelöschte
// Zeilen (deleted_at IS NULL) erfassen. Ohne diese Beschränkung kollidiert
// ein "Replace"-Vorgang (Delete + Create derselben Zuordnung, z.B. erneutes
// Speichern eines Formulars ohne inhaltliche Änderung) mit dem soft-gelöschten
// Altdatensatz auf einem regulären Unique-Index und schlägt mit 500 fehl,
// obwohl es ein idempotenter No-Op sein sollte. GORMs AutoMigrate kann keine
// partiellen Indizes erzeugen (siehe CreateSoftDeleteUniqueIndexes), daher Raw-SQL.
// Ein eventuell aus einer älteren AutoMigrate-Version stammender regulärer
// Unique-Index gleichen Namens wird vorher entfernt. Idempotent per IF NOT
// EXISTS, daher gefahrlos bei jedem InitDB-Lauf erneut ausführbar.
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
		// eine Übersetzung je (Update, Sprache).
		{"incident_update_translations", "idx_incident_update_translation", "idx_incident_update_translation_active", "update_id, language_code"},
		{"status_page_languages", "idx_statuspage_language", "idx_statuspage_language_active", "status_page_id, language_code"},
		// Kopplung eigene ↔ externe Komponente und die externe
		// Komponente selbst (eine Zeile je externer ID und Quelle). Keine
		// Altindizes vorhanden, DROP IF EXISTS ist dann ein No-Op.
		{"external_component_links", "idx_external_component_link", "idx_external_component_link_active", "component_id, external_component_id"},
		{"external_components", "idx_external_component", "idx_external_component_active", "external_source_id, external_id"},
		// gespiegelte Vorfälle/Wartungen, ihre Updates und betroffenen Komponenten.
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

// dropLegacySSOIdentityIndex entfernt den Unique-Index
// idx_user_sso_identity auf (status_page_id, issuer, subject). AutoMigrate hat
// zu diesem Zeitpunkt schon die Spalte protocol (Default "oidc", damit gelten
// alle Bestandsverknüpfungen als OIDC) und den neuen Index
// idx_user_sso_identity_protocol angelegt, löscht alte Indizes aber nie selbst.
// Idempotent per IF EXISTS. Ein Fehler ist nicht fatal: Bleibt der alte Index
// stehen, scheitert nur die seltene Verknüpfung, bei der OIDC-Issuer und
// SAML-Entity-ID samt Subject übereinstimmen.
//
// Ebenso der Zwischenstand idx_user_sso_identity_protocol
// (ohne NameID-Format); ersetzt durch idx_user_sso_identity_key. Bestandszeilen
// behalten subject_format = ” (OIDC). Ein Backfill für SAML-Zeilen entfällt:
// Deren Format war bisher nicht gespeichert, eine Annahme wäre geraten; die
// wenigen Konten verknüpfen neu (SAML war noch nicht im Betrieb).
func dropLegacySSOIdentityIndex(db *gorm.DB) {
	_ = db.Exec(`DROP INDEX IF EXISTS idx_user_sso_identity`).Error
	_ = db.Exec(`DROP INDEX IF EXISTS idx_user_sso_identity_protocol`).Error
}

// softDeleteUniqueIndexes sind die partiellen Unique-Indizes für
// Namen und Slugs soft-delete-fähiger Modelle. Sie erfassen nur aktive Zeilen
// (deleted_at IS NULL): Ein gelöschter Tag oder eine gelöschte Statusseite
// blockiert ihren Namen bzw. Slug sonst für immer, weil gorm.Model nur
// deleted_at setzt und die Zeile in der Tabelle bleibt. Die Handler prüfen
// Duplikate per GORM (das soft-gelöschte Zeilen ausblendet), der alte Index
// sah sie aber noch: Ergebnis war ein 500 statt eines erfolgreichen Anlegens.
//
// Tags: Ein normaler Unique-Index auf (name, status_page_id) würde
// doppelte globale Tags (status_page_id IS NULL) nicht verhindern, da NULL in
// Postgres paarweise verschieden ist. Deshalb zwei Indizes, fallunabhängig
// per LOWER(name).
//
// Statusseiten: Der Slug hatte bisher das gorm-Tag uniqueIndex (Index
// idx_status_pages_slug). AutoMigrate kann keine partiellen Indizes und
// entfernt alte Indizes nie selbst, deshalb ist das Tag entfernt und der
// Index wird hier ersetzt.
//
// legacyIndex ist der frühere, nicht-partielle Index gleichen Zwecks. Er wird
// per DROP INDEX IF EXISTS entfernt, der neue unter eigenem Namen per IF NOT
// EXISTS angelegt (Muster: createJoinTableUniqueIndexes). Beides ist ab dem
// zweiten Lauf ein No-Op. Ein Wiederverwenden der alten Namen ginge nicht
// idempotent: IF NOT EXISTS prüft nur den Namen, nicht die Definition.
var softDeleteUniqueIndexes = []struct {
	legacyIndex string
	newIndex    string
	definition  string
}{
	{"idx_tags_global_name", "idx_tags_global_name_active", "tags (LOWER(name)) WHERE status_page_id IS NULL AND deleted_at IS NULL"},
	{"idx_tags_scoped_name", "idx_tags_scoped_name_active", "tags (LOWER(name), status_page_id) WHERE status_page_id IS NOT NULL AND deleted_at IS NULL"},
	{"idx_status_pages_slug", "idx_status_pages_slug_active", "status_pages (slug) WHERE deleted_at IS NULL"},
	// höchstens eine aktive Systemseite. Kein Altindex.
	{"", "idx_status_pages_system_active", "status_pages (is_system) WHERE is_system = true AND deleted_at IS NULL"},
	// Namen, E-Mails und Übersetzungen gelöschter Zeilen werden
	// wieder frei. Die Altindizes stammen aus den früheren gorm-Tags
	// (uniqueIndex bzw. index:...,unique). User.Email: eine gelöschte Adresse darf
	// neu angelegt werden; die Adresse der gelöschten Zeile bleibt als Audit-Spur
	// erhalten. Bewusst NICHT umgestellt: Language.Code und SystemSetting.Key
	// (kein Soft-Delete im Betrieb, Sprachen werden deaktiviert; der Upsert der
	// Einstellungen braucht per ON CONFLICT (key) einen vollständigen Index).
	{"idx_roles_name", "idx_roles_name_active", "roles (name) WHERE deleted_at IS NULL"},
	{"idx_users_email", "idx_users_email_active", "users (email) WHERE deleted_at IS NULL"},
	{"idx_notification_template", "idx_notification_template_active", "notification_templates (status_page_id, kind, audience, language) WHERE deleted_at IS NULL"},
	{"idx_incident_translation", "idx_incident_translation_active", "incident_translations (incident_id, language_code) WHERE deleted_at IS NULL"},
	{"idx_incident_template_translation", "idx_incident_template_translation_active", "incident_template_translations (incident_template_id, language_code) WHERE deleted_at IS NULL"},
	{"idx_component_translation", "idx_component_translation_active", "component_translations (component_id, language_code) WHERE deleted_at IS NULL"},
	// Kennung der Komponente, je Statusseite unter aktiven Zeilen
	// eindeutig. Leere Kennungen (Altzeilen vor dem Backfill) sind ausgenommen,
	// der Backfill läuft deshalb VOR diesem Index (BackfillComponentSlugs).
	{"", "idx_components_slug_active", "components (status_page_id, slug) WHERE deleted_at IS NULL AND slug <> ''"},
	// Eine Domain gehört höchstens einer Seite, sobald sie verifiziert ist;
	// unbewiesene Eintragungen (pending) dürfen sich überschneiden. Kein Altindex.
	{"", "idx_status_pages_custom_domain_live", "status_pages (LOWER(custom_domain)) WHERE custom_domain_status IN ('verified','active') AND deleted_at IS NULL"},
	{"idx_notice_translation", "idx_notice_translation_active", "general_notice_translations (general_notice_id, language_code) WHERE deleted_at IS NULL"},
}

// CreateSoftDeleteUniqueIndexes legt die Indizes aus softDeleteUniqueIndexes an
// und ersetzt dabei die früheren, nicht-partiellen Indizes. Exportiert, damit
// Handler-Tests auf SQLite dieselben Indizes wie die Produktion haben
// (testutils.SetupTestDB kennt nur AutoMigrate).
//
// Reihenfolge erst neuer Index, dann Altindex. Scheitert das
// Anlegen (z. B. weil in einer abweichenden Bestandsdatenbank aktive Duplikate
// liegen), bleibt der Altindex stehen und es gibt nur eine Warnung statt eines
// Startabbruchs - der Altindex ist strenger und schützt weiter. Es wird nie
// etwas gelöscht außer dem ersetzten Index.
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

// removeOrphanedUserStatusPageRoles räumt einmalig Zuordnungen
// auf, deren Benutzer oder Rolle per Soft-Delete gelöscht wurde. Soft-Delete
// löst das FK-OnDelete nie aus, die Handler räumen erst seit der Umstellung selbst auf.
// Hard-Delete der Join-Zeilen wie in replaceUserPageRoles; die Zeilen waren
// ohnehin wirkungslos (die Rolle bzw. der Benutzer ist nicht mehr ladbar).
// Idempotent: nach dem ersten Lauf trifft das Statement nichts mehr.
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

// removeOrphanedRoleAndUserReferences räumt einmalig Altlasten auf:
// StatusPage.DefaultRoleID, die auf eine gelöschte Rolle zeigt, wird auf NULL
// gesetzt; UserTag und UserSSOIdentity gelöschter Benutzer werden entfernt
// (Hard-Delete der Join-Zeilen wie in me_delete). Die Handler räumen seit der Umstellung
// selbst auf. Idempotent: nach dem ersten Lauf trifft kein Statement mehr.
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

// PageOwnerRoleName ist der Name der Standard-Seitenrolle mit pages:write
// .
const PageOwnerRoleName = "Page Owner"

// pageOwnerSeededKey markiert (SystemSetting), dass die Rolle einmal angelegt wurde.
const pageOwnerSeededKey = "seed.page_owner_role"

// PageOwnerPermissions sind die Rechte der Rolle "Page Owner": alle Rechte von
// "Page Admin" plus pages:write (Benutzer der Seite, SSO, Domain, Mail-Templates,
// Sprachen). "Page Admin" bleibt bewusst unverändert.
//
// aus der alten Rechteliste abgeleitet (permissions.MigrateLegacy),
// damit die Rolle exakt die Wirkung von früher behält - also create/read/update/
// delete auf pages, Komponenten, Externe Quellen, Vorfälle, Wartung,
// Vorfallvorlagen, Hinweise, Abonnenten, Mail-Templates, Sprachen, UI-Texte und SSO.
var PageOwnerPermissions = permissions.MigrateLegacy(pageAdminLegacyPermissions + ",pages:write")

// pageAdminLegacyPermissions ist die Rechteliste der Rolle "Page Admin" vor der Umstellung.
const pageAdminLegacyPermissions = "components:write,incidents:write,notices:write,subscribers:write," + permissions.AdminDashboard

// seedPageOwnerRole legt die Standardrolle "Page Owner" an. Seit der
// Rechtereform ist "Global Admin" nicht mehr als Seitenrolle vergebbar und war im Seed
// die einzige Rolle mit pages:write; wer einen Seiten-Admin mit
// Benutzerverwaltung wollte, musste eine eigene Rolle bauen. Läuft bei jedem
// Start, unabhängig von SEED_DB (die Rolle wird auch in Produktion gebraucht),
// und ist idempotent und Admin-sicher: Die Prüfung läuft Unscoped, existiert
// also schon eine Rolle gleichen Namens (auch eine vom Betreiber angepasste
// oder bewusst gelöschte), wird nichts angelegt oder überschrieben.
func seedPageOwnerRole(db *gorm.DB) {
	// Stabiles Merkmal statt Name: Ist der Seed einmal gelaufen,
	// gilt er als erledigt - auch wenn der Betreiber die Rolle umbenennt oder
	// löscht. Sonst entstünde nach jeder Umbenennung beim Start eine neue Rolle.
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

// migrateTagsFromCountryStrings überträgt (Schritt 1/4 -
// Country → Tags) die früheren Country-Strings additiv in das Tag-Datenmodell,
// analog zu migrateIncidentComponents(): Raw-SQL-Check der Bestandsdaten +
// Insert, sofern die Zuordnung noch nicht existiert.
//
// Seit Schritt 4/4 existieren die Go-Felder Component.Country,
// Incident.Country und User.AllowedCountries nicht mehr - AutoMigrate legt
// ihre DB-Spalten (country/allowed_countries) deshalb auf KEINER neuen
// Datenbank mehr an (Tests, frische Deployments). Auf bereits laufenden
// Installationen (wo die Spalten aus einer früheren
// Modell-Version noch physisch existieren, Zwei-Phasen-Cutover) bleibt diese Funktion wie gehabt wirksam. Statt eines
// harten log.Fatalf bei fehlender Spalte (das den kompletten Serverstart auf
// jeder neuen DB zum Absturz brächte) wird ein Query-Fehler hier als "nichts
// zu migrieren" gewertet und die Funktion bricht den betroffenen Schritt
// weich ab - die Migration ist ohnehin bereits für jede Bestandsinstallation
// gelaufen, sobald sie einmal deployed wurde.
func migrateTagsFromCountryStrings(db *gorm.DB) {
	slog.Info("Migrating country strings into tags (no-op if legacy columns are already gone)...")

	type entityCountry struct {
		ID           uint
		Country      string
		StatusPageID uint
	}

	// 1. Components: distinkte, nicht-"global"-Country-Werte je Statusseite
	// werden zu je einem statusseiten-gebundenen Tag.
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

	// 2. Incidents: dasselbe Vorgehen wie für Components.
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

	// 3. Users: kommagetrennte allowed_countries (inkl. "global") werden zu
	// globalen Tags (StatusPageID = nil), da AllowedCountries seitenübergreifend
	// gilt.
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

// findOrCreateTag findet einen bestehenden Tag per fallunabhängigem
// Namensvergleich im angegebenen Scope (nil = global) oder legt ihn an -
// konsistent mit den partiellen Unique-Indizes aus CreateSoftDeleteUniqueIndexes.
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

// seedHashPassword ist HashPassword; als Variable, damit ein Test den
// Abbruch des Seeds bei einem Hash-Fehler auslösen kann.
var seedHashPassword = HashPassword

func SeedData() {
	slog.Info("Seeding initial data...")

	// --- Roles ---
	roles := []models.Role{
		// aus den früheren Listen abgeleitet (gleiche Wirkung wie vorher).
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
	// Name bewusst ohne "Status"-Suffix: die H1 auf der öffentlichen Seite hängt
	// "Status" selbst an ({{ pageInfo.Name }} Status) – mit "Acme Corp Status" als
	// Name kam es sonst zu "Acme Corp Status Status".
	page1 := models.StatusPage{Name: "Acme Corp", Slug: "acme", Description: "Offizielle Statusseite der Acme Corporation", IsPublicIndexed: true}
	page2 := models.StatusPage{Name: "TechStart GmbH", Slug: "techstart", Description: "Statusseite für TechStart GmbH Produkte", IsPublicIndexed: true}
	DB.Create(&page1)
	DB.Create(&page2)

	DB.Create(&models.UserStatusPageRole{UserID: adminUser.ID, StatusPageID: page1.ID, RoleID: globalAdminRole.ID})
	DB.Create(&models.UserStatusPageRole{UserID: adminUser.ID, StatusPageID: page2.ID, RoleID: globalAdminRole.ID})
	DB.Create(&models.UserStatusPageRole{UserID: editorUser.ID, StatusPageID: page1.ID, RoleID: editorRole.ID})
	// Bewusst abweichend von der globalen Rolle: "viewer" ist global nur "Viewer",
	// hat aber auf Acme Corp Status "Page Admin" – reproduziert den gemeldeten Bug,
	// bei dem das UI weiterhin die globale (veraltete) Rolle statt der
	// seitenbezogenen Rolle anzeigte.
	DB.Create(&models.UserStatusPageRole{UserID: viewerUser.ID, StatusPageID: page1.ID, RoleID: pageAdminRole.ID})

	// Google-Chat-Abo ohne Email, dem admin per CreatedByUserID zugeordnet – Fixture
	// fürs e2e "Mein Abo bearbeiten" (deckt Abos ab, die sich nicht über den
	// Email-String, sondern nur über den Account-Link wiederfinden lassen).
	DB.Create(&models.Subscriber{
		StatusPageID:           page1.ID,
		Channel:                "google_chat",
		WebhookURL:             "https://chat.googleapis.com/v1/spaces/SEED-DEMO/messages",
		SubscribedComponentIDs: "all",
		CreatedByUserID:        &adminUser.ID,
	})

	// ============================================================
	// Acme Corp: Verschachtelte Komponentenstruktur (Ordner-Baum)
	// ============================================================
	//
	// 📁 Core Infrastructure
	//    📁 API Services
	//       🟢 API Gateway (DE)
	//       🟡 API Gateway (US)
	//    📁 Datenbanken
	//       🟢 PostgreSQL Primary
	//       🟢 Redis Cache
	// 📁 Externe Dienste
	//    🟢 Payment Processor (Stripe)
	//    🟢 E-Mail Service (Mailgun)
	// 📁 Frontend & CDN
	//    🟠 Storage & CDN
	//    🟢 Web Application

	// Ebene 1: Root-Gruppen
	grpInfra := models.Component{StatusPageID: page1.ID, Name: "Core Infrastructure", Description: "Kerninfrastruktur und Backend-Dienste", Status: "group", SortOrder: 1}
	grpExternal := models.Component{StatusPageID: page1.ID, Name: "Externe Dienste", Description: "Drittanbieter-Integrationen", Status: "group", SortOrder: 2}
	grpFrontend := models.Component{StatusPageID: page1.ID, Name: "Frontend & Delivery", Description: "Web-Applikation und Content Delivery", Status: "group", SortOrder: 3}
	DB.Create(&grpInfra)
	DB.Create(&grpExternal)
	DB.Create(&grpFrontend)

	// Ebene 2: Untergruppen
	grpAPI := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpInfra.ID), Name: "API Services", Description: "REST und GraphQL API Gateways", Status: "group", SortOrder: 1}
	grpDB := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpInfra.ID), Name: "Datenbanken", Description: "Primäre und Cache-Datenbanken", Status: "group", SortOrder: 2}
	DB.Create(&grpAPI)
	DB.Create(&grpDB)

	// Ebene 3: Echte Komponenten unter API Services
	apiDE := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpAPI.ID), Name: "API Gateway (DE)", Description: "Zentraler API-Knotenpunkt Frankfurt (EU-West)", Status: "operational", SortOrder: 1}
	apiUS := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpAPI.ID), Name: "API Gateway (US)", Description: "Zentraler API-Knotenpunkt US East (Virginia)", Status: "degraded_performance", SortOrder: 2}
	DB.Create(&apiDE)
	DB.Create(&apiUS)

	// Ebene 3: Echte Komponenten unter Datenbanken
	pgPrimary := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpDB.ID), Name: "PostgreSQL Primary", Description: "Primäre relationale Datenbank", Status: "operational", SortOrder: 1}
	redisCache := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpDB.ID), Name: "Redis Cache", Description: "In-Memory Cache & Session Store", Status: "operational", SortOrder: 2}
	DB.Create(&pgPrimary)
	DB.Create(&redisCache)

	// Ebene 2: Einzelne externe Dienste (keine Untergruppe)
	stripe := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpExternal.ID), Name: "Payment Processor (Stripe)", Description: "Externe Zahlungsabwicklung via Stripe", Status: "operational", SortOrder: 1}
	mailgun := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpExternal.ID), Name: "E-Mail Service (Mailgun)", Description: "Transaktionale E-Mails und Newsletter", Status: "operational", SortOrder: 2}
	DB.Create(&stripe)
	DB.Create(&mailgun)

	// Ebene 2: Frontend & CDN
	cdn := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpFrontend.ID), Name: "Storage & CDN", Description: "Globales Asset Delivery Network", Status: "partial_outage", SortOrder: 1}
	webapp := models.Component{StatusPageID: page1.ID, ParentID: ptr(grpFrontend.ID), Name: "Web Application", Description: "Haupt-Webanwendung (Vue.js Frontend)", Status: "operational", SortOrder: 2}
	DB.Create(&cdn)
	DB.Create(&webapp)

	// Incidents für Acme (Komponenten-Zuordnung jetzt über
	// IncidentComponent statt direkter FK)
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
	// TechStart GmbH: Einfachere, flachere Struktur
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
	// Ersetzt die früheren Country-Freitextwerte ("global"/"DE"/"US") der
	// obigen Demo-Daten durch echte Tag-Zuordnungen - dieselbe Verteilung wie
	// zuvor, nur über das neue Modell statt eines Strings. Nutzt bewusst
	// dieselben Helper (findOrCreateTag/create*TagIfMissing), die auch
	// migrateTagsFromCountryStrings verwendet.
	globalTag := findOrCreateTag(DB, "global", nil)
	page1DETag := findOrCreateTag(DB, "DE", &page1.ID)
	page1USTag := findOrCreateTag(DB, "US", &page1.ID)
	page2DETag := findOrCreateTag(DB, "DE", &page2.ID)

	// Admin/Editor/Viewer: global (voller Zugriff, wie zuvor AllowedCountries="global")
	for _, u := range []models.User{adminUser, editorUser, viewerUser} {
		createUserTagIfMissing(DB, u.ID, globalTag.ID)
	}

	// Acme Corp: global-Komponenten + die beiden regionalen API-Gateways
	for _, compID := range []uint{grpInfra.ID, grpExternal.ID, grpFrontend.ID, grpAPI.ID, grpDB.ID, redisCache.ID, stripe.ID, mailgun.ID, cdn.ID, webapp.ID} {
		createComponentTagIfMissing(DB, compID, globalTag.ID)
	}
	createComponentTagIfMissing(DB, apiDE.ID, page1DETag.ID)
	createComponentTagIfMissing(DB, pgPrimary.ID, page1DETag.ID)
	createComponentTagIfMissing(DB, apiUS.ID, page1USTag.ID)

	createIncidentTagIfMissing(DB, acmeIncidents[0].Incident.ID, page1USTag.ID) // US-Gateway-Latenz
	createIncidentTagIfMissing(DB, acmeIncidents[1].Incident.ID, globalTag.ID)  // CDN-Bild-Assets

	// TechStart GmbH: Produkt global, Infrastruktur DE
	for _, compID := range []uint{grpProduct.ID, techAPI.ID} {
		createComponentTagIfMissing(DB, compID, globalTag.ID)
	}
	for _, compID := range []uint{grpInfra2.ID, techWeb.ID, techDB.ID} {
		createComponentTagIfMissing(DB, compID, page2DETag.ID)
	}
	createIncidentTagIfMissing(DB, techDBIncident.ID, page2DETag.ID)

	slog.Info("Database seeding completed successfully.")
}
