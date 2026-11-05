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
)

// Auto-Join per DefaultRoleID gilt nur noch für Konten, deren
// E-Mail-Domain in der Allowlist der Seite (AutoJoinEmailDomains) steht.
func TestAutoJoinRequiresAllowlistedEmailDomain(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages/:slug/dashboard", middleware.OptionalAuthMiddleware(), h.GetPublicDashboardHandler)

	role := models.Role{Name: "AJ Viewer", Permissions: "components:read"}
	db.Create(&role)

	cases := []struct {
		name    string
		domains string
		email   string
		want    int
	}{
		{"leere Allowlist: kein Auto-Join", "", "a@example.com", http.StatusNotFound},
		{"Domain passt", "example.com", "a@example.com", http.StatusOK},
		{"Gross-/Kleinschreibung egal", "example.com", "A@EXAMPLE.COM", http.StatusOK},
		{"zweite Domain der Liste passt", "other.org,example.com", "a@example.com", http.StatusOK},
		{"andere Domain", "example.com", "a@evil.org", http.StatusNotFound},
		{"Subdomain ist nicht dieselbe Domain", "example.com", "a@sub.example.com", http.StatusNotFound},
		{"Suffix-Trick", "example.com", "a@notexample.com", http.StatusNotFound},
		{"Domain im Local-Part", "example.com", "example.com@evil.org", http.StatusNotFound},
		{"Mail ohne @", "example.com", "kaputt", http.StatusNotFound},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slug := "aj-" + strconv.Itoa(i)
			page := models.StatusPage{Name: slug, Slug: slug, IsPrivate: true, DefaultRoleID: &role.ID, AutoJoinEmailDomains: tc.domains}
			assert.NoError(t, db.Create(&page).Error)
			u := models.User{Email: strconv.Itoa(i) + tc.email}
			assert.NoError(t, db.Create(&u).Error)
			token, _ := middleware.GenerateToken(u.ID, u.Email, "", "", false)

			req, _ := http.NewRequest("GET", "/api/public/pages/"+slug+"/dashboard", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.want, w.Code)

			var n int64
			db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", u.ID, page.ID).Count(&n)
			if tc.want == http.StatusOK {
				assert.Equal(t, int64(1), n)
			} else {
				assert.Equal(t, int64(0), n, "ohne Allowlist-Treffer darf keine Rolle angelegt werden")
			}
		})
	}
}

