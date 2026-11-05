package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Funktionsverzeichnis auf der Systemseite.

func capabilityComponentCount() int64 {
	n := int64(0)
	for _, g := range SystemCapabilities {
		n += 1 + int64(len(g.Items))
	}
	return n
}

func seededSystemPage(t *testing.T) (*gorm.DB, models.StatusPage) {
	t.Helper()
	db := newIntegrityTestDB(t)
	CreateSoftDeleteUniqueIndexes(db)
	SeedSystemStatusPage(db)
	var page models.StatusPage
	require.NoError(t, db.Where("is_system = ?", true).First(&page).Error)
	return db, page
}

func componentBySlug(t *testing.T, db *gorm.DB, slug string) models.Component {
	t.Helper()
	var c models.Component
	require.NoError(t, db.Unscoped().Where("slug = ?", slug).First(&c).Error, slug)
	return c
}

// Neue Funktionen müssen im Verzeichnis stehen: jede Hilfeseite (außer den
// Übersichtsseiten) gehört zu genau einer Funktion und umgekehrt.
func TestSystemCapabilities_CoverEveryDocsPage(t *testing.T) {
	files, err := filepath.Glob("../../frontend/src/docs/de/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, files, "Hilfeseiten nicht gefunden")

	registered := map[string]bool{}
	for _, g := range SystemCapabilities {
		for _, it := range g.Items {
			assert.False(t, registered[it.Doc], "doppelter Eintrag %s", it.Doc)
			registered[it.Doc] = true
		}
	}
	for _, f := range files {
		doc := strings.TrimSuffix(filepath.Base(f), ".md")
		if DocsWithoutCapability[doc] {
			continue
		}
		assert.True(t, registered[doc], "Hilfeseite %q hat keine Funktion in SystemCapabilities (neue Funktion = neuer Eintrag)", doc)
	}
	for doc := range registered {
		if strings.HasPrefix(doc, "plattform-") {
			continue
		}
		_, err := os.Stat("../../frontend/src/docs/de/" + doc + ".md")
		assert.NoError(t, err, "Eintrag %q hat keine Hilfeseite", doc)
	}
}

func TestSystemCapabilities_SeedCreatesGroupsAndAdoptsPlatformComponents(t *testing.T) {
	db, page := seededSystemPage(t)

	var total int64
	require.NoError(t, db.Model(&models.Component{}).Where("status_page_id = ?", page.ID).Count(&total).Error)
	assert.Equal(t, capabilityComponentCount(), total, "die vier Altkomponenten werden übernommen, nicht verdoppelt")

	group := componentBySlug(t, db, "gruppe-plattform")
	assert.Equal(t, "group", group.Status)
	api := componentBySlug(t, db, "feature-plattform-api")
	require.NotNil(t, api.ParentID)
	assert.Equal(t, group.ID, *api.ParentID)
	assert.Equal(t, "API", api.Name)
	vorfaelle := componentBySlug(t, db, "feature-vorfaelle")
	assert.Equal(t, "operational", vorfaelle.Status)
}

func TestSystemCapabilities_SyncIsIdempotent(t *testing.T) {
	db, page := seededSystemPage(t)
	SyncSystemCapabilities(db, page.ID)
	SeedSystemStatusPage(db)
	var total int64
	require.NoError(t, db.Unscoped().Model(&models.Component{}).Count(&total).Error)
	assert.Equal(t, capabilityComponentCount(), total)
}

// Admin-Entscheidungen überleben den Neustart: umbenannt bleibt umbenannt,
// gelöscht bleibt gelöscht, ein manuell gesetzter Status bleibt.
func TestSystemCapabilities_RespectsAdminChanges(t *testing.T) {
	db, page := seededSystemPage(t)
	require.NoError(t, db.Model(&models.Component{}).Where("slug = ?", "feature-vorfaelle").Updates(map[string]any{"name": "Störungen", "status": "degraded_performance"}).Error)
	require.NoError(t, db.Where("slug = ?", "feature-audit-log").Delete(&models.Component{}).Error)

	SyncSystemCapabilities(db, page.ID)

	v := componentBySlug(t, db, "feature-vorfaelle")
	assert.Equal(t, "Störungen", v.Name)
	assert.Equal(t, "degraded_performance", v.Status)
	var live int64
	require.NoError(t, db.Model(&models.Component{}).Where("slug = ?", "feature-audit-log").Count(&live).Error)
	assert.Zero(t, live, "gelöschte Funktion darf nicht zurückkommen")
}

func TestSystemCapabilities_DeletedGroupKeepsItsItemsGone(t *testing.T) {
	db, page := seededSystemPage(t)
	require.NoError(t, db.Where("slug = ?", "feature-sso").Delete(&models.Component{}).Error)
	require.NoError(t, db.Where("slug = ?", "gruppe-zugang").Delete(&models.Component{}).Error)

	SyncSystemCapabilities(db, page.ID)

	var live int64
	require.NoError(t, db.Model(&models.Component{}).Where("slug IN ?", []string{"gruppe-zugang", "feature-sso"}).Count(&live).Error)
	assert.Zero(t, live)
}

// Eine später hinzugefügte Funktion erscheint beim nächsten Start in ihrer Gruppe.
func TestSystemCapabilities_NewCapabilityIsAddedLater(t *testing.T) {
	db, page := seededSystemPage(t)
	orig := SystemCapabilities
	t.Cleanup(func() { SystemCapabilities = orig })
	extended := append([]CapabilityGroup(nil), orig...)
	extended[2] = CapabilityGroup{Key: orig[2].Key, Name: orig[2].Name, Items: append(append([]Capability(nil), orig[2].Items...), Capability{Doc: "neu", Name: "Neue Funktion"})}
	SystemCapabilities = extended

	SyncSystemCapabilities(db, page.ID)

	neu := componentBySlug(t, db, "feature-neu")
	group := componentBySlug(t, db, "gruppe-meldungen")
	require.NotNil(t, neu.ParentID)
	assert.Equal(t, group.ID, *neu.ParentID)
}

// Eine bereits übernommene Plattform-Komponente, die der Admin wieder nach oben zieht, bleibt dort.
func TestSystemCapabilities_AdoptedComponentStaysWhereAdminPutsIt(t *testing.T) {
	db, page := seededSystemPage(t)
	require.NoError(t, db.Model(&models.Component{}).Where("slug = ?", "feature-plattform-api").Update("parent_id", nil).Error)

	SyncSystemCapabilities(db, page.ID)

	assert.Nil(t, componentBySlug(t, db, "feature-plattform-api").ParentID)
}

// Schlägt das Anlegen fehl (hier: Tabelle fehlt), liefert ensureComponent ok=false
// und stürzt nicht ab; der Start läuft weiter.
func TestEnsureComponent_CreateErrorReturnsNotOK(t *testing.T) {
	db, page := seededSystemPage(t)
	if err := db.Migrator().DropTable(&models.Component{}); err != nil {
		t.Fatal(err)
	}

	_, ok := ensureComponent(db, page.ID, "feature-neu", models.Component{Name: "Neu", Status: "operational"})

	assert.False(t, ok)
	assert.NotPanics(t, func() { SyncSystemCapabilities(db, page.ID) })
}
