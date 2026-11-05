package database

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBuildDSN(t *testing.T) {
	clearDSNEnv := func() {
		_ = os.Unsetenv("DB_DSN")
		_ = os.Unsetenv("DB_HOST")
		_ = os.Unsetenv("DB_USER")
		_ = os.Unsetenv("DB_PASSWORD")
		_ = os.Unsetenv("DB_NAME")
		_ = os.Unsetenv("DB_PORT")
	}

	t.Run("uses DB_DSN verbatim when set", func(t *testing.T) {
		clearDSNEnv()
		defer clearDSNEnv()
		_ = os.Setenv("DB_DSN", "host=custom-host user=me password=placeholder-value-xyz dbname=mydb port=1234 sslmode=require")

		got := buildDSN()
		want := "host=custom-host user=me password=placeholder-value-xyz dbname=mydb port=1234 sslmode=require"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("falls back to defaults when nothing set", func(t *testing.T) {
		clearDSNEnv()
		defer clearDSNEnv()

		got := buildDSN()
		want := "host=localhost user=postgres password=localpassword dbname=statushafen port=5432 sslmode=disable TimeZone=UTC"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("assembles from individual env vars when DB_DSN unset", func(t *testing.T) {
		clearDSNEnv()
		defer clearDSNEnv()
		_ = os.Setenv("DB_HOST", "db.internal")
		_ = os.Setenv("DB_USER", "svc")
		_ = os.Setenv("DB_PASSWORD", "placeholder-value-abc")
		_ = os.Setenv("DB_NAME", "statushafen_prod")
		_ = os.Setenv("DB_PORT", "6543")

		got := buildDSN()
		want := "host=db.internal user=svc password=placeholder-value-abc dbname=statushafen_prod port=6543 sslmode=disable TimeZone=UTC"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})
}

func TestEnvOrDefault(t *testing.T) {
	t.Run("returns env value when set", func(t *testing.T) {
		_ = os.Setenv("TEST_ENV_OR_DEFAULT_KEY", "custom-value")
		defer func() { _ = os.Unsetenv("TEST_ENV_OR_DEFAULT_KEY") }()

		if got := envOrDefault("TEST_ENV_OR_DEFAULT_KEY", "fallback"); got != "custom-value" {
			t.Fatalf("expected custom-value, got %q", got)
		}
	})

	t.Run("returns fallback when unset", func(t *testing.T) {
		_ = os.Unsetenv("TEST_ENV_OR_DEFAULT_KEY_UNSET")

		if got := envOrDefault("TEST_ENV_OR_DEFAULT_KEY_UNSET", "fallback"); got != "fallback" {
			t.Fatalf("expected fallback, got %q", got)
		}
	})

	t.Run("returns fallback when env is empty string", func(t *testing.T) {
		_ = os.Setenv("TEST_ENV_OR_DEFAULT_KEY_EMPTY", "")
		defer func() { _ = os.Unsetenv("TEST_ENV_OR_DEFAULT_KEY_EMPTY") }()

		if got := envOrDefault("TEST_ENV_OR_DEFAULT_KEY_EMPTY", "fallback"); got != "fallback" {
			t.Fatalf("expected fallback for empty env value, got %q", got)
		}
	})
}

// setupTagMigrationTestDB spins up an isolated in-memory-ish SQLite DB (file
// based, like testutils.SetupTestDB) and AutoMigrates exactly the models
// touched by the Country -> Tags migration. It deliberately does
// not depend on the testutils package, since testutils imports this
// (database) package and that would create an import cycle.
func setupTagMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbName := filepath.Join(t.TempDir(), "tagmigration.db")
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(
		&models.StatusPage{},
		&models.Role{},
		&models.User{},
		&models.Component{},
		&models.Incident{},
		&models.Tag{},
		&models.ComponentTag{},
		&models.IncidentTag{},
		&models.UserTag{},
	)
	require.NoError(t, err)

	// Component.Country/Incident.Country/User.AllowedCountries wurden aus den
	// Go-Structs entfernt, AutoMigrate legt ihre Spalten deshalb
	// nicht mehr an. Diese Tests decken aber genau die Migration DIESER
	// (früher physisch vorhandenen) Spalten ab - sie hier manuell nachbilden,
	// damit setLegacyCountry/setLegacyAllowedCountries unten funktionieren.
	require.NoError(t, db.Exec(`ALTER TABLE components ADD COLUMN country TEXT`).Error)
	require.NoError(t, db.Exec(`ALTER TABLE incidents ADD COLUMN country TEXT`).Error)
	require.NoError(t, db.Exec(`ALTER TABLE users ADD COLUMN allowed_countries TEXT`).Error)

	return db
}

// setLegacyCountry/setLegacyAllowedCountries schreiben direkt in die
// nachgebildeten Legacy-Spalten (siehe setupTagMigrationTestDB) - Ersatz für
// die entfernten Go-Felder Component.Country/Incident.Country/
// User.AllowedCountries, die migrateTagsFromCountryStrings testweise
// weiterhin lesen können muss.
func setLegacyCountry(t *testing.T, db *gorm.DB, table string, id uint, country string) {
	t.Helper()
	require.NoError(t, db.Exec(`UPDATE `+table+` SET country = ? WHERE id = ?`, country, id).Error)
}

func setLegacyAllowedCountries(t *testing.T, db *gorm.DB, userID uint, allowedCountries string) {
	t.Helper()
	require.NoError(t, db.Exec(`UPDATE users SET allowed_countries = ? WHERE id = ?`, allowedCountries, userID).Error)
}

// tagNamesFor returns the (sorted) tag names assigned to an entity via the
// given join table, e.g. "component_tags" / "component_id" or
// "incident_tags" / "incident_id" or "user_tags" / "user_id".
func tagNamesFor(t *testing.T, db *gorm.DB, joinTable, fkColumn string, entityID uint) []string {
	t.Helper()
	var names []string
	err := db.Raw(
		`SELECT tags.name FROM tags JOIN `+joinTable+` ON `+joinTable+`.tag_id = tags.id WHERE `+joinTable+`.`+fkColumn+` = ?`,
		entityID,
	).Scan(&names).Error
	require.NoError(t, err)
	sort.Strings(names)
	return names
}

// splitCountries mirrors how the old access-check code (UserHasAccessToCountry)
// parses a comma-separated AllowedCountries string, so tests can compare the
// migrated tag set against the previous access set.
func splitCountries(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

func TestMigrateTagsFromCountryStrings_ComponentsAndIncidents(t *testing.T) {
	db := setupTagMigrationTestDB(t)

	page1 := models.StatusPage{Name: "Page 1", Slug: "page-1"}
	page2 := models.StatusPage{Name: "Page 2", Slug: "page-2"}
	require.NoError(t, db.Create(&page1).Error)
	require.NoError(t, db.Create(&page2).Error)

	compGlobal := models.Component{StatusPageID: page1.ID, Name: "Global Component", Status: "operational"}
	compDE := models.Component{StatusPageID: page1.ID, Name: "DE Component", Status: "operational"}
	compDEOtherPage := models.Component{StatusPageID: page2.ID, Name: "DE Component (Page 2)", Status: "operational"}
	require.NoError(t, db.Create(&compGlobal).Error)
	require.NoError(t, db.Create(&compDE).Error)
	require.NoError(t, db.Create(&compDEOtherPage).Error)
	setLegacyCountry(t, db, "components", compGlobal.ID, "global")
	setLegacyCountry(t, db, "components", compDE.ID, "DE")
	setLegacyCountry(t, db, "components", compDEOtherPage.ID, "DE")

	incGlobal := models.Incident{StatusPageID: page1.ID, Title: "Global Incident", Message: "msg", Status: "investigating"}
	incDE := models.Incident{StatusPageID: page1.ID, Title: "DE Incident", Message: "msg", Status: "investigating"}
	require.NoError(t, db.Create(&incGlobal).Error)
	require.NoError(t, db.Create(&incDE).Error)
	setLegacyCountry(t, db, "incidents", incGlobal.ID, "global")
	setLegacyCountry(t, db, "incidents", incDE.ID, "DE")

	CreateSoftDeleteUniqueIndexes(db)
	migrateTagsFromCountryStrings(db)

	// "global" components/incidents get no tag row (item 1 of the Verifikation
	// spec: only non-"global" values are migrated in this step).
	assert.Empty(t, tagNamesFor(t, db, "component_tags", "component_id", compGlobal.ID))
	assert.Empty(t, tagNamesFor(t, db, "incident_tags", "incident_id", incGlobal.ID))

	// Non-global values produce exactly the previous access value as a tag.
	assert.Equal(t, []string{"DE"}, tagNamesFor(t, db, "component_tags", "component_id", compDE.ID))
	assert.Equal(t, []string{"DE"}, tagNamesFor(t, db, "component_tags", "component_id", compDEOtherPage.ID))
	assert.Equal(t, []string{"DE"}, tagNamesFor(t, db, "incident_tags", "incident_id", incDE.ID))

	// The two "DE" components on different status pages must resolve to two
	// distinct, page-scoped tags (not the same row), while the "DE" component
	// and "DE" incident on the SAME status page must share one tag.
	var compDETag, compDEOtherPageTag, incDETag models.Tag
	require.NoError(t, db.Joins("JOIN component_tags ON component_tags.tag_id = tags.id").
		Where("component_tags.component_id = ?", compDE.ID).First(&compDETag).Error)
	require.NoError(t, db.Joins("JOIN component_tags ON component_tags.tag_id = tags.id").
		Where("component_tags.component_id = ?", compDEOtherPage.ID).First(&compDEOtherPageTag).Error)
	require.NoError(t, db.Joins("JOIN incident_tags ON incident_tags.tag_id = tags.id").
		Where("incident_tags.incident_id = ?", incDE.ID).First(&incDETag).Error)

	assert.Equal(t, compDETag.ID, incDETag.ID, "DE component and DE incident on the same status page should share one scoped tag")
	assert.NotEqual(t, compDETag.ID, compDEOtherPageTag.ID, "DE tags on different status pages must be distinct rows")
	require.NotNil(t, compDETag.StatusPageID)
	assert.Equal(t, page1.ID, *compDETag.StatusPageID)
	require.NotNil(t, compDEOtherPageTag.StatusPageID)
	assert.Equal(t, page2.ID, *compDEOtherPageTag.StatusPageID)

	var tagCount int64
	require.NoError(t, db.Model(&models.Tag{}).Count(&tagCount).Error)
	assert.EqualValues(t, 2, tagCount, "expected exactly one DE tag per status page, no duplicates")
}

func TestMigrateTagsFromCountryStrings_Users(t *testing.T) {
	db := setupTagMigrationTestDB(t)

	userGlobal := models.User{Email: "global@example.com", PasswordHash: "x"}
	userSingle := models.User{Email: "de@example.com", PasswordHash: "x"}
	userMulti := models.User{Email: "multi@example.com", PasswordHash: "x"}
	userGlobalAgain := models.User{Email: "global2@example.com", PasswordHash: "x"}
	require.NoError(t, db.Create(&userGlobal).Error)
	require.NoError(t, db.Create(&userSingle).Error)
	require.NoError(t, db.Create(&userMulti).Error)
	require.NoError(t, db.Create(&userGlobalAgain).Error)
	setLegacyAllowedCountries(t, db, userGlobal.ID, "global")
	setLegacyAllowedCountries(t, db, userSingle.ID, "DE")
	setLegacyAllowedCountries(t, db, userMulti.ID, "DE, AT, CH")
	setLegacyAllowedCountries(t, db, userGlobalAgain.ID, "global")

	CreateSoftDeleteUniqueIndexes(db)
	migrateTagsFromCountryStrings(db)

	// Resulting tag assignment per user must exactly match the previous
	// comma-separated AllowedCountries access set (Verifikation:
	// Vorher/Nachher-Vergleich), including "global" for users.
	assert.Equal(t, splitCountries("global"), tagNamesFor(t, db, "user_tags", "user_id", userGlobal.ID))
	assert.Equal(t, splitCountries("DE"), tagNamesFor(t, db, "user_tags", "user_id", userSingle.ID))
	assert.Equal(t, splitCountries("DE, AT, CH"), tagNamesFor(t, db, "user_tags", "user_id", userMulti.ID))
	assert.Equal(t, []string{"AT", "CH", "DE"}, tagNamesFor(t, db, "user_tags", "user_id", userMulti.ID))

	// Both "global" users must share the very same global tag row.
	var tagGlobal1, tagGlobal2 models.Tag
	require.NoError(t, db.Joins("JOIN user_tags ON user_tags.tag_id = tags.id").
		Where("user_tags.user_id = ?", userGlobal.ID).First(&tagGlobal1).Error)
	require.NoError(t, db.Joins("JOIN user_tags ON user_tags.tag_id = tags.id").
		Where("user_tags.user_id = ?", userGlobalAgain.ID).First(&tagGlobal2).Error)
	assert.Equal(t, tagGlobal1.ID, tagGlobal2.ID)
	assert.Nil(t, tagGlobal1.StatusPageID, "user tags are always global (StatusPageID nil)")

	var tagCount int64
	require.NoError(t, db.Model(&models.Tag{}).Count(&tagCount).Error)
	assert.EqualValues(t, 4, tagCount, "expected one global + DE + AT + CH tag, deduplicated")
}

func TestMigrateTagsFromCountryStrings_Idempotent(t *testing.T) {
	db := setupTagMigrationTestDB(t)

	page1 := models.StatusPage{Name: "Page 1", Slug: "page-1"}
	require.NoError(t, db.Create(&page1).Error)

	compDE := models.Component{StatusPageID: page1.ID, Name: "DE Component", Status: "operational"}
	require.NoError(t, db.Create(&compDE).Error)
	setLegacyCountry(t, db, "components", compDE.ID, "DE")

	incDE := models.Incident{StatusPageID: page1.ID, Title: "DE Incident", Message: "msg", Status: "investigating"}
	require.NoError(t, db.Create(&incDE).Error)
	setLegacyCountry(t, db, "incidents", incDE.ID, "DE")

	userMulti := models.User{Email: "multi@example.com", PasswordHash: "x"}
	require.NoError(t, db.Create(&userMulti).Error)
	setLegacyAllowedCountries(t, db, userMulti.ID, "DE, AT, CH")
	userGlobal := models.User{Email: "global@example.com", PasswordHash: "x"}
	require.NoError(t, db.Create(&userGlobal).Error)
	setLegacyAllowedCountries(t, db, userGlobal.ID, "global")

	CreateSoftDeleteUniqueIndexes(db)

	// Run the migration twice in a row, as the same function is (re-)invoked
	// on every InitDB() call in production.
	migrateTagsFromCountryStrings(db)
	migrateTagsFromCountryStrings(db)
	migrateTagsFromCountryStrings(db)

	var tagCount, componentTagCount, incidentTagCount, userTagCount int64
	require.NoError(t, db.Model(&models.Tag{}).Count(&tagCount).Error)
	require.NoError(t, db.Model(&models.ComponentTag{}).Count(&componentTagCount).Error)
	require.NoError(t, db.Model(&models.IncidentTag{}).Count(&incidentTagCount).Error)
	require.NoError(t, db.Model(&models.UserTag{}).Count(&userTagCount).Error)

	// DE-scoped-to-page1 (shared by component+incident) + DE-global + AT-global
	// + CH-global + global-global = 5 tags. Component/Incident tags are scoped
	// per status page while User tags are always global, so the user's "DE"
	// intentionally does NOT reuse the component/incident's page-scoped "DE" tag.
	assert.EqualValues(t, 5, tagCount, "repeated migration runs must not create duplicate tags")
	assert.EqualValues(t, 1, componentTagCount, "repeated migration runs must not create duplicate component_tags rows")
	assert.EqualValues(t, 1, incidentTagCount, "repeated migration runs must not create duplicate incident_tags rows")
	assert.EqualValues(t, 4, userTagCount, "repeated migration runs must not create duplicate user_tags rows")

	assert.Equal(t, []string{"AT", "CH", "DE"}, tagNamesFor(t, db, "user_tags", "user_id", userMulti.ID))
	assert.Equal(t, []string{"global"}, tagNamesFor(t, db, "user_tags", "user_id", userGlobal.ID))
}

// TestSeedSystemStatusPage_Idempotent: running the seed
// twice (as InitDB() does on every startup) must not create a duplicate
// system status page nor duplicate components under it.
func TestSeedSystemStatusPage_Idempotent(t *testing.T) {
	db := setupTagMigrationTestDB(t)

	SeedSystemStatusPage(db)
	SeedSystemStatusPage(db)
	SeedSystemStatusPage(db)

	var pageCount int64
	require.NoError(t, db.Model(&models.StatusPage{}).Where("slug = ?", SystemStatusPageSlug).Count(&pageCount).Error)
	assert.EqualValues(t, 1, pageCount, "repeated seed runs must not create duplicate system status pages")

	var page models.StatusPage
	require.NoError(t, db.Where("slug = ?", SystemStatusPageSlug).First(&page).Error)
	assert.Equal(t, "System Status", page.Name)
	assert.True(t, page.IsPublicIndexed)
	assert.False(t, page.IsPrivate)

	var componentCount int64
	require.NoError(t, db.Model(&models.Component{}).Where("status_page_id = ?", page.ID).Count(&componentCount).Error)
	assert.EqualValues(t, capabilityComponentCount(), componentCount, "repeated seed runs must not duplicate the standard components")

	var names []string
	require.NoError(t, db.Model(&models.Component{}).Where("status_page_id = ? AND parent_id IS NOT NULL", page.ID).Pluck("name", &names).Error)
	assert.Subset(t, names, []string{"API", "Datenbank", "Frontend", "E-Mail-Versand"})

	var statuses []string
	require.NoError(t, db.Model(&models.Component{}).Where("status_page_id = ?", page.ID).Pluck("status", &statuses).Error)
	for _, s := range statuses {
		assert.Contains(t, []string{"operational", "group"}, s)
	}
}

func TestCreateTagUniqueIndexes(t *testing.T) {
	db := setupTagMigrationTestDB(t)
	CreateSoftDeleteUniqueIndexes(db)

	// Calling it again must stay a no-op (IF NOT EXISTS) rather than error.
	CreateSoftDeleteUniqueIndexes(db)

	insertGlobalTag := func(name string) error {
		return db.Exec(`INSERT INTO tags (name, status_page_id, created_at, updated_at) VALUES (?, NULL, datetime('now'), datetime('now'))`, name).Error
	}
	insertScopedTag := func(name string, statusPageID uint) error {
		return db.Exec(`INSERT INTO tags (name, status_page_id, created_at, updated_at) VALUES (?, ?, datetime('now'), datetime('now'))`, name, statusPageID).Error
	}

	// Two distinct global tags: fine.
	require.NoError(t, insertGlobalTag("alpha"))
	require.NoError(t, insertGlobalTag("beta"))

	// Duplicate global tag name (even different case): rejected by
	// idx_tags_global_name.
	assert.Error(t, insertGlobalTag("alpha"))
	assert.Error(t, insertGlobalTag("ALPHA"))

	page1 := models.StatusPage{Name: "Page 1", Slug: "page-1"}
	page2 := models.StatusPage{Name: "Page 2", Slug: "page-2"}
	require.NoError(t, db.Create(&page1).Error)
	require.NoError(t, db.Create(&page2).Error)

	// Same name scoped to two different status pages: fine (independent
	// scopes), and independent from the global tag of the same name.
	require.NoError(t, insertScopedTag("gamma", page1.ID))
	require.NoError(t, insertScopedTag("gamma", page2.ID))
	require.NoError(t, insertGlobalTag("gamma"))

	// Duplicate scoped tag name within the SAME status page: rejected by
	// idx_tags_scoped_name.
	assert.Error(t, insertScopedTag("gamma", page1.ID))
	assert.Error(t, insertScopedTag("GAMMA", page1.ID))
}

// setupJoinTableTestDB spins up an isolated SQLite DB with the four
// soft-delete-fähigen many-to-many-Join-Tabellen und ihren
// Eltern-Modellen migriert.
func setupJoinTableTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbName := filepath.Join(t.TempDir(), "jointable.db")
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(
		&models.StatusPage{},
		&models.Role{},
		&models.User{},
		&models.Component{},
		&models.Incident{},
		&models.Tag{},
		&models.IncidentComponent{},
		&models.ComponentTag{},
		&models.IncidentTag{},
		&models.UserTag{},
		&models.StatusPageLanguage{},
		// ebenfalls in createJoinTableUniqueIndexes.
		&models.IncidentUpdate{},
		&models.IncidentUpdateTranslation{},
		// ebenfalls in createJoinTableUniqueIndexes.
		&models.ExternalSource{},
		&models.ExternalComponent{},
		&models.ExternalComponentLink{},
		// ebenfalls in createJoinTableUniqueIndexes.
		&models.ExternalIncident{},
		&models.ExternalIncidentUpdate{},
		&models.ExternalIncidentComponent{},
		// Admin-Nachrichtencenter.
		&models.AdminNotification{},
		&models.NotificationRead{},
		&models.ComponentStatusChange{},
		&models.PageInvitation{},
	)
	require.NoError(t, err)

	return db
}

func TestCreateJoinTableUniqueIndexes(t *testing.T) {
	db := setupJoinTableTestDB(t)
	createJoinTableUniqueIndexes(db)

	// Calling it again must stay a no-op (IF NOT EXISTS) rather than error.
	createJoinTableUniqueIndexes(db)

	page := models.StatusPage{Name: "Page", Slug: "page"}
	require.NoError(t, db.Create(&page).Error)
	comp1 := models.Component{StatusPageID: page.ID, Name: "Comp 1", Status: "operational"}
	comp2 := models.Component{StatusPageID: page.ID, Name: "Comp 2", Status: "operational"}
	require.NoError(t, db.Create(&comp1).Error)
	require.NoError(t, db.Create(&comp2).Error)
	incident := models.Incident{StatusPageID: page.ID, Title: "Incident", Message: "Investigating", Status: "investigating"}
	require.NoError(t, db.Create(&incident).Error)

	t.Run("idx_incident_component_active verhindert echtes Duplikat, aber nicht Recreate nach Soft-Delete", func(t *testing.T) {
		link1 := models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp1.ID}
		require.NoError(t, db.Create(&link1).Error)

		// Echtes Duplikat auf denselben (aktiven) (incident_id, component_id):
		// abgelehnt.
		dup := models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp1.ID}
		assert.Error(t, db.Create(&dup).Error)

		// Zwei verschiedene Components am selben Incident: erlaubt.
		link2 := models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp2.ID}
		require.NoError(t, db.Create(&link2).Error)

		// Soft-Delete gefolgt vom Recreate desselben Paares (wie es
		// replaceIncidentComponents bei einem "Update mit unveränderten
		// ComponentIDs" tut) muss ein No-Op sein, keine Unique-Verletzung.
		require.NoError(t, db.Delete(&link1).Error)
		recreated := models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp1.ID}
		require.NoError(t, db.Create(&recreated).Error)
	})

	t.Run("idx_component_tag_active / idx_incident_tag_active / idx_user_tag_active gleiches Verhalten", func(t *testing.T) {
		tag := models.Tag{Name: "prod"}
		require.NoError(t, db.Create(&tag).Error)
		user := models.User{Email: "user@example.com", PasswordHash: "placeholder-hash"}
		require.NoError(t, db.Create(&user).Error)

		compTag := models.ComponentTag{ComponentID: comp1.ID, TagID: tag.ID}
		require.NoError(t, db.Create(&compTag).Error)
		assert.Error(t, db.Create(&models.ComponentTag{ComponentID: comp1.ID, TagID: tag.ID}).Error)
		require.NoError(t, db.Delete(&compTag).Error)
		require.NoError(t, db.Create(&models.ComponentTag{ComponentID: comp1.ID, TagID: tag.ID}).Error)

		incTag := models.IncidentTag{IncidentID: incident.ID, TagID: tag.ID}
		require.NoError(t, db.Create(&incTag).Error)
		assert.Error(t, db.Create(&models.IncidentTag{IncidentID: incident.ID, TagID: tag.ID}).Error)
		require.NoError(t, db.Delete(&incTag).Error)
		require.NoError(t, db.Create(&models.IncidentTag{IncidentID: incident.ID, TagID: tag.ID}).Error)

		userTag := models.UserTag{UserID: user.ID, TagID: tag.ID}
		require.NoError(t, db.Create(&userTag).Error)
		assert.Error(t, db.Create(&models.UserTag{UserID: user.ID, TagID: tag.ID}).Error)
		require.NoError(t, db.Delete(&userTag).Error)
		require.NoError(t, db.Create(&models.UserTag{UserID: user.ID, TagID: tag.ID}).Error)
	})
}