func putPage(t *testing.T, r http.Handler, id uint, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", "/api/admin/pages/"+strconv.Itoa(int(id)), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// DefaultRoleID wird serverseitig beschränkt, die Allowlist
// normalisiert und validiert.
func TestUpdateStatusPageValidatesAutoJoinSettings(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.Use(mockUserIDMiddleware)
	r.PUT("/api/admin/pages/:page_id", h.UpdateStatusPageHandler)
	r.POST("/api/admin/pages", h.CreateStatusPageHandler)

	viewer := models.Role{Name: "V318", Permissions: "components:read,incidents:read"}
	writer := models.Role{Name: "W318", Permissions: "components:read,incidents:write"}
	subs := models.Role{Name: "S318", Permissions: "subscribers:read"}
	db.Create(&viewer)
	db.Create(&writer)
	db.Create(&subs)
	page := models.StatusPage{Name: "P318", Slug: "p318", IsPrivate: true}
	db.Create(&page)

	base := func(extra map[string]interface{}) map[string]interface{} {
		m := map[string]interface{}{"Name": "P318", "Slug": "p318", "IsPrivate": true}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	t.Run("harmlose Rolle wird akzeptiert, Domains normalisiert", func(t *testing.T) {
		w := putPage(t, r, page.ID, base(map[string]interface{}{
			"DefaultRoleID":        viewer.ID,
			"AutoJoinEmailDomains": " @Example.COM, other.org ,example.com,",
		}))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var got models.StatusPage
		db.First(&got, page.ID)
		assert.Equal(t, "example.com,other.org", got.AutoJoinEmailDomains)
		assert.Equal(t, viewer.ID, *got.DefaultRoleID)
	})
	t.Run("Rolle mit :write wird abgelehnt", func(t *testing.T) {
		w := putPage(t, r, page.ID, base(map[string]interface{}{"DefaultRoleID": writer.ID}))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("Rolle mit subscribers:read wird abgelehnt", func(t *testing.T) {
		w := putPage(t, r, page.ID, base(map[string]interface{}{"DefaultRoleID": subs.ID}))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("unbekannte Rolle wird abgelehnt", func(t *testing.T) {
		w := putPage(t, r, page.ID, base(map[string]interface{}{"DefaultRoleID": 99999}))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("ungültige Domain wird abgelehnt", func(t *testing.T) {
		for _, bad := range []string{"keine domain", "a@b.de", "localhost", "-x.de", "x..de", "*.de"} {
			w := putPage(t, r, page.ID, base(map[string]interface{}{"AutoJoinEmailDomains": bad}))
			assert.Equal(t, http.StatusBadRequest, w.Code, bad)
		}
	})
	t.Run("leere Allowlist und keine Rolle sind erlaubt", func(t *testing.T) {
		w := putPage(t, r, page.ID, base(nil))
		assert.Equal(t, http.StatusOK, w.Code)
		var got models.StatusPage
		db.First(&got, page.ID)
		assert.Equal(t, "", got.AutoJoinEmailDomains)
		assert.Nil(t, got.DefaultRoleID)
	})
	t.Run("Anlegen validiert genauso", func(t *testing.T) {
		b, _ := json.Marshal(base(map[string]interface{}{"Slug": "p318-neu", "DefaultRoleID": writer.ID}))
		req, _ := http.NewRequest("POST", "/api/admin/pages", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// Die Rollenprüfung gilt auch beim Beitritt, nicht
// nur beim Speichern der Seite. Spätere Rechteänderungen an der Default-Rolle
// und Bestandsseiten dürfen Auto-Join-Konten keine Schreib- oder
// Abonnentenrechte geben.
func TestAutoJoinRechecksRoleAtJoinTime(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages/:slug/dashboard", middleware.OptionalAuthMiddleware(), h.GetPublicDashboardHandler)
	r.PUT("/api/admin/roles/:id", h.UpdateRoleHandler)

	join := func(t *testing.T, slug string, u models.User) *httptest.ResponseRecorder {
		token, _ := middleware.GenerateToken(u.ID, u.Email, "", "", false)
		req, _ := http.NewRequest("GET", "/api/public/pages/"+slug+"/dashboard", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	roleCount := func(u models.User, page models.StatusPage) int64 {
		var n int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", u.ID, page.ID).Count(&n)
		return n
	}

	setup := func(t *testing.T, name, perms string) (models.StatusPage, models.Role, models.User) {
		role := models.Role{Name: name, Permissions: perms}
		assert.NoError(t, db.Create(&role).Error)
		page := models.StatusPage{Name: name, Slug: name, IsPrivate: true, DefaultRoleID: &role.ID, AutoJoinEmailDomains: "example.com"}
		assert.NoError(t, db.Create(&page).Error)
		u := models.User{Email: name + "@example.com"}
		assert.NoError(t, db.Create(&u).Error)
		return page, role, u
	}

	for _, perms := range []string{"components:read,incidents:write", "components:read,subscribers:read"} {
		t.Run("nachträglich unsicher geänderte Rolle: "+perms, func(t *testing.T) {
			slug := "spaeter-" + strconv.Itoa(len(perms))
			page, role, u := setup(t, slug, "components:read")

			body, _ := json.Marshal(map[string]string{"name": role.Name, "permissions": perms})
			req, _ := http.NewRequest("PUT", "/api/admin/roles/"+strconv.Itoa(int(role.ID)), bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)

			assert.Equal(t, http.StatusNotFound, join(t, slug, u).Code)
			assert.Equal(t, int64(0), roleCount(u, page))
		})
	}

	t.Run("Bestandsseite mit unsicherer Rolle in der DB", func(t *testing.T) {
		page, _, u := setup(t, "bestand", "incidents:write")
		assert.Equal(t, http.StatusNotFound, join(t, "bestand", u).Code)
		assert.Equal(t, int64(0), roleCount(u, page))
	})

	t.Run("gelöschte Default-Rolle liefert 404 statt 500", func(t *testing.T) {
		page, role, u := setup(t, "geloescht", "components:read")
		assert.NoError(t, db.Unscoped().Delete(&role).Error)
		assert.Equal(t, http.StatusNotFound, join(t, "geloescht", u).Code)
		assert.Equal(t, int64(0), roleCount(u, page))
	})

	t.Run("sichere Rolle: Beitritt klappt weiterhin", func(t *testing.T) {
		page, _, u := setup(t, "sicher", "components:read")
		assert.Equal(t, http.StatusOK, join(t, "sicher", u).Code)
		assert.Equal(t, int64(1), roleCount(u, page))
	})
}

// Scheitert die Rollenvergabe beim Auto-Join an der Datenbank, ist das ein
// Serverfehler (500) und keine Antwort wie bei einer fehlenden Seite: Ein
// berechtigtes Konto soll den Fehler sehen, statt die Seite für nicht vorhanden
// zu halten.
func TestAutoJoinDatabaseFailureIs500NotNotFound(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages/:slug/dashboard", middleware.OptionalAuthMiddleware(), h.GetPublicDashboardHandler)
	role := models.Role{Name: "AJ Fehler", Permissions: "components:read"}
	require.NoError(t, db.Create(&role).Error)
	page := models.StatusPage{Name: "aj-fehler", Slug: "aj-fehler", IsPrivate: true, DefaultRoleID: &role.ID, AutoJoinEmailDomains: "example.com"}
	require.NoError(t, db.Create(&page).Error)
	u := models.User{Email: "fehler@example.com"}
	require.NoError(t, db.Create(&u).Error)
	token, _ := middleware.GenerateToken(u.ID, u.Email, "", "", false)
	failDB(t, db, "create", "user_status_page_roles", 0)

	req, _ := http.NewRequest("GET", "/api/public/pages/aj-fehler/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"server_error"`)
}
