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

// newIntegrityTestDB creates the tables for the tests
// (without the unique indexes - those come from CreateSoftDeleteUniqueIndexes).
func newIntegrityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "integrity.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.StatusPage{}, &models.Role{}, &models.User{}, &models.UserStatusPageRole{},
		&models.Component{}, &models.ComponentTranslation{}, &models.Incident{}, &models.IncidentTranslation{},
		&models.GeneralNotice{}, &models.GeneralNoticeTranslation{}, &models.IncidentTemplate{}, &models.IncidentTemplateTranslation{}, &models.NotificationTemplate{}, &models.Tag{}, &models.SystemSetting{},
	))
	return db
}

// requireReusable checks the pattern create → duplicate fails → delete →
// re-create succeeds. mk returns a fresh row each time with an identical
// business key.
func requireReusable(t *testing.T, db *gorm.DB, mk func() any) {
	t.Helper()
	first := mk()
	require.NoError(t, db.Create(first).Error)
	require.Error(t, db.Create(mk()).Error, "aktiver Schlüssel muss eindeutig bleiben")
	require.NoError(t, db.Delete(first).Error)
	require.NoError(t, db.Create(mk()).Error, "gelöschter Schlüssel muss wieder frei sein")
}

// TestSoftDeleteUniqueIndexes_NamesAndTranslationsReusable:
// role name, user email, NotificationTemplate and the three
// translation tables were not reusable after deletion, because their
// unique indexes also covered soft-deleted rows.
func TestSoftDeleteUniqueIndexes_NamesAndTranslationsReusable(t *testing.T) {
	db := newIntegrityTestDB(t)
	CreateSoftDeleteUniqueIndexes(db)
	CreateSoftDeleteUniqueIndexes(db) // idempotent

	page := models.StatusPage{Name: "P", Slug: "p"}
	require.NoError(t, db.Create(&page).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "I"}
	require.NoError(t, db.Create(&inc).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "C"}
	require.NoError(t, db.Create(&comp).Error)
	notice := models.GeneralNotice{StatusPageID: page.ID}
	require.NoError(t, db.Create(&notice).Error)
	tmpl := models.IncidentTemplate{StatusPageID: page.ID, Name: "n", Title: "t", Message: "m"}
	require.NoError(t, db.Create(&tmpl).Error)

	cases := map[string]func() any{
		"Rolle":    func() any { return &models.Role{Name: "Editor", Permissions: "x"} },
		"Benutzer": func() any { return &models.User{Email: "a@example.com"} },
		"NotificationTemplate": func() any {
			return &models.NotificationTemplate{StatusPageID: page.ID, Kind: "k", Audience: "staff", Language: "de", Subject: "s", BodyHTML: "b"}
		},
		"IncidentTranslation": func() any {
			return &models.IncidentTranslation{IncidentID: inc.ID, LanguageCode: "de", Title: "t", Message: "m"}
		},
		"ComponentTranslation": func() any {
			return &models.ComponentTranslation{ComponentID: comp.ID, LanguageCode: "de", Title: "t", Description: "d"}
		},
		"IncidentTemplateTranslation": func() any {
			return &models.IncidentTemplateTranslation{IncidentTemplateID: tmpl.ID, LanguageCode: "de", Title: "t", Message: "m"}
		},
		"GeneralNoticeTranslation": func() any {
			return &models.GeneralNoticeTranslation{GeneralNoticeID: notice.ID, LanguageCode: "de", Title: "t", Message: "m"}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) { requireReusable(t, db, mk) })
	}
}

