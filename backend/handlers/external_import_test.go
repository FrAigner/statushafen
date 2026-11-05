package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// externe Bausteine direkt übernehmen (Komponente anlegen + koppeln).

type importResp struct {
	Created []struct {
		ComponentID         uint   `json:"component_id"`
		Name                string `json:"name"`
		ExternalComponentID uint   `json:"external_component_id"`
		IsGroup             bool   `json:"is_group"`
	} `json:"created"`
	Skipped []struct {
		ExternalComponentID uint   `json:"external_component_id"`
		Reason              string `json:"reason"`
	} `json:"skipped"`
}

func (f *extFixture) importPath(p models.StatusPage, srcID uint) string {
	return f.sourcesPath(p) + "/" + uid(srcID) + "/import"
}

func (f *extFixture) doImport(srcID uint, tok string, body any) (*httptest.ResponseRecorder, importResp) {
	f.t.Helper()
	w := f.do(http.MethodPost, f.importPath(f.page, srcID), tok, body)
	var resp importResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

// extGroup legt eine externe Gruppe an; Mitglieder per extMember.
func (f *extFixture) extGroup(srcID uint, externalID, name string) models.ExternalComponent {
	f.t.Helper()
	g := models.ExternalComponent{ExternalSourceID: srcID, ExternalID: externalID, Name: name, IsGroup: true, Status: "operational", LastSeenAt: time.Now()}
	require.NoError(f.t, f.db.Create(&g).Error)
	return g
}

func (f *extFixture) extMember(srcID uint, g models.ExternalComponent, externalID, name string, pos int) models.ExternalComponent {
	f.t.Helper()
	m := models.ExternalComponent{ExternalSourceID: srcID, ExternalID: externalID, Name: name, Status: "operational", GroupExternalID: g.ExternalID, GroupName: g.Name, Position: pos, LastSeenAt: time.Now()}
	require.NoError(f.t, f.db.Create(&m).Error)
	return m
}

func ids(v ...models.ExternalComponent) []uint {
	out := make([]uint, 0, len(v))
	for _, e := range v {
		out = append(out, e.ID)
	}
	return out
}

func TestImport_SingleComponents_CreatesAndLinks(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	a := f.extComp(src, "a", "  Compute  ", "operational")
	long := f.extComp(src, "b", strings.Repeat("ä", 300), "operational")

	w, resp := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(a, long)})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Len(t, resp.Created, 2)
	assert.Empty(t, resp.Skipped)
	assert.Equal(t, "Compute", resp.Created[0].Name, "getrimmt")
	assert.Equal(t, 200, len([]rune(resp.Created[1].Name)), "auf das Namenslimit gekürzt")

	var comp models.Component
	require.NoError(t, f.db.First(&comp, resp.Created[0].ComponentID).Error)
	assert.Equal(t, "operational", comp.Status)
	assert.Equal(t, f.page.ID, comp.StatusPageID)
	assert.Nil(t, comp.ParentID)
	assert.Empty(t, comp.Description)
	var link models.ExternalComponentLink
	require.NoError(t, f.db.Where("component_id = ?", comp.ID).First(&link).Error)
	assert.Equal(t, a.ID, link.ExternalComponentID)
	assert.Equal(t, f.page.ID, link.StatusPageID)

	// SortOrder ans Ende der obersten Ebene, in Auswahlreihenfolge.
	var c2 models.Component
	require.NoError(t, f.db.First(&c2, resp.Created[1].ComponentID).Error)
	assert.Equal(t, comp.SortOrder+1, c2.SortOrder)
	assert.Contains(t, f.auditActions(), "external_link.imported")
	assert.Contains(t, f.auditActions(), "component.created")
}

func TestImport_EmptyNameFallsBackToExternalID(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	a := f.extComp(src, "kennung-1", "   ", "operational")
	w, resp := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(a)})
	require.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "kennung-1", resp.Created[0].Name)
}

