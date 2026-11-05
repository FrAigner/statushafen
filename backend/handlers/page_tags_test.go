package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Seiten-Rollen brauchen einen Lese-Endpunkt für Tags
// (GET /api/pages/:page_id/tags) und dürfen nur seiteneigene Tags vergeben.
type pageTagsFixture struct {
	db                                           *gorm.DB
	router                                       http.Handler
	page, other                                  models.StatusPage
	global, own, foreign                         models.Tag
	editorToken, adminToken, readerToken, noRole string
}

func newPageTagsFixture(t *testing.T) *pageTagsFixture {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.GET("/pages/:page_id/tags", middleware.RequirePermission("components:read"), h.GetPageTagsHandler)
	comp := authGroup.Group("/pages/:page_id/components")
	comp.Use(middleware.RequirePermission("components:write"))
	comp.POST("", h.CreateComponentHandler)
	comp.PUT("/:id", h.UpdateComponentHandler)
	inc := authGroup.Group("/pages/:page_id/incidents")
	inc.Use(middleware.RequirePermission("incidents:write"))
	inc.POST("", h.CreateIncidentHandler)
	inc.PUT("/:id", h.UpdateIncidentHandler)

	f := &pageTagsFixture{router: r, db: db}
	f.page = models.StatusPage{Name: "A", Slug: "pt-a"}
	f.other = models.StatusPage{Name: "B", Slug: "pt-b"}
	require.NoError(t, db.Create(&f.page).Error)
	require.NoError(t, db.Create(&f.other).Error)
	f.global = models.Tag{Name: "global"}
	f.own = models.Tag{Name: "own", StatusPageID: &f.page.ID}
	f.foreign = models.Tag{Name: "foreign", StatusPageID: &f.other.ID}
	for _, tg := range []*models.Tag{&f.global, &f.own, &f.foreign} {
		require.NoError(t, db.Create(tg).Error)
	}
	editorRole := models.Role{Name: "Ed", Permissions: "components:write,incidents:write"}
	readerRole := models.Role{Name: "Rd", Permissions: "components:read"}
	require.NoError(t, db.Create(&editorRole).Error)
	require.NoError(t, db.Create(&readerRole).Error)
	mk := func(email string, global bool, role *models.Role) string {
		u := models.User{Email: email, IsGlobalAdmin: global}
		require.NoError(t, db.Create(&u).Error)
		if role != nil {
			require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: f.page.ID, RoleID: role.ID}).Error)
		}
		tok, _ := middleware.GenerateToken(u.ID, u.Email, "", "", global)
		return tok
	}
	f.editorToken = mk("ed@example.com", false, &editorRole)
	f.readerToken = mk("rd@example.com", false, &readerRole)
	f.adminToken = mk("ga@example.com", true, nil)
	f.noRole = mk("nr@example.com", false, nil)
	return f
}

func (f *pageTagsFixture) do(method, path, token string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func pageIDStr(p models.StatusPage) string { return strconv.Itoa(int(p.ID)) }

func TestGetPageTagsHandler(t *testing.T) {
	f := newPageTagsFixture(t)
	path := "/api/pages/" + pageIDStr(f.page) + "/tags"

	t.Run("Seiten-Rolle sieht globale und seiteneigene Tags, nie fremde", func(t *testing.T) {
		w := f.do("GET", path, f.readerToken, nil)
		require.Equal(t, http.StatusOK, w.Code)
		var tags []models.Tag
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tags))
		names := []string{}
		for _, tg := range tags {
			names = append(names, tg.Name)
		}
		assert.ElementsMatch(t, []string{"global", "own"}, names)
	})
	t.Run("Global Admin bekommt dieselbe Mandanten-Sicht", func(t *testing.T) {
		w := f.do("GET", path, f.adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotContains(t, w.Body.String(), "foreign")
	})
	t.Run("ohne Rolle auf der Seite -> 403", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, f.do("GET", path, f.noRole, nil).Code)
	})
	t.Run("Rolle nur auf Seite A, Abruf Seite B -> 403", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, f.do("GET", "/api/pages/"+pageIDStr(f.other)+"/tags", f.readerToken, nil).Code)
	})
	t.Run("unauthentifiziert -> 401", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, f.do("GET", path, "", nil).Code)
	})
	t.Run("unbekannte Seite (Global Admin) -> 404", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, f.do("GET", "/api/pages/999999/tags", f.adminToken, nil).Code)
	})
	t.Run("ungültige page_id (Global Admin) -> 400", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, f.do("GET", "/api/pages/abc/tags", f.adminToken, nil).Code)
	})
}