// TestCreateSoftDeleteUniqueIndexes_ReplacesLegacyNameIndexes: an existing
// database with the old indexes (gorm tags) is switched over, data stays.
func TestCreateSoftDeleteUniqueIndexes_ReplacesLegacyNameIndexes(t *testing.T) {
	db := newIntegrityTestDB(t)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_roles_name ON roles (name)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_users_email ON users (email)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_notification_template ON notification_templates (status_page_id, kind, audience, language)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_incident_translation ON incident_translations (incident_id, language_code)`).Error)
	require.NoError(t, db.Create(&models.Role{Name: "Editor", Permissions: "x"}).Error)

	CreateSoftDeleteUniqueIndexes(db)
	CreateSoftDeleteUniqueIndexes(db)

	assert.Equal(t, []string{"idx_roles_name_active"}, indexNames(t, db, "roles"))
	assert.Equal(t, []string{"idx_users_email_active"}, indexNames(t, db, "users"))
	assert.Equal(t, []string{"idx_notification_template_active"}, indexNames(t, db, "notification_templates"))
	assert.Equal(t, []string{"idx_incident_translation_active"}, indexNames(t, db, "incident_translations"))
	var n int64
	require.NoError(t, db.Model(&models.Role{}).Where("name = ?", "Editor").Count(&n).Error)
	assert.EqualValues(t, 1, n, "Bestandsdaten dürfen nicht verändert werden")
}

// TestCreateSoftDeleteUniqueIndexes_KeepsLegacyOnFailure: if
// the new index cannot be created (active duplicates in a deviating
// existing database), the legacy index stays, nothing is deleted and
// startup does not abort.
func TestCreateSoftDeleteUniqueIndexes_KeepsLegacyOnFailure(t *testing.T) {
	db := newIntegrityTestDB(t)
	require.NoError(t, db.Create(&models.Role{Name: "Dup", Permissions: "x"}).Error)
	require.NoError(t, db.Create(&models.Role{Name: "Dup", Permissions: "y"}).Error)
	// Non-unique legacy index of the same name: it must not be removed.
	require.NoError(t, db.Exec(`CREATE INDEX idx_roles_name ON roles (name)`).Error)

	assert.NotPanics(t, func() { CreateSoftDeleteUniqueIndexes(db) })

	assert.Equal(t, []string{"idx_roles_name"}, indexNames(t, db, "roles"))
	var n int64
	require.NoError(t, db.Model(&models.Role{}).Count(&n).Error)
	assert.EqualValues(t, 2, n, "es wird nie etwas gelöscht")
}

// TestRemoveOrphanedUserStatusPageRoles: assignments of deleted
// users or roles are removed once, all others stay; the
// second run changes nothing.
func TestRemoveOrphanedUserStatusPageRoles(t *testing.T) {
	db := newIntegrityTestDB(t)
	page := models.StatusPage{Name: "P", Slug: "p"}
	require.NoError(t, db.Create(&page).Error)
	roleOK := models.Role{Name: "Ok", Permissions: "x"}
	roleGone := models.Role{Name: "Gone", Permissions: "x"}
	userOK := models.User{Email: "ok@example.com"}
	userGone := models.User{Email: "gone@example.com"}
	for _, v := range []any{&roleOK, &roleGone, &userOK, &userGone} {
		require.NoError(t, db.Create(v).Error)
	}
	link := func(u, r uint) {
		require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u, StatusPageID: page.ID, RoleID: r}).Error)
	}
	link(userOK.ID, roleOK.ID)
	link(userGone.ID, roleOK.ID)
	link(userOK.ID, roleGone.ID)
	require.NoError(t, db.Delete(&userGone).Error)
	require.NoError(t, db.Delete(&roleGone).Error)

	removeOrphanedUserStatusPageRoles(db)
	removeOrphanedUserStatusPageRoles(db)

	var rows []models.UserStatusPageRole
	require.NoError(t, db.Unscoped().Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, userOK.ID, rows[0].UserID)
	assert.Equal(t, roleOK.ID, rows[0].RoleID)
}

func countSystemPages(t *testing.T, db *gorm.DB) (all, flagged int64) {
	t.Helper()
	require.NoError(t, db.Unscoped().Model(&models.StatusPage{}).Count(&all).Error)
	require.NoError(t, db.Unscoped().Model(&models.StatusPage{}).Where("is_system = ?", true).Count(&flagged).Error)
	return all, flagged
}

// TestSeedSystemStatusPage_SlugChangeDoesNotCreateSecondPage: the
// system page is found via the marker; a changed slug does not
// create a second page on the next start.
func TestSeedSystemStatusPage_SlugChangeDoesNotCreateSecondPage(t *testing.T) {
	db := newIntegrityTestDB(t)
	CreateSoftDeleteUniqueIndexes(db)
	SeedSystemStatusPage(db)
	require.NoError(t, db.Model(&models.StatusPage{}).Where("is_system = ?", true).Update("slug", "umbenannt").Error)

	SeedSystemStatusPage(db)

	all, flagged := countSystemPages(t, db)
	assert.EqualValues(t, 1, all, "es darf keine zweite Systemseite entstehen")
	assert.EqualValues(t, 1, flagged)
	var comps int64
	require.NoError(t, db.Model(&models.Component{}).Count(&comps).Error)
	assert.EqualValues(t, capabilityComponentCount(), comps, "Komponenten dürfen nicht verdoppelt werden")
}

// TestSeedSystemStatusPage_BackfillsLegacyPage: an existing
// system page without marker gets it once; nothing is duplicated.
func TestSeedSystemStatusPage_BackfillsLegacyPage(t *testing.T) {
	db := newIntegrityTestDB(t)
	CreateSoftDeleteUniqueIndexes(db)
	legacy := models.StatusPage{Name: "System Status", Slug: SystemStatusPageSlug}
	require.NoError(t, db.Create(&legacy).Error)

	SeedSystemStatusPage(db)
	SeedSystemStatusPage(db)

	all, flagged := countSystemPages(t, db)
	assert.EqualValues(t, 1, all)
	assert.EqualValues(t, 1, flagged)
	var page models.StatusPage
	require.NoError(t, db.Where("is_system = ?", true).First(&page).Error)
	assert.Equal(t, legacy.ID, page.ID)
}

// TestSeedSystemStatusPage_BackfillPrefersActiveOldest: if there are several pages
// with the slug (active and deleted), the oldest active one receives the marker.
func TestSeedSystemStatusPage_BackfillPrefersActiveOldest(t *testing.T) {
	db := newIntegrityTestDB(t) // deliberately without a partial index: several pages possible
	deleted := models.StatusPage{Name: "alt", Slug: SystemStatusPageSlug}
	active := models.StatusPage{Name: "aktiv", Slug: SystemStatusPageSlug}
	active2 := models.StatusPage{Name: "aktiv2", Slug: SystemStatusPageSlug}
	for _, p := range []*models.StatusPage{&deleted, &active, &active2} {
		require.NoError(t, db.Create(p).Error)
	}
	require.NoError(t, db.Delete(&deleted).Error)

	SeedSystemStatusPage(db)

	var page models.StatusPage
	require.NoError(t, db.Where("is_system = ?", true).First(&page).Error)
	assert.Equal(t, active.ID, page.ID)
	all, flagged := countSystemPages(t, db)
	assert.EqualValues(t, 3, all)
	assert.EqualValues(t, 1, flagged)
}

// TestSystemPageIndex_AllowsOnlyOneActiveSystemPage.
func TestSystemPageIndex_AllowsOnlyOneActiveSystemPage(t *testing.T) {
	db := newIntegrityTestDB(t)
	CreateSoftDeleteUniqueIndexes(db)
	require.NoError(t, db.Create(&models.StatusPage{Name: "a", Slug: "a", IsSystem: true}).Error)
	assert.Error(t, db.Create(&models.StatusPage{Name: "b", Slug: "b", IsSystem: true}).Error)
	require.NoError(t, db.Create(&models.StatusPage{Name: "c", Slug: "c"}).Error)
	require.NoError(t, db.Create(&models.StatusPage{Name: "d", Slug: "d"}).Error, "mehrere normale Seiten sind erlaubt")
}
