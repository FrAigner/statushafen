package handlers

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// cascadeAccess prüft die Tag-Rechte für alle Komponenten einer Kaskade:
// Wer mindestens eine Komponente mit fremdem Tag trifft, darf die Gruppe nicht
// archivieren; Lesefehler enden als Fehler und nie als Freigabe (fail closed).
func TestCascadeAccess_TagRulesAndDBErrors(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "P", Slug: "p-cascade"}
	require.NoError(t, db.Create(&page).Error)
	mkTag := func(name string) models.Tag {
		tg := models.Tag{Name: name, StatusPageID: &page.ID}
		require.NoError(t, db.Create(&tg).Error)
		return tg
	}
	mine, foreign := mkTag("mein-tag"), mkTag("fremder-tag")
	mkComp := func(name string, tags ...models.Tag) models.Component {
		comp := models.Component{StatusPageID: page.ID, Name: name, Status: "operational"}
		require.NoError(t, db.Create(&comp).Error)
		for _, tg := range tags {
			require.NoError(t, db.Create(&models.ComponentTag{ComponentID: comp.ID, TagID: tg.ID}).Error)
		}
		return comp
	}
	open, ownTagged, foreignTagged := mkComp("offen"), mkComp("eigen", mine), mkComp("fremd", foreign)
	user := models.User{Email: "cascade@example.com"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&models.UserTag{UserID: user.ID, TagID: mine.ID}).Error)
	newAccess := func() func(db2 *gorm.DB, ids []uint) (bool, error) {
		c, _ := pageTestContext(http.MethodPost, "", user.ID)
		return h.cascadeAccess(c)
	}

	allowed, err := newAccess()(db, []uint{open.ID, ownTagged.ID})
	require.NoError(t, err)
	assert.True(t, allowed, "eigener Tag und Komponente ohne Tag")

	allowed, err = newAccess()(db, []uint{ownTagged.ID, foreignTagged.ID})
	require.NoError(t, err)
	assert.False(t, allowed, "eine Komponente mit fremdem Tag sperrt die Kaskade")

	for _, table := range []string{"component_tags", "tags"} {
		t.Run("Lesefehler "+table, func(t *testing.T) {
			access := newAccess()
			remove := injectDBError(t, db, "query", table, 0)
			defer remove()
			allowed, err := access(db, []uint{ownTagged.ID})
			require.Error(t, err)
			assert.False(t, allowed)
		})
	}
}

// Ohne gesetzte Uhr (Produktion) archiviert der Handler mit der Systemzeit.
func TestArchiveComponentHandler_UsesSystemClockByDefault(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	require.Nil(t, h.Now)
	page := models.StatusPage{Name: "P", Slug: "p-archive-clock"}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	admin := models.User{Email: "archiv-admin@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)

	c, w := pageTestContext(http.MethodPost, "", admin.ID)
	c.Params = gin.Params{{Key: "page_id", Value: strconv.Itoa(int(page.ID))}, {Key: "id", Value: strconv.Itoa(int(comp.ID))}}
	h.ArchiveComponentHandler(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var reloaded models.Component
	require.NoError(t, db.First(&reloaded, comp.ID).Error)
	require.NotNil(t, reloaded.ArchivedAt)
	assert.WithinDuration(t, time.Now(), *reloaded.ArchivedAt, 5*time.Second)
}