func TestImport_IntoOwnGroup_WithTags(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	a := f.extComp(src, "a", "Compute", "operational")
	tag := models.Tag{Name: "eu", StatusPageID: &f.page.ID}
	require.NoError(t, f.db.Create(&tag).Error)

	w, resp := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(a), "parent_id": f.group.ID, "tag_ids": []uint{tag.ID}})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var comp models.Component
	require.NoError(t, f.db.First(&comp, resp.Created[0].ComponentID).Error)
	require.NotNil(t, comp.ParentID)
	assert.Equal(t, f.group.ID, *comp.ParentID)
	assert.Equal(t, 1, comp.SortOrder, "ans Ende der Gruppe (vorhandenes Kind hat SortOrder 0)")
	var ct models.ComponentTag
	require.NoError(t, f.db.Where("component_id = ?", comp.ID).First(&ct).Error)
	assert.Equal(t, tag.ID, ct.TagID)
}

func TestImport_Group_CreatesGroupWithMembers_GroupNotLinked(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	g := f.extGroup(src, "g1", "Region A")
	m1 := f.extMember(src, g, "m1", "Compute", 1)
	m2 := f.extMember(src, g, "m2", "Service 18", 2)
	gone := f.extMember(src, g, "m3", "Alt", 3)
	require.NoError(t, f.db.Model(&gone).Update("vanished", true).Error)

	w, resp := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(g)})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Len(t, resp.Created, 3, "Gruppe + 2 aktuelle Mitglieder")
	assert.True(t, resp.Created[0].IsGroup)
	assert.Equal(t, "Region A", resp.Created[0].Name)

	var gc models.Component
	require.NoError(t, f.db.First(&gc, resp.Created[0].ComponentID).Error)
	assert.Equal(t, "group", gc.Status)
	var kids []models.Component
	require.NoError(t, f.db.Where("parent_id = ?", gc.ID).Order("sort_order").Find(&kids).Error)
	require.Len(t, kids, 2)
	assert.Equal(t, "Compute", kids[0].Name)
	assert.Equal(t, "Service 18", kids[1].Name)
	var n int64
	f.db.Model(&models.ExternalComponentLink{}).Where("external_component_id IN ?", []uint{m1.ID, m2.ID}).Count(&n)
	assert.EqualValues(t, 2, n)
	f.db.Model(&models.ExternalComponentLink{}).Where("external_component_id = ?", g.ID).Count(&n)
	assert.EqualValues(t, 0, n, "die Gruppe selbst wird nicht gekoppelt")
}

func TestImport_Idempotent_DoubleClickSkips(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	a := f.extComp(src, "a", "Compute", "operational")
	g := f.extGroup(src, "g9", "Gruppe9")
	f.extMember(src, g, "m1", "M1", 1)
	body := map[string]any{"external_component_ids": ids(a, g)}

	w, _ := f.doImport(src, f.pageAdminTok, body)
	require.Equal(t, http.StatusCreated, w.Code)
	var before int64
	f.db.Model(&models.Component{}).Count(&before)

	w, resp := f.doImport(src, f.pageAdminTok, body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, resp.Created)
	require.Len(t, resp.Skipped, 2)
	for _, s := range resp.Skipped {
		assert.Equal(t, "already_linked", s.Reason)
	}
	var after int64
	f.db.Model(&models.Component{}).Count(&after)
	assert.Equal(t, before, after, "kein zweites Mal angelegt")
	assert.NotNil(t, resp.Created, "leer, aber nicht null")
	assert.Contains(t, w.Body.String(), `"created":[]`)
}

