package models_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// BeforeCreate vergibt eine Kennung, wenn keine gesetzt ist, und
// lässt eine gesetzte unangetastet.
func TestComponentBeforeCreate_VergibtKennung(t *testing.T) {
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "P", Slug: "p-577"}
	require.NoError(t, db.Create(&page).Error)

	a := models.Component{StatusPageID: page.ID, Name: "Datenbank Köln"}
	b := models.Component{StatusPageID: page.ID, Name: "Datenbank Köln"}
	own := models.Component{StatusPageID: page.ID, Name: "Egal", Slug: "eigen"}
	for _, c := range []*models.Component{&a, &b, &own} {
		require.NoError(t, db.Create(c).Error)
	}
	assert.Equal(t, "datenbank-koeln", a.Slug)
	assert.Equal(t, "datenbank-koeln-2", b.Slug)
	assert.Equal(t, "eigen", own.Slug)

	// Gelöschte Zeilen blockieren die Kennung nicht (wie der partielle Index).
	require.NoError(t, db.Delete(&a).Error)
	c := models.Component{StatusPageID: page.ID, Name: "Datenbank Köln"}
	require.NoError(t, db.Create(&c).Error)
	assert.Equal(t, "datenbank-koeln", c.Slug)
}

func TestComponentBeforeCreate_DatenbankfehlerBrichtAnlegenAb(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, db.Migrator().DropTable(&models.Component{}))
	err := db.Create(&models.Component{StatusPageID: 1, Name: "X"}).Error
	assert.Error(t, err)
}
