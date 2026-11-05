package handlers_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Auch die Übernahme externer Bausteine vergibt Kennungen
// (Umlaute transliteriert, Kollision zählt hoch, höchstens 60 Zeichen).
func TestImport_VergibtKennungen(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	a := f.extComp(src, "k1", "Größe Dienst", "operational")
	b := f.extComp(src, "k2", "Größe Dienst", "operational")
	long := f.extComp(src, "k3", strings.Repeat("ä", 100), "operational")

	w, resp := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(a, b, long)})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Len(t, resp.Created, 3)

	slugOf := func(i int) string {
		var comp models.Component
		require.NoError(t, f.db.First(&comp, resp.Created[i].ComponentID).Error)
		return comp.Slug
	}
	assert.Equal(t, "groesse-dienst", slugOf(0))
	assert.Equal(t, "groesse-dienst-2", slugOf(1))
	assert.True(t, models.ValidComponentSlug(slugOf(2)), slugOf(2))
}