func TestImport_Skips_LinkedVanishedEmptyGroup_AndDedupesGroupMember(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	linked := f.extComp(src, "l", "Gekoppelt", "operational")
	require.NoError(t, f.db.Create(&models.ExternalComponentLink{StatusPageID: f.page.ID, ComponentID: f.comp.ID, ExternalComponentID: linked.ID}).Error)
	gone := f.extComp(src, "v", "Weg", "operational")
	require.NoError(t, f.db.Model(&gone).Update("vanished", true).Error)
	goneGroup := f.extGroup(src, "gv", "WegGruppe")
	require.NoError(t, f.db.Model(&goneGroup).Update("vanished", true).Error)
	empty := f.extGroup(src, "ge", "Leer")
	g := f.extGroup(src, "g1", "G")
	m := f.extMember(src, g, "m1", "Mitglied", 1)
	fresh := f.extComp(src, "n", "Neu", "operational")

	w, resp := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(linked, gone, goneGroup, empty, m, g, fresh)})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	reasons := map[uint]string{}
	for _, s := range resp.Skipped {
		reasons[s.ExternalComponentID] = s.Reason
	}
	assert.Equal(t, "already_linked", reasons[linked.ID])
	assert.Equal(t, "vanished", reasons[gone.ID])
	assert.Equal(t, "vanished", reasons[goneGroup.ID])
	assert.Equal(t, "empty_group", reasons[empty.ID])
	assert.Equal(t, "already_linked", reasons[m.ID], "Mitglied einer gewählten Gruppe nicht doppelt")
	// Gruppe G + Mitglied (1x) + Neu = 3.
	assert.Len(t, resp.Created, 3)
	var n int64
	f.db.Model(&models.ExternalComponentLink{}).Where("external_component_id = ?", m.ID).Count(&n)
	assert.EqualValues(t, 1, n)
}

func TestImport_GroupWithAllMembersLinked_SkippedWholly(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	g := f.extGroup(src, "g1", "G")
	m := f.extMember(src, g, "m1", "M", 1)
	require.NoError(t, f.db.Create(&models.ExternalComponentLink{StatusPageID: f.page.ID, ComponentID: f.comp.ID, ExternalComponentID: m.ID}).Error)
	var before int64
	f.db.Model(&models.Component{}).Count(&before)
	w, resp := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(g)})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Created)
	var after int64
	f.db.Model(&models.Component{}).Count(&after)
	assert.Equal(t, before, after, "keine leere Gruppe angelegt")
}