func TestSeitenRollenDuerfenKeineGlobalenTagsVergeben(t *testing.T) {
	f := newPageTagsFixture(t)
	compPath := "/api/pages/" + pageIDStr(f.page) + "/components"
	incPath := "/api/pages/" + pageIDStr(f.page) + "/incidents"
	type obj = map[string]any

	assertGlobalForbidden := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "global_tag_forbidden")
	}

	t.Run("Komponente anlegen: globaler Tag -> 403 mit Code", func(t *testing.T) {
		assertGlobalForbidden(t, f.do("POST", compPath, f.editorToken, obj{"name": "X", "tag_ids": []uint{f.global.ID}}))
	})
	t.Run("Komponente anlegen: seiteneigener Tag -> 201", func(t *testing.T) {
		assert.Equal(t, http.StatusCreated, f.do("POST", compPath, f.editorToken, obj{"name": "Y", "tag_ids": []uint{f.own.ID}}).Code)
	})
	t.Run("Komponente anlegen: fremder Tag -> 400", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, f.do("POST", compPath, f.editorToken, obj{"name": "Z", "tag_ids": []uint{f.foreign.ID}}).Code)
	})
	t.Run("bereits getragener globaler Tag darf beim Bearbeiten bleiben", func(t *testing.T) {
		w := f.do("POST", compPath, f.adminToken, obj{"name": "G", "tag_ids": []uint{f.global.ID}})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		var created models.Component
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		upd := f.do("PUT", compPath+"/"+strconv.Itoa(int(created.ID)), f.editorToken,
			obj{"name": "G2", "status": "operational", "tag_ids": []uint{f.global.ID, f.own.ID}})
		assert.Equal(t, http.StatusOK, upd.Code, upd.Body.String())
	})
	t.Run("Komponente ändern: neuen globalen Tag hinzufügen -> 403", func(t *testing.T) {
		w := f.do("POST", compPath, f.editorToken, obj{"name": "U", "tag_ids": []uint{f.own.ID}})
		require.Equal(t, http.StatusCreated, w.Code)
		var created models.Component
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		assertGlobalForbidden(t, f.do("PUT", compPath+"/"+strconv.Itoa(int(created.ID)), f.editorToken,
			obj{"name": "U", "status": "operational", "tag_ids": []uint{f.own.ID, f.global.ID}}))
	})
	t.Run("Vorfall: globaler Tag -> 403, seiteneigener -> 201, Global Admin darf", func(t *testing.T) {
		w := f.do("POST", compPath, f.adminToken, obj{"name": "C"})
		require.Equal(t, http.StatusCreated, w.Code)
		var comp models.Component
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &comp))
		body := func(tag uint) obj {
			return obj{"title": "T", "message": "m", "status": "investigating", "ComponentIDs": []uint{comp.ID}, "TagIDs": []uint{tag}}
		}
		assertGlobalForbidden(t, f.do("POST", incPath, f.editorToken, body(f.global.ID)))
		w = f.do("POST", incPath, f.editorToken, body(f.own.ID))
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		var inc models.Incident
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &inc))
		upd := obj{"title": "T", "message": "m", "status": "investigating", "TagIDs": []uint{f.own.ID, f.global.ID}}
		incURL := incPath + "/" + strconv.Itoa(int(inc.ID))
		assertGlobalForbidden(t, f.do("PUT", incURL, f.editorToken, upd))
		ok := f.do("PUT", incURL, f.adminToken, upd)
		assert.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
	})
}

func TestGetPageTagsHandler_DBErrorReturns500(t *testing.T) {
	f := newPageTagsFixture(t)
	require.NoError(t, f.db.Exec(`DROP TABLE tags`).Error)
	w := f.do("GET", "/api/pages/"+pageIDStr(f.page)+"/tags", f.readerToken, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
