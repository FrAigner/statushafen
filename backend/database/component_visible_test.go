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

// Bestandskomponenten stammen aus einem Schema ohne Spalte
// `visible`. Nach der Migration müssen sie sichtbar sein (Default true), sonst
// verschwände nach dem Deploy die ganze öffentliche Statusseite. Ein zweiter
// Lauf ändert nichts, auch nicht an bewusst ausgeblendeten Zeilen.
func TestAutoMigrate_ComponentVisibleDefaultTrue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE components (id integer PRIMARY KEY, created_at datetime, updated_at datetime, deleted_at datetime, status_page_id integer NOT NULL, parent_id integer, name text NOT NULL, description text, status text NOT NULL DEFAULT 'operational', sort_order integer DEFAULT 0)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO components (status_page_id, name) VALUES (1, 'Alt A'), (1, 'Alt B')`).Error)

	require.NoError(t, db.AutoMigrate(&models.Component{}))

	var comps []models.Component
	require.NoError(t, db.Order("id").Find(&comps).Error)
	require.Len(t, comps, 2)
	for _, c := range comps {
		assert.True(t, c.Visible, "Bestand %q muss nach der Migration sichtbar sein", c.Name)
	}

	// Idempotent: eine bewusst ausgeblendete Zeile überlebt den nächsten Lauf.
	require.NoError(t, db.Model(&models.Component{}).Where("id = ?", comps[0].ID).Update("visible", false).Error)
	require.NoError(t, db.AutoMigrate(&models.Component{}))
	var hidden models.Component
	require.NoError(t, db.First(&hidden, comps[0].ID).Error)
	assert.False(t, hidden.Visible)
}
