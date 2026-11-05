package database

import (
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Backfill vergibt Kennungen deterministisch nach ID, löst
// Kollisionen auf, schützt vorhandene Kennungen, ignoriert gelöschte Zeilen
// und ist beim zweiten Lauf ein No-Op. Danach lässt sich der Index anlegen.
func TestBackfillComponentSlugs(t *testing.T) {
	db := setupTagMigrationTestDB(t)
	p1 := models.StatusPage{Name: "P1", Slug: "p1-577"}
	p2 := models.StatusPage{Name: "P2", Slug: "p2-577"}
	require.NoError(t, db.Create(&p1).Error)
	require.NoError(t, db.Create(&p2).Error)

	insert := func(pageID uint, name, slug string) uint {
		require.NoError(t, db.Exec(`INSERT INTO components (created_at, updated_at, status_page_id, name, slug, status, visible) VALUES (datetime('now'), datetime('now'), ?, ?, ?, 'operational', 1)`, pageID, name, slug).Error)
		var id uint
		require.NoError(t, db.Raw(`SELECT MAX(id) FROM components`).Scan(&id).Error)
		return id
	}
	a := insert(p1.ID, "Mail Server", "")
	vergeben := insert(p1.ID, "Irgendwas", "mail-server-2") // belegt die Kollisionsvariante
	b := insert(p1.ID, "Mail Server", "")
	c := insert(p1.ID, "Mail Server", "")
	d := insert(p2.ID, "Mail Server", "")
	gelöscht := insert(p1.ID, "Alt", "")
	require.NoError(t, db.Exec(`UPDATE components SET deleted_at = datetime('now') WHERE id = ?`, gelöscht).Error)

	slug := func(id uint) string {
		var s string
		require.NoError(t, db.Raw(`SELECT slug FROM components WHERE id = ?`, id).Scan(&s).Error)
		return s
	}

	BackfillComponentSlugs(db)
	assert.Equal(t, "mail-server", slug(a))
	assert.Equal(t, "mail-server-2", slug(vergeben), "vorhandene Kennung bleibt")
	assert.Equal(t, "mail-server-3", slug(b), "Kollision zählt hoch, ID-Reihenfolge")
	assert.Equal(t, "mail-server-4", slug(c))
	assert.Equal(t, "mail-server", slug(d), "andere Statusseite hat ihren eigenen Raum")
	assert.Equal(t, "", slug(gelöscht), "gelöschte Zeilen bleiben unberührt")

	// Zweiter Lauf: nichts ändert sich.
	var vorher []string
	require.NoError(t, db.Raw(`SELECT slug FROM components ORDER BY id`).Scan(&vorher).Error)
	BackfillComponentSlugs(db)
	var nachher []string
	require.NoError(t, db.Raw(`SELECT slug FROM components ORDER BY id`).Scan(&nachher).Error)
	assert.Equal(t, vorher, nachher)

	// Index lässt sich danach anlegen und erzwingt Eindeutigkeit.
	CreateSoftDeleteUniqueIndexes(db)
	assert.Contains(t, indexNames(t, db, "components"), "idx_components_slug_active")
	require.Error(t, db.Exec(`INSERT INTO components (created_at, updated_at, status_page_id, name, slug, status, visible) VALUES (datetime('now'), datetime('now'), ?, 'X', 'mail-server', 'operational', 1)`, p1.ID).Error)
}

func TestBackfillComponentSlugs_FehlerBrichtNichtAb(t *testing.T) {
	db := setupTagMigrationTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.Component{}))
	assert.NotPanics(t, func() { BackfillComponentSlugs(db) })
}

// Scheitert das Schreiben einer Zeile, geht der Backfill mit den übrigen weiter
// (nur Warnung, kein Abbruch); die Zeile wird beim nächsten Start erneut versucht.
func TestBackfillComponentSlugs_EinzelfehlerUeberspringtZeile(t *testing.T) {
	db := setupTagMigrationTestDB(t)
	p := models.StatusPage{Name: "P", Slug: "p-577-fehler"}
	require.NoError(t, db.Create(&p).Error)
	for _, n := range []string{"Erste", "Zweite"} {
		require.NoError(t, db.Exec(`INSERT INTO components (created_at, updated_at, status_page_id, name, slug, status, visible) VALUES (datetime('now'), datetime('now'), ?, ?, '', 'operational', 1)`, p.ID, n).Error)
	}
	require.NoError(t, db.Exec(`CREATE TRIGGER sperre BEFORE UPDATE ON components WHEN NEW.name = 'Erste' BEGIN SELECT RAISE(ABORT, 'gesperrt'); END`).Error)

	BackfillComponentSlugs(db)

	var slugs []string
	require.NoError(t, db.Raw(`SELECT slug FROM components ORDER BY id`).Scan(&slugs).Error)
	assert.Equal(t, []string{"", "zweite"}, slugs)

	require.NoError(t, db.Exec(`DROP TRIGGER sperre`).Error)
	BackfillComponentSlugs(db)
	require.NoError(t, db.Raw(`SELECT slug FROM components ORDER BY id`).Scan(&slugs).Error)
	assert.Equal(t, []string{"erste", "zweite"}, slugs, "nächster Start holt es nach")
}
