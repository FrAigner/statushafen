package database

import (
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveOrphanedPageSSOIdentities: Verknüpfungen, deren
// Mitgliedschaft nach dem Verknüpfen weich entfernt wurde, werden beim Start
// gelöscht. Bewusste Verknüpfungen ohne Rolle (nie Mitglied oder nach dem
// Entfernen neu verknüpft), aktive Mitglieder und andere Seiten bleiben. Der
// zweite Lauf ändert nichts.
func TestRemoveOrphanedPageSSOIdentities(t *testing.T) {
	db := newIntegrityTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.UserSSOIdentity{}))
	pageA := models.StatusPage{Name: "A", Slug: "a"}
	pageB := models.StatusPage{Name: "B", Slug: "b"}
	role := models.Role{Name: "Viewer", Permissions: "pages:read"}
	require.NoError(t, db.Create(&pageA).Error)
	require.NoError(t, db.Create(&pageB).Error)
	require.NoError(t, db.Create(&role).Error)
	user := func(email string) models.User {
		u := models.User{Email: email}
		require.NoError(t, db.Create(&u).Error)
		return u
	}
	t0 := time.Now().Add(-48 * time.Hour)
	ident := func(u models.User, p models.StatusPage, created time.Time) {
		require.NoError(t, db.Create(&models.UserSSOIdentity{UserID: u.ID, StatusPageID: p.ID, Protocol: "oidc",
			Issuer: "iss", Subject: u.Email + p.Slug, CreatedAt: created}).Error)
	}
	membership := func(u models.User, p models.StatusPage, deleted *time.Time) {
		m := models.UserStatusPageRole{UserID: u.ID, StatusPageID: p.ID, RoleID: role.ID}
		require.NoError(t, db.Create(&m).Error)
		if deleted != nil {
			require.NoError(t, db.Unscoped().Model(&m).UpdateColumn("deleted_at", *deleted).Error)
		}
	}
	removedLater := t0.Add(time.Hour)

	orphan := user("orphan@x.example") // verknüpft, danach entfernt -> weg
	ident(orphan, pageA, t0)
	membership(orphan, pageA, &removedLater)
	ident(orphan, pageB, t0) // andere Seite ohne Beleg -> bleibt

	relinked := user("relinked@x.example") // entfernt, danach neu verknüpft -> bleibt
	membership(relinked, pageA, &removedLater)
	ident(relinked, pageA, removedLater.Add(time.Hour))

	noRole := user("norole@x.example") // nie Mitglied (Selbstverknüpfung) -> bleibt
	ident(noRole, pageA, t0)

	active := user("active@x.example") // alte entfernte Zeile, aber wieder Mitglied -> bleibt
	ident(active, pageA, t0)
	membership(active, pageA, &removedLater)
	membership(active, pageA, nil)

	removeOrphanedPageSSOIdentities(db)
	removeOrphanedPageSSOIdentities(db)

	count := func(u models.User, p models.StatusPage) int64 {
		var n int64
		require.NoError(t, db.Model(&models.UserSSOIdentity{}).Where("user_id = ? AND status_page_id = ?", u.ID, p.ID).Count(&n).Error)
		return n
	}
	assert.EqualValues(t, 0, count(orphan, pageA), "verwaiste Verknüpfung muss weg")
	assert.EqualValues(t, 1, count(orphan, pageB), "andere Seite bleibt")
	assert.EqualValues(t, 1, count(relinked, pageA), "nach dem Entfernen neu verknüpft bleibt")
	assert.EqualValues(t, 1, count(noRole, pageA), "bewusste Verknüpfung ohne Rolle bleibt")
	assert.EqualValues(t, 1, count(active, pageA), "aktives Mitglied bleibt")

	var markers int64
	require.NoError(t, db.Unscoped().Model(&models.UserStatusPageRole{}).Where("deleted_at IS NOT NULL").Count(&markers).Error)
	assert.EqualValues(t, 3, markers, "soft-gelöschte Mitgliedszeilen (Merker) bleiben unverändert")
}

// TestRemoveOrphanedPageSSOIdentities_ErrorIsNotFatal: Fehlt die Tabelle,
// bricht die Bereinigung nur ab (Warnung), der Start läuft weiter.
func TestRemoveOrphanedPageSSOIdentities_ErrorIsNotFatal(t *testing.T) {
	db := newIntegrityTestDB(t)
	assert.NotPanics(t, func() { removeOrphanedPageSSOIdentities(db) })
}