func TestImport_Errors(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	otherSrc := f.createSource(f.other, f.otherTok, "https://status.example.org")
	a := f.extComp(src, "a", "Compute", "operational")
	foreign := f.extComp(otherSrc, "a", "Fremd", "operational")
	tooMany := make([]uint, 201)
	for i := range tooMany {
		tooMany[i] = uint(i + 1)
	}
	foreignTag := models.Tag{Name: "x", StatusPageID: &f.other.ID}
	require.NoError(t, f.db.Create(&foreignTag).Error)

	cases := []struct {
		name string
		tok  string
		path string
		body any
		code int
	}{
		{"ohne Token", "", f.importPath(f.page, src), map[string]any{"external_component_ids": []uint{a.ID}}, http.StatusUnauthorized},
		{"Viewer ohne external_sources:update", f.viewerTok, f.importPath(f.page, src), map[string]any{"external_component_ids": []uint{a.ID}}, http.StatusForbidden},
		{"Admin einer anderen Seite", f.otherTok, f.importPath(f.page, src), map[string]any{"external_component_ids": []uint{a.ID}}, http.StatusForbidden},
		{"fremde Quelle (Mandant)", f.otherTok, f.importPath(f.other, src), map[string]any{"external_component_ids": []uint{a.ID}}, http.StatusNotFound},
		{"ungültige Quellen-ID", f.pageAdminTok, f.sourcesPath(f.page) + "/abc/import", map[string]any{"external_component_ids": []uint{a.ID}}, http.StatusBadRequest},
		{"kaputtes JSON", f.pageAdminTok, f.importPath(f.page, src), "{", http.StatusBadRequest},
		{"leere Auswahl", f.pageAdminTok, f.importPath(f.page, src), map[string]any{"external_component_ids": []uint{}}, http.StatusBadRequest},
		{"zu viele", f.pageAdminTok, f.importPath(f.page, src), map[string]any{"external_component_ids": tooMany}, http.StatusBadRequest},
		{"Ziel keine Gruppe", f.pageAdminTok, f.importPath(f.page, src), map[string]any{"external_component_ids": []uint{a.ID}, "parent_id": f.comp.ID}, http.StatusBadRequest},
		{"Ziel fremde Seite", f.pageAdminTok, f.importPath(f.page, src), map[string]any{"external_component_ids": []uint{a.ID}, "parent_id": f.otherComp.ID}, http.StatusBadRequest},
		{"Ziel unbekannt", f.pageAdminTok, f.importPath(f.page, src), map[string]any{"external_component_ids": []uint{a.ID}, "parent_id": 99999}, http.StatusBadRequest},
		{"fremder Tag", f.pageAdminTok, f.importPath(f.page, src), map[string]any{"external_component_ids": []uint{a.ID}, "tag_ids": []uint{foreignTag.ID}}, http.StatusBadRequest},
		{"externer Baustein einer fremden Quelle", f.pageAdminTok, f.importPath(f.page, src), map[string]any{"external_component_ids": []uint{foreign.ID}}, http.StatusNotFound},
		{"unbekannter Baustein", f.pageAdminTok, f.importPath(f.page, src), map[string]any{"external_component_ids": []uint{a.ID, 99999}}, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := f.do(http.MethodPost, tc.path, tc.tok, tc.body)
			assert.Equal(t, tc.code, w.Code, w.Body.String())
		})
	}
	var n int64
	f.db.Model(&models.ExternalComponentLink{}).Count(&n)
	assert.EqualValues(t, 0, n, "kein Fehlerfall hat etwas angelegt")
	f.db.Model(&models.Component{}).Where("name = ?", "Compute").Count(&n)
	assert.EqualValues(t, 0, n)
}

func TestImport_TagAccessForbidden(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	a := f.extComp(src, "a", "Compute", "operational")
	userTag := models.Tag{Name: "nur-ich"}
	resTag := models.Tag{Name: "geheim"}
	require.NoError(t, f.db.Create(&userTag).Error)
	require.NoError(t, f.db.Create(&resTag).Error)
	var u models.User
	require.NoError(t, f.db.Where("email = ?", "pa@example.com").First(&u).Error)
	require.NoError(t, f.db.Create(&models.UserTag{UserID: u.ID, TagID: userTag.ID}).Error)

	w, _ := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": []uint{a.ID}, "tag_ids": []uint{resTag.ID}})
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	// Auch mit passendem User-Tag darf eine Seiten-Rolle keinen
	// GLOBALEN Tag vergeben (nur Global Admins).
	w, _ = f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": []uint{a.ID}, "tag_ids": []uint{userTag.ID}})
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "global_tag_forbidden")
}

func TestImport_TransactionRollsBackOnLinkFailure(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	a := f.extComp(src, "a", "Eins", "operational")
	b := f.extComp(src, "b", "Zwei", "operational")
	var before int64
	f.db.Model(&models.Component{}).Count(&before)
	extFailOn(t, f.db, "create", "external_component_links")

	w, _ := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(a, b)})
	require.Equal(t, http.StatusInternalServerError, w.Code)
	var after int64
	f.db.Model(&models.Component{}).Count(&after)
	assert.Equal(t, before, after, "alles oder nichts: auch die angelegten Komponenten sind zurückgerollt")
	assert.NotContains(t, f.auditActions(), "external_link.imported")
}

