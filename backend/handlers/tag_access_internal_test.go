package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func tagTestContext(userID uint) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if userID != 0 {
		c.Set("userID", userID)
	}
	return c
}

func TestTagIDList(t *testing.T) {
	a, b := models.Tag{}, models.Tag{}
	a.ID, b.ID = 3, 7
	assert.Equal(t, []uint{3, 7}, tagIDList([]models.Tag{a, b}))
	assert.Empty(t, tagIDList(nil))
}

// Ein unbekannter Nutzer (z. B. nach dem Löschen) erhält weder Adminrechte
// noch Tags; die Prüfung schlägt zu, statt durchzulassen.
func TestUserTagsAndAdmin_UnknownUserHasNothing(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)

	tags, admin := h.userTagsAndAdmin(tagTestContext(0))
	assert.Empty(t, tags)
	assert.False(t, admin)

	tags, admin = h.userTagsAndAdmin(tagTestContext(9999))
	assert.Empty(t, tags)
	assert.False(t, admin)
}

// Datenbankfehler beim Auflösen von Tag-IDs werden als Fehler gemeldet und nicht
// als "Tag nicht gefunden" (das wäre eine falsche Aussage über die Eingabe).
func TestResolveTags_DBErrorIsReturned(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	injectDBError(t, db, "query", "tags", 0)

	_, err := h.resolveTagsByIDs([]uint{1}, 1)
	require.Error(t, err)
	assert.Equal(t, "", TemplateErrorCode(err), "kein Fachfehler-Code")

	_, err = h.resolveGlobalTagsByIDs([]uint{1})
	require.Error(t, err)
	assert.Equal(t, "", TemplateErrorCode(err))
}

// Die replace-Funktionen brechen beim ersten Fehler ab und melden ihn, damit die
// umgebende Transaktion zurückgerollt wird.
func TestReplaceTags_ErrorsAreReturned(t *testing.T) {
	tag := models.Tag{Name: "eu"}
	require.NoError(t, testutils.SetupTestDB().Create(&tag).Error)

	type replaceFn func(h *Handler, tx *gorm.DB, id uint, tags []models.Tag) error
	cases := []struct {
		name  string
		table string
		fn    replaceFn
	}{
		{"Komponente", "component_tags", func(h *Handler, tx *gorm.DB, id uint, tags []models.Tag) error {
			return h.replaceComponentTags(tx, id, tags)
		}},
		{"Vorfall", "incident_tags", func(h *Handler, tx *gorm.DB, id uint, tags []models.Tag) error {
			return h.replaceIncidentTags(tx, id, tags)
		}},
		{"Nutzer", "user_tags", func(h *Handler, tx *gorm.DB, id uint, tags []models.Tag) error {
			return h.replaceUserTags(tx, id, tags)
		}},
	}
	for _, tc := range cases {
		for _, op := range []string{"delete", "create"} {
			t.Run(tc.name+" "+op, func(t *testing.T) {
				db := testutils.SetupTestDB()
				h := NewHandler(db)
				injectDBError(t, db, op, tc.table, 0)
				err := tc.fn(h, db, 1, []models.Tag{tag})
				require.Error(t, err)
			})
		}
		t.Run(tc.name+" ohne Fehler", func(t *testing.T) {
			db := testutils.SetupTestDB()
			h := NewHandler(db)
			require.NoError(t, tc.fn(h, db, 1, []models.Tag{tag}))
			require.NoError(t, tc.fn(h, db, 1, nil), "leere Menge entfernt nur")
		})
	}
}

// incidentTagFilterScope: Nutzer mit Seiten-Tag sehen Vorfälle ohne Tag und
// Vorfälle mit passendem oder globalem Tag, aber keine Vorfälle mit fremdem Tag.
// Nutzer mit globalem Nutzer-Tag und Nutzer ohne Tags sehen alles.
func TestIncidentTagFilterScope(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "P", Slug: "p-tag-scope"}
	require.NoError(t, db.Create(&page).Error)

	mine := models.Tag{Name: "eu", StatusPageID: &page.ID}
	other := models.Tag{Name: "us", StatusPageID: &page.ID}
	global := models.Tag{Name: "Global"}
	for _, tg := range []*models.Tag{&mine, &other, &global} {
		require.NoError(t, db.Create(tg).Error)
	}
	mkIncident := func(title string, tags ...models.Tag) models.Incident {
		inc := models.Incident{Title: title, StatusPageID: page.ID}
		require.NoError(t, db.Create(&inc).Error)
		for _, tg := range tags {
			require.NoError(t, db.Create(&models.IncidentTag{IncidentID: inc.ID, TagID: tg.ID}).Error)
		}
		return inc
	}
	mkIncident("ohne Tag")
	mkIncident("eigener Tag", mine)
	mkIncident("fremder Tag", other)
	mkIncident("globaler Tag", global)

	mkUser := func(email string, tags ...models.Tag) uint {
		u := models.User{Email: email}
		require.NoError(t, db.Create(&u).Error)
		for _, tg := range tags {
			require.NoError(t, db.Create(&models.UserTag{UserID: u.ID, TagID: tg.ID}).Error)
		}
		return u.ID
	}
	titles := func(userID uint) []string {
		var got []models.Incident
		scope := h.incidentTagFilterScope(tagTestContext(userID))
		require.NoError(t, db.Model(&models.Incident{}).Scopes(scope).Order("id").Find(&got).Error)
		out := make([]string, len(got))
		for i, inc := range got {
			out[i] = inc.Title
		}
		return out
	}

	assert.Equal(t, []string{"ohne Tag", "eigener Tag", "globaler Tag"}, titles(mkUser("eu@example.com", mine)))
	assert.Len(t, titles(mkUser("global@example.com", global)), 4, "globaler Nutzer-Tag: keine Einschränkung")
	assert.Len(t, titles(mkUser("none@example.com")), 4, "ohne Tags: keine Einschränkung")
}
