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

// newIntegrityTestDB legt die Tabellen für die Tests
// an (ohne die Unique-Indizes - die kommen aus CreateSoftDeleteUniqueIndexes).
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

// requireReusable prüft das Muster Anlegen → Duplikat scheitert → Löschen →
// Neuanlegen gelingt. mk liefert jedes Mal eine frische Zeile mit identischem
// fachlichem Schlüssel.
func requireReusable(t *testing.T, db *gorm.DB, mk func() any) {
	t.Helper()
	first := mk()
	require.NoError(t, db.Create(first).Error)
	require.Error(t, db.Create(mk()).Error, "aktiver Schlüssel muss eindeutig bleiben")
	require.NoError(t, db.Delete(first).Error)
	require.NoError(t, db.Create(mk()).Error, "gelöschter Schlüssel muss wieder frei sein")
}

// TestSoftDeleteUniqueIndexes_NamesAndTranslationsReusable:
// Rollenname, Benutzer-E-Mail, NotificationTemplate und die drei
// Übersetzungstabellen waren nach dem Löschen nicht wiederverwendbar, weil ihre
// Unique-Indizes auch soft-gelöschte Zeilen erfassten.
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

// TestCreateSoftDeleteUniqueIndexes_ReplacesLegacyNameIndexes: Bestands-
// datenbank mit den alten Indizes (gorm-Tags) wird umgestellt, Daten bleiben.
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

// TestCreateSoftDeleteUniqueIndexes_KeepsLegacyOnFailure: Lässt
// sich der neue Index nicht anlegen (aktive Duplikate in einer abweichenden
// Bestandsdatenbank), bleibt der Altindex stehen, es wird nichts gelöscht und
// der Start bricht nicht ab.
func TestCreateSoftDeleteUniqueIndexes_KeepsLegacyOnFailure(t *testing.T) {
	db := newIntegrityTestDB(t)
	require.NoError(t, db.Create(&models.Role{Name: "Dup", Permissions: "x"}).Error)
	require.NoError(t, db.Create(&models.Role{Name: "Dup", Permissions: "y"}).Error)
	// Nicht-eindeutiger Altindex gleichen Namens: Er darf nicht entfernt werden.
	require.NoError(t, db.Exec(`CREATE INDEX idx_roles_name ON roles (name)`).Error)

	assert.NotPanics(t, func() { CreateSoftDeleteUniqueIndexes(db) })

	assert.Equal(t, []string{"idx_roles_name"}, indexNames(t, db, "roles"))
	var n int64
	require.NoError(t, db.Model(&models.Role{}).Count(&n).Error)
	assert.EqualValues(t, 2, n, "es wird nie etwas gelöscht")
}

// TestRemoveOrphanedUserStatusPageRoles: Zuordnungen gelöschter
// Benutzer oder Rollen werden einmalig entfernt, alle anderen bleiben; der
// zweite Lauf ändert nichts.
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

// TestSeedSystemStatusPage_SlugChangeDoesNotCreateSecondPage: Die
// Systemseite wird über das Kennzeichen gefunden; ein geänderter Slug lässt
// beim nächsten Start keine zweite Seite entstehen.
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

// TestSeedSystemStatusPage_BackfillsLegacyPage: Eine Bestands-
// Systemseite ohne Kennzeichen bekommt es einmalig; nichts wird verdoppelt.
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

// TestSeedSystemStatusPage_BackfillPrefersActiveOldest: Gibt es mehrere Seiten
// mit dem Slug (aktive und gelöschte), erhält die älteste aktive das Kennzeichen.
func TestSeedSystemStatusPage_BackfillPrefersActiveOldest(t *testing.T) {
	db := newIntegrityTestDB(t) // bewusst ohne partiellen Index: mehrere Seiten möglich
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