func TestImport_DBErrorPaths(t *testing.T) {
	for _, table := range []string{"components", "component_tags"} {
		t.Run("create "+table, func(t *testing.T) {
			f := newExtFixture(t)
			src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
			a := f.extComp(src, "a", "Eins", "operational")
			tag := models.Tag{Name: "t", StatusPageID: &f.page.ID}
			require.NoError(t, f.db.Create(&tag).Error)
			extFailOn(t, f.db, "create", table)
			w, _ := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(a), "tag_ids": []uint{tag.ID}})
			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})
	}
	t.Run("Kopplungsliste nicht lesbar", func(t *testing.T) {
		f := newExtFixture(t)
		src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
		a := f.extComp(src, "a", "Eins", "operational")
		extFailOn(t, f.db, "query", "external_component_links")
		w, _ := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(a)})
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestImport_GroupFailsInMemberLink_RollsBackGroup(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	g := f.extGroup(src, "g", "G")
	f.extMember(src, g, "m1", "M1", 1)
	var before int64
	f.db.Model(&models.Component{}).Count(&before)
	extFailOn(t, f.db, "create", "external_component_links")
	w, _ := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(g)})
	require.Equal(t, http.StatusInternalServerError, w.Code)
	var after int64
	f.db.Model(&models.Component{}).Count(&after)
	assert.Equal(t, before, after)
}

func TestImport_ImportedComponentAppearsInTreeAndGroupStatusFollowsChildren(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	g := f.extGroup(src, "g", "G")
	f.extMember(src, g, "m1", "M1", 1)
	m2 := f.extMember(src, g, "m2", "M2", 2)
	require.NoError(t, f.db.Model(&m2).Update("status", "major_outage").Error)
	w, _ := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(g)})
	require.Equal(t, http.StatusCreated, w.Code)

	w = f.do(http.MethodGet, "/api/pages/"+pid(f.page)+"/components", f.pageAdminTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"major_outage"`, "Gruppenstatus aus den gekoppelten Kindern")
}

func TestExternalComponents_NeverFetchedSourceReturnsEmptyLists(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", src).Update("active", false).Error)
	w := f.do(http.MethodGet, f.sourcesPath(f.page)+"/"+uid(src)+"/components", f.pageAdminTok, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"groups":[],"components":[]}`, w.Body.String())
}

func TestImport_GroupDBErrors(t *testing.T) {
	t.Run("Gruppenkomponente nicht anlegbar", func(t *testing.T) {
		f := newExtFixture(t)
		src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
		g := f.extGroup(src, "g1", "G")
		f.extMember(src, g, "m1", "M1", 1)
		extFailOn(t, f.db, "create", "components")
		w, _ := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(g)})
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
	t.Run("Mitglieder nicht lesbar", func(t *testing.T) {
		f := newExtFixture(t)
		src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
		g := f.extGroup(src, "g1", "G")
		f.extMember(src, g, "m1", "M1", 1)
		// Die erste Abfrage (Auswahl laden) gelingt, die zweite (Mitglieder) scheitert.
		calls := 0
		name := "test:import:second-query"
		require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "external_components" {
				calls++
				if calls > 1 {
					_ = tx.AddError(errors.New("injizierter Fehler"))
				}
			}
		}))
		t.Cleanup(func() { _ = f.db.Callback().Query().Remove(name) })
		w, _ := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(g)})
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestImport_PartiallyLinkedGroup_ImportsRestAndReportsLinkedMember(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	g := f.extGroup(src, "g1", "G")
	m1 := f.extMember(src, g, "m1", "M1", 1)
	f.extMember(src, g, "m2", "M2", 2)
	require.NoError(t, f.db.Create(&models.ExternalComponentLink{StatusPageID: f.page.ID, ComponentID: f.comp.ID, ExternalComponentID: m1.ID}).Error)
	w, resp := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(g)})
	require.Equal(t, http.StatusCreated, w.Code)
	assert.Len(t, resp.Created, 2, "Gruppe + das noch freie Mitglied")
	require.Len(t, resp.Skipped, 1)
	assert.Equal(t, m1.ID, resp.Skipped[0].ExternalComponentID)
}
