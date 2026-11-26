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

// TestInitDB_MigratesOIDCTables lets InitDB run entirely against SQLite
// The new OIDC tables must be in the production AutoMigrate list,
// not only in testutils.SetupTestDB.
func TestInitDB_MigratesOIDCTables(t *testing.T) {
	prevDialector, prevDB := openDialector, DB
	t.Cleanup(func() { openDialector, DB = prevDialector, prevDB })
	openDialector = sqlite.Open
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "initdb.db"))
	t.Setenv("SEED_DB", "")

	InitDB()

	assert.True(t, DB.Migrator().HasTable(&models.OIDCLoginState{}))
	assert.True(t, DB.Migrator().HasTable(&models.UserSSOIdentity{}))
	// SAML is gone: a fresh database no longer gets SAML tables (existing
	// ones stay, AutoMigrate deletes nothing).
	assert.False(t, DB.Migrator().HasTable("saml_login_states"))
	assert.False(t, DB.Migrator().HasTable("saml_assertion_uses"))
	// Security review: the NameID format is also part of the key.
	assert.True(t, DB.Migrator().HasIndex(&models.UserSSOIdentity{}, "idx_user_sso_identity_key"))
	assert.False(t, DB.Migrator().HasIndex(&models.UserSSOIdentity{}, "idx_user_sso_identity_protocol"))
	assert.False(t, DB.Migrator().HasIndex(&models.UserSSOIdentity{}, "idx_user_sso_identity"))
	assert.False(t, DB.Migrator().HasColumn(&models.StatusPageSSOConfig{}, "saml_sp_key"))
}

// TestDropLegacySSOIdentityIndex: an existing database still has
// the unique index without protocol. It must go (otherwise OIDC and
// SAML identities with the same issuer and subject collide), existing rows count as
// OIDC, and a second run changes nothing.
func TestDropLegacySSOIdentityIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE user_sso_identities (id integer PRIMARY KEY, user_id integer NOT NULL, status_page_id integer NOT NULL, issuer text NOT NULL, subject text NOT NULL, email text NOT NULL DEFAULT '', created_at datetime)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_user_sso_identity ON user_sso_identities (status_page_id, issuer, subject)`).Error)
	require.NoError(t, db.Exec(`ALTER TABLE user_sso_identities ADD COLUMN protocol text NOT NULL DEFAULT 'oidc'`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_user_sso_identity_protocol ON user_sso_identities (status_page_id, protocol, issuer, subject)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_sso_identities (user_id, status_page_id, issuer, subject) VALUES (1, 1, 'https://idp', 'sub')`).Error)

	require.NoError(t, db.AutoMigrate(&models.UserSSOIdentity{}))
	dropLegacySSOIdentityIndex(db)
	dropLegacySSOIdentityIndex(db)

	assert.False(t, db.Migrator().HasIndex(&models.UserSSOIdentity{}, "idx_user_sso_identity"))
	assert.False(t, db.Migrator().HasIndex(&models.UserSSOIdentity{}, "idx_user_sso_identity_protocol"))
	assert.True(t, db.Migrator().HasIndex(&models.UserSSOIdentity{}, "idx_user_sso_identity_key"))
	var ident models.UserSSOIdentity
	require.NoError(t, db.First(&ident).Error)
	assert.Equal(t, models.SSOProtocolOIDC, ident.Protocol)
	// Same issuer and subject, different protocol: now allowed.
	assert.NoError(t, db.Create(&models.UserSSOIdentity{UserID: 1, StatusPageID: 1, Protocol: "saml", Issuer: "https://idp", Subject: "sub"}).Error)
	// Same value, different NameID format: a separate identity.
	assert.NoError(t, db.Create(&models.UserSSOIdentity{UserID: 1, StatusPageID: 1, Protocol: "saml", Issuer: "https://idp", Subject: "sub", SubjectFormat: "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"}).Error)
	assert.Equal(t, "", ident.SubjectFormat)
}

// The status history is backfilled at InitDB for existing components
// (including those of the system page) and a second run changes nothing.
func TestInitDB_StatushistorieNachtragIdempotent(t *testing.T) {
	prevDialector, prevDB := openDialector, DB
	t.Cleanup(func() { openDialector, DB = prevDialector, prevDB })
	openDialector = sqlite.Open
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "history.db"))
	t.Setenv("SEED_DB", "")

	InitDB()
	require.True(t, DB.Migrator().HasTable(&models.ComponentStatusChange{}))
	require.True(t, DB.Migrator().HasIndex(&models.ComponentStatusChange{}, "idx_csc_component_time"))

	page := models.StatusPage{Name: "Alt", Slug: "alt-705"}
	require.NoError(t, DB.Create(&page).Error)
	down := models.Component{StatusPageID: page.ID, Name: "Down", Status: "major_outage"}
	require.NoError(t, DB.Create(&down).Error)
	InitDB()

	var n int64
	require.NoError(t, DB.Model(&models.ComponentStatusChange{}).Where("component_id = ? AND new_status = ? AND source = ?", down.ID, "major_outage", models.StatusSourceBackfill).Count(&n).Error)
	assert.EqualValues(t, 1, n)
	var gesamt, ohne int64
	require.NoError(t, DB.Model(&models.ComponentStatusChange{}).Count(&gesamt).Error)
	require.NoError(t, DB.Model(&models.Component{}).Where("status <> 'group' AND id NOT IN (SELECT component_id FROM component_status_changes)").Count(&ohne).Error)
	assert.Zero(t, ohne, "jede Komponente hat einen Stand, auch die der Systemseite")

	InitDB()
	var danach int64
	require.NoError(t, DB.Model(&models.ComponentStatusChange{}).Count(&danach).Error)
	assert.Equal(t, gesamt, danach, "zweiter Lauf ändert nichts")
}

func TestBackfillStatusHistory_FehlerBrichtNichtAb(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "leer.db")), &gorm.Config{})
	require.NoError(t, err)
	assert.NotPanics(t, func() { backfillStatusHistory(db) }) // tables missing: error is only logged
}