// je (Update, Sprache) höchstens eine aktive Übersetzung; nach
// Soft-Delete ist die Sprache wieder frei, ein anderes Update ist unabhängig.
func TestCreateJoinTableUniqueIndexes_IncidentUpdateTranslation(t *testing.T) {
	db := setupJoinTableTestDB(t)
	createJoinTableUniqueIndexes(db)

	page := models.StatusPage{Name: "Page", Slug: "page"}
	require.NoError(t, db.Create(&page).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "I", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	u1 := models.IncidentUpdate{IncidentID: inc.ID, Status: "monitoring", Message: "eins"}
	u2 := models.IncidentUpdate{IncidentID: inc.ID, Status: "resolved", Message: "zwei"}
	require.NoError(t, db.Create(&u1).Error)
	require.NoError(t, db.Create(&u2).Error)

	first := models.IncidentUpdateTranslation{UpdateID: u1.ID, LanguageCode: "en", Message: "one"}
	require.NoError(t, db.Create(&first).Error)
	assert.Error(t, db.Create(&models.IncidentUpdateTranslation{UpdateID: u1.ID, LanguageCode: "en", Message: "dup"}).Error)
	require.NoError(t, db.Create(&models.IncidentUpdateTranslation{UpdateID: u1.ID, LanguageCode: "fr", Message: "un"}).Error)
	require.NoError(t, db.Create(&models.IncidentUpdateTranslation{UpdateID: u2.ID, LanguageCode: "en", Message: "two"}).Error)

	require.NoError(t, db.Delete(&first).Error)
	require.NoError(t, db.Create(&models.IncidentUpdateTranslation{UpdateID: u1.ID, LanguageCode: "en", Message: "again"}).Error)
}

// Wird das Update gelöscht, verschwinden seine Übersetzungen (FK mit
// OnDelete:CASCADE). SQLite erzwingt Fremdschlüssel nur mit PRAGMA.
func TestIncidentUpdateTranslation_CascadeBeimLoeschenDesUpdates(t *testing.T) {
	db := setupJoinTableTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = ON").Error)

	page := models.StatusPage{Name: "Page", Slug: "page"}
	require.NoError(t, db.Create(&page).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "I", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	u := models.IncidentUpdate{IncidentID: inc.ID, Status: "monitoring", Message: "eins"}
	require.NoError(t, db.Create(&u).Error)
	require.NoError(t, db.Create(&models.IncidentUpdateTranslation{UpdateID: u.ID, LanguageCode: "en", Message: "one"}).Error)

	require.NoError(t, db.Delete(&models.IncidentUpdate{}, u.ID).Error)

	var n int64
	require.NoError(t, db.Unscoped().Model(&models.IncidentUpdateTranslation{}).Count(&n).Error)
	assert.Zero(t, n)
}

// PageLayout kommt rein additiv per AutoMigrate. Bestehende Seiten
// (Tabelle ohne die Spalte) müssen danach "standard" haben, neue ebenso.
func TestMigratePageLayout_ExistingPagesGetStandard(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE status_pages (id integer primary key autoincrement, name text, slug text, header_style text not null default 'classic')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO status_pages (name, slug) VALUES ('Alt', 'alt')`).Error)

	require.NoError(t, db.AutoMigrate(&models.StatusPage{}))
	require.NoError(t, db.Create(&models.StatusPage{Name: "Neu", Slug: "neu"}).Error)

	var layouts []string
	require.NoError(t, db.Model(&models.StatusPage{}).Order("id").Pluck("page_layout", &layouts).Error)
	assert.Equal(t, []string{"standard", "standard"}, layouts)
}

// Eine Custom Domain gehört höchstens einer Seite, sobald sie verifiziert oder
// aktiv ist (ohne Beachtung der Schreibweise); unbewiesene Eintragungen dürfen
// sich überschneiden, gelöschte Seiten geben die Domain frei.
func TestCreateCustomDomainLiveUniqueIndex(t *testing.T) {
	db := setupTagMigrationTestDB(t)
	CreateSoftDeleteUniqueIndexes(db)
	CreateSoftDeleteUniqueIndexes(db) // idempotent

	n := 0
	insert := func(domain, status string) error {
		n++
		return db.Exec(`INSERT INTO status_pages (name, slug, custom_domain, custom_domain_status, created_at, updated_at)
			VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`, "P", "cd-"+strconv.Itoa(n), domain, status).Error
	}

	require.NoError(t, insert("status.acme.example", "pending"))
	require.NoError(t, insert("status.acme.example", "pending"), "unbewiesene Eintragungen dürfen sich überschneiden")
	require.NoError(t, insert("status.acme.example", "verified"), "eine verifizierte neben unbewiesenen")
	require.Error(t, insert("STATUS.acme.example", "active"), "zweite verifizierte/aktive Seite derselben Domain")
	require.NoError(t, insert("status.other.example", "active"), "andere Domain bleibt frei")

	require.NoError(t, db.Exec(`UPDATE status_pages SET deleted_at = datetime('now') WHERE custom_domain_status = 'verified'`).Error)
	require.NoError(t, insert("status.acme.example", "active"), "gelöschte Seite gibt die Domain frei")
}

// token_version kommt rein additiv per AutoMigrate. Bestehende Konten
// (Tabelle ohne die Spalte) starten mit 0, ein zweiter Lauf ändert nichts,
// und eine zwischenzeitlich erhöhte Version bleibt erhalten.
func TestMigrateUserTokenVersion_ExistingUsersStartAtZeroAndRerunKeepsValue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE users (id integer primary key autoincrement, email text not null, password_hash text)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO users (email) VALUES ('alt@example.com')`).Error)

	require.NoError(t, db.AutoMigrate(&models.User{}))
	var u models.User
	require.NoError(t, db.First(&u).Error)
	assert.Zero(t, u.TokenVersion)

	require.NoError(t, db.Model(&u).UpdateColumn("token_version", 4).Error)
	require.NoError(t, db.AutoMigrate(&models.User{}))
	require.NoError(t, db.First(&u).Error)
	assert.Equal(t, uint(4), u.TokenVersion)
}
