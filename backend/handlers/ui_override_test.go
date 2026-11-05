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

	"gorm.io/gorm"
)

// UiTranslationOverride: globale (Global Admin) und
// pro-Seite (Page Admin, nur eigene Seite) editierbare Overrides für
// App-UI-Texte. Diese Tests bauen dieselbe Router-Struktur wie main.go auf
// (AuthMiddleware + RequireGlobalAdmin/RequirePermission), analog zum
// Vorbild statuspage_users_test.go, damit echte Rechte-Eskalationslücken
// (fremde Seite, kein pages:write) tatsächlich geprüft werden statt nur
// den Happy Path mit fest gemocktem Global-Admin-Kontext.

func newUiOverrideHarness(t *testing.T) (*gorm.DB, http.Handler) {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/public/ui-overrides", h.GetPublicUiOverridesHandler)

	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())

	globalAdmin := authGroup.Group("/admin/ui-overrides")
	globalAdmin.Use(middleware.RequireGlobalAdmin())
	globalAdmin.GET("", h.GetGlobalUiOverridesHandler)
	globalAdmin.PUT("", h.PutGlobalUiOverrideHandler)
	globalAdmin.DELETE("", h.DeleteGlobalUiOverrideHandler)

	pageAdmin := authGroup.Group("/pages/:page_id/ui-overrides")
	pageAdmin.GET("", h.GetPageUiOverridesHandler)
	pageAdmin.PUT("", middleware.RequirePermission("pages:write"), h.PutPageUiOverrideHandler)
	pageAdmin.DELETE("", middleware.RequirePermission("pages:write"), h.DeletePageUiOverrideHandler)

	return db, r
}

func doUiOverrideRequest(r http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doUiOverridePut(r http.Handler, path, token, languageCode, key, value string) *httptest.ResponseRecorder {
	body := map[string]string{"language_code": languageCode, "key": key, "value": value}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGlobalUiOverrides(t *testing.T) {
	db, r := newUiOverrideHarness(t)

	globalAdminRole := models.Role{Name: "Global Admin (Test)", Permissions: "pages:write"}
	db.Create(&globalAdminRole)
	globalAdminUser := models.User{Email: "global-admin@example.com", IsGlobalAdmin: true, RoleID: globalAdminRole.ID}
	db.Create(&globalAdminUser)
	globalAdminToken, _ := middleware.GenerateToken(globalAdminUser.ID, globalAdminUser.Email, "", "", true)

	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	db.Create(&page)
	pageAdminRole := models.Role{Name: "Page Admin", Permissions: "pages:write"}
	db.Create(&pageAdminRole)
	pageAdminUser := models.User{Email: "page-admin@example.com"}
	db.Create(&pageAdminUser)
	db.Create(&models.UserStatusPageRole{UserID: pageAdminUser.ID, StatusPageID: page.ID, RoleID: pageAdminRole.ID})
	pageAdminToken, _ := middleware.GenerateToken(pageAdminUser.ID, pageAdminUser.Email, "", "", false)

	t.Run("Ohne Auth -> 401", func(t *testing.T) {
		w := doUiOverrideRequest(r, http.MethodGet, "/api/admin/ui-overrides?language_code=de", "")
		requireStatusCode(t, w, http.StatusUnauthorized)
	})

	t.Run("Page Admin ohne Global-Admin-Rechte -> 403", func(t *testing.T) {
		w := doUiOverridePut(r, "/api/admin/ui-overrides", pageAdminToken, "de", "nav.items.sso", "Custom")
		requireStatusCode(t, w, http.StatusForbidden)
	})

	t.Run("Global Admin legt Override an, GET zeigt ihn", func(t *testing.T) {
		w := doUiOverridePut(r, "/api/admin/ui-overrides", globalAdminToken, "de", "nav.items.sso", "Mein SSO")
		requireStatusCode(t, w, http.StatusOK)

		w = doUiOverrideRequest(r, http.MethodGet, "/api/admin/ui-overrides?language_code=de", globalAdminToken)
		requireStatusCode(t, w, http.StatusOK)
		var overrides map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &overrides)
		if overrides["nav.items.sso"] != "Mein SSO" {
			t.Fatalf("expected override to be set, got %+v", overrides)
		}
	})

	t.Run("Erneutes PUT desselben Keys aktualisiert statt zu duplizieren", func(t *testing.T) {
		doUiOverridePut(r, "/api/admin/ui-overrides", globalAdminToken, "de", "nav.items.sso", "Erst")
		doUiOverridePut(r, "/api/admin/ui-overrides", globalAdminToken, "de", "nav.items.sso", "Dann")

		var count int64
		db.Model(&models.UiTranslationOverride{}).Where("language_code = ? AND key = ?", "de", "nav.items.sso").Count(&count)
		if count != 1 {
			t.Fatalf("expected exactly one row after two PUTs of the same key, got %d", count)
		}
	})

	t.Run("DELETE entfernt den Override wieder (Revert auf Default)", func(t *testing.T) {
		doUiOverridePut(r, "/api/admin/ui-overrides", globalAdminToken, "de", "nav.items.sso", "Mein SSO")
		w := doUiOverrideRequest(r, http.MethodDelete, "/api/admin/ui-overrides?language_code=de&key=nav.items.sso", globalAdminToken)
		requireStatusCode(t, w, http.StatusOK)

		w = doUiOverrideRequest(r, http.MethodGet, "/api/admin/ui-overrides?language_code=de", globalAdminToken)
		var overrides map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &overrides)
		if _, exists := overrides["nav.items.sso"]; exists {
			t.Fatalf("expected override to be gone after delete, got %+v", overrides)
		}
	})

	t.Run("DELETE ist idempotent (kein Fehler ohne vorhandenen Override)", func(t *testing.T) {
		w := doUiOverrideRequest(r, http.MethodDelete, "/api/admin/ui-overrides?language_code=de&key=never.existed", globalAdminToken)
		requireStatusCode(t, w, http.StatusOK)
	})

	t.Run("Soft-Delete gefolgt von Neuanlage desselben Keys funktioniert (partieller Unique-Index)", func(t *testing.T) {
		doUiOverridePut(r, "/api/admin/ui-overrides", globalAdminToken, "de", "recreate.me", "Erst")
		doUiOverrideRequest(r, http.MethodDelete, "/api/admin/ui-overrides?language_code=de&key=recreate.me", globalAdminToken)
		w := doUiOverridePut(r, "/api/admin/ui-overrides", globalAdminToken, "de", "recreate.me", "Neu angelegt")
		requireStatusCode(t, w, http.StatusOK)
	})

	t.Run("GET erfordert language_code", func(t *testing.T) {
		w := doUiOverrideRequest(r, http.MethodGet, "/api/admin/ui-overrides", globalAdminToken)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("PUT lehnt leeren Key/Value ab", func(t *testing.T) {
		w := doUiOverridePut(r, "/api/admin/ui-overrides", globalAdminToken, "de", "", "Wert")
		requireStatusCode(t, w, http.StatusBadRequest)
	})
}

func TestPageUiOverrides(t *testing.T) {
	db, r := newUiOverrideHarness(t)

	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	db.Create(&page)
	otherPage := models.StatusPage{Name: "Other", Slug: "other"}
	db.Create(&otherPage)

	pageAdminRole := models.Role{Name: "Page Admin", Permissions: "pages:write"}
	db.Create(&pageAdminRole)
	editorRole := models.Role{Name: "Editor", Permissions: "incidents:write"}
	db.Create(&editorRole)

	pageAdminUser := models.User{Email: "page-admin@example.com"}
	db.Create(&pageAdminUser)
	db.Create(&models.UserStatusPageRole{UserID: pageAdminUser.ID, StatusPageID: page.ID, RoleID: pageAdminRole.ID})
	pageAdminToken, _ := middleware.GenerateToken(pageAdminUser.ID, pageAdminUser.Email, "", "", false)

	// Editor ohne pages:write auf `page` - darf lesen, aber nicht schreiben.
	editorUser := models.User{Email: "editor@example.com"}
	db.Create(&editorUser)
	db.Create(&models.UserStatusPageRole{UserID: editorUser.ID, StatusPageID: page.ID, RoleID: editorRole.ID})
	editorToken, _ := middleware.GenerateToken(editorUser.ID, editorUser.Email, "", "", false)

	globalAdminRole := models.Role{Name: "Global Admin (Test)", Permissions: "pages:write"}
	db.Create(&globalAdminRole)
	globalAdminUser := models.User{Email: "global-admin@example.com", IsGlobalAdmin: true, RoleID: globalAdminRole.ID}
	db.Create(&globalAdminUser)
	globalAdminToken, _ := middleware.GenerateToken(globalAdminUser.ID, globalAdminUser.Email, "", "", true)

	pagePath := "/api/pages/" + strconv.FormatUint(uint64(page.ID), 10) + "/ui-overrides"
	otherPagePath := "/api/pages/" + strconv.FormatUint(uint64(otherPage.ID), 10) + "/ui-overrides"

	t.Run("Page Admin legt Override für eigene Seite an", func(t *testing.T) {
		w := doUiOverridePut(r, pagePath, pageAdminToken, "de", "adminNav.items.sso", "Seiten-SSO")
		requireStatusCode(t, w, http.StatusOK)

		w = doUiOverrideRequest(r, http.MethodGet, pagePath+"?language_code=de", pageAdminToken)
		var overrides map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &overrides)
		if overrides["adminNav.items.sso"] != "Seiten-SSO" {
			t.Fatalf("expected page override to be set, got %+v", overrides)
		}
	})

	t.Run("Editor ohne pages:write kann lesen, aber nicht schreiben", func(t *testing.T) {
		w := doUiOverrideRequest(r, http.MethodGet, pagePath+"?language_code=de", editorToken)
		requireStatusCode(t, w, http.StatusOK)

		w = doUiOverridePut(r, pagePath, editorToken, "de", "adminNav.items.sso", "Darf ich nicht")
		requireStatusCode(t, w, http.StatusForbidden)
	})

	t.Run("pages:write auf fremder Seite reicht nicht (403, Regressionstest gegen Rechte-Eskalation)", func(t *testing.T) {
		w := doUiOverridePut(r, otherPagePath, pageAdminToken, "de", "adminNav.items.sso", "Fremde Seite")
		requireStatusCode(t, w, http.StatusForbidden)
	})

	t.Run("Overrides sind strikt pro Seite getrennt", func(t *testing.T) {
		doUiOverridePut(r, otherPagePath, globalAdminToken, "de", "adminNav.items.sso", "Andere Seite")

		w := doUiOverrideRequest(r, http.MethodGet, pagePath+"?language_code=de", pageAdminToken)
		var overrides map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &overrides)
		if overrides["adminNav.items.sso"] != "Seiten-SSO" {
			t.Fatalf("expected this page's own override, got %+v", overrides)
		}
	})

	t.Run("Global Admin kann jede Seite editieren (bestehendes Bypass-Verhalten)", func(t *testing.T) {
		w := doUiOverridePut(r, otherPagePath, globalAdminToken, "de", "common.save", "Speichern (angepasst)")
		requireStatusCode(t, w, http.StatusOK)
	})

	t.Run("DELETE entfernt nur den Override dieser Seite", func(t *testing.T) {
		w := doUiOverrideRequest(r, http.MethodDelete, pagePath+"?language_code=de&key=adminNav.items.sso", pageAdminToken)
		requireStatusCode(t, w, http.StatusOK)

		w = doUiOverrideRequest(r, http.MethodGet, pagePath+"?language_code=de", pageAdminToken)
		var overrides map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &overrides)
		if _, exists := overrides["adminNav.items.sso"]; exists {
			t.Fatalf("expected page override to be gone, got %+v", overrides)
		}
	})
}

func TestPublicUiOverrides(t *testing.T) {
	db, r := newUiOverrideHarness(t)

	globalAdminRole := models.Role{Name: "Global Admin (Test)", Permissions: "pages:write"}
	db.Create(&globalAdminRole)
	globalAdminUser := models.User{Email: "global-admin@example.com", IsGlobalAdmin: true, RoleID: globalAdminRole.ID}
	db.Create(&globalAdminUser)
	globalAdminToken, _ := middleware.GenerateToken(globalAdminUser.ID, globalAdminUser.Email, "", "", true)

	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	db.Create(&page)
	pageAdminRole := models.Role{Name: "Page Admin", Permissions: "pages:write"}
	db.Create(&pageAdminRole)
	pageAdminUser := models.User{Email: "page-admin@example.com"}
	db.Create(&pageAdminUser)
	db.Create(&models.UserStatusPageRole{UserID: pageAdminUser.ID, StatusPageID: page.ID, RoleID: pageAdminRole.ID})
	pageAdminToken, _ := middleware.GenerateToken(pageAdminUser.ID, pageAdminUser.Email, "", "", false)

	doUiOverridePut(r, "/api/admin/ui-overrides", globalAdminToken, "de", "nav.items.sso", "Globaler Text")
	pagePath := "/api/pages/" + strconv.FormatUint(uint64(page.ID), 10) + "/ui-overrides"
	doUiOverridePut(r, pagePath, pageAdminToken, "de", "nav.items.sso", "Seiten-Text")

	t.Run("Öffentlicher Read ohne Auth liefert nur globale Overrides, keine Seiten-Overrides", func(t *testing.T) {
		w := doUiOverrideRequest(r, http.MethodGet, "/api/public/ui-overrides?language_code=de", "")
		requireStatusCode(t, w, http.StatusOK)
		var overrides map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &overrides)
		if overrides["nav.items.sso"] != "Globaler Text" {
			t.Fatalf("expected global override, got %+v", overrides)
		}
	})

	t.Run("Öffentlicher Read erfordert language_code", func(t *testing.T) {
		w := doUiOverrideRequest(r, http.MethodGet, "/api/public/ui-overrides", "")
		requireStatusCode(t, w, http.StatusBadRequest)
	})
}

// Schlüssel mit Prototyp-Segmenten oder Sonderzeichen dürfen nie
// gespeichert werden, sonst würden sie an alle Besucher ausgeliefert.
func TestUiOverrideRejectsUnsafeKeys(t *testing.T) {
	db, r := newUiOverrideHarness(t)

	role := models.Role{Name: "Global Admin (Test)", Permissions: "pages:write"}
	db.Create(&role)
	admin := models.User{Email: "global-admin@example.com", IsGlobalAdmin: true, RoleID: role.ID}
	db.Create(&admin)
	token, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	db.Create(&page)
	pagePath := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/ui-overrides"

	unsafeKeys := []string{
		"__proto__.polluted", "a.__proto__.b", "constructor.prototype.x", "prototype", "a.constructor",
		"nav..sso", ".nav", "nav.", "nav items", "nav/sso", "näv.sso",
	}
	for _, key := range unsafeKeys {
		t.Run("global "+key, func(t *testing.T) {
			w := doUiOverridePut(r, "/api/admin/ui-overrides", token, "de", key, "yes")
			requireStatusCode(t, w, http.StatusBadRequest)
		})
		t.Run("seite "+key, func(t *testing.T) {
			w := doUiOverridePut(r, pagePath, token, "de", key, "yes")
			requireStatusCode(t, w, http.StatusBadRequest)
		})
	}

	var count int64
	db.Model(&models.UiTranslationOverride{}).Count(&count)
	if count != 0 {
		t.Fatalf("unsichere Schlüssel wurden gespeichert: %d", count)
	}

	for _, key := range []string{"nav.items.sso", "common.save_all", "a-b.c_d.e1"} {
		t.Run("gültig "+key, func(t *testing.T) {
			w := doUiOverridePut(r, "/api/admin/ui-overrides", token, "de", key, "ok")
			requireStatusCode(t, w, http.StatusOK)
		})
	}
}

// "Speichern" einer Zeile mit leerem Override-Feld hat bisher ""
// als globalen Override gespeichert - der Text (z. B. nav.profile) war danach
// für alle Besucher leer. Ein leerer bzw. nur aus Leerzeichen bestehender Wert
// wird jetzt mit 400 abgelehnt und darf einen bestehenden Override nicht
// anfassen; Entfernen geht nur explizit per DELETE, und das wirkt strikt nur
// auf genau (Scope, Seite, Sprache, Key).
func TestUiOverrideEmptyValueAndDeleteScope(t *testing.T) {
	db, r := newUiOverrideHarness(t)

	role := models.Role{Name: "Global Admin (Test)", Permissions: "pages:write"}
	db.Create(&role)
	admin := models.User{Email: "global-admin@example.com", IsGlobalAdmin: true, RoleID: role.ID}
	db.Create(&admin)
	token, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	db.Create(&page)
	otherPage := models.StatusPage{Name: "Other", Slug: "other"}
	db.Create(&otherPage)
	const globalPath = "/api/admin/ui-overrides"
	pagePath := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/ui-overrides"
	otherPagePath := "/api/pages/" + strconv.Itoa(int(otherPage.ID)) + "/ui-overrides"

	readOverrides := func(t *testing.T, path, lang string) map[string]string {
		t.Helper()
		w := doUiOverrideRequest(r, http.MethodGet, path+"?language_code="+lang, token)
		requireStatusCode(t, w, http.StatusOK)
		var overrides map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &overrides); err != nil {
			t.Fatalf("Antwort nicht lesbar: %v", err)
		}
		return overrides
	}

	t.Run("leerer Wert ohne bestehenden Override legt nichts an", func(t *testing.T) {
		for _, value := range []string{"", "   ", "\t\n"} {
			w := doUiOverridePut(r, globalPath, token, "de", "nav.profile", value)
			requireStatusCode(t, w, http.StatusBadRequest)
		}
		w := doUiOverrideRequest(r, http.MethodGet, "/api/public/ui-overrides?language_code=de", "")
		requireStatusCode(t, w, http.StatusOK)
		var public map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &public)
		if _, exists := public["nav.profile"]; exists {
			t.Fatalf("leerer Override wurde öffentlich ausgeliefert: %+v", public)
		}
	})

	// Gleicher Key in allen Umfängen, damit jeder Übergriff auffällt.
	fixtures := []struct{ path, lang, value string }{
		{globalPath, "de", "Global DE"},
		{globalPath, "en", "Global EN"},
		{pagePath, "de", "Seite DE"},
		{pagePath, "en", "Seite EN"},
		{otherPagePath, "de", "Andere DE"},
	}
	for _, f := range fixtures {
		requireStatusCode(t, doUiOverridePut(r, f.path, token, f.lang, "nav.profile", f.value), http.StatusOK)
	}
	requireStatusCode(t, doUiOverridePut(r, pagePath, token, "de", "nav.home", "Start"), http.StatusOK)

	t.Run("leerer Wert lässt bestehenden Override unverändert", func(t *testing.T) {
		requireStatusCode(t, doUiOverridePut(r, globalPath, token, "de", "nav.profile", ""), http.StatusBadRequest)
		requireStatusCode(t, doUiOverridePut(r, pagePath, token, "de", "nav.profile", " "), http.StatusBadRequest)
		if got := readOverrides(t, globalPath, "de")["nav.profile"]; got != "Global DE" {
			t.Fatalf("globaler Override verändert: %q", got)
		}
		if got := readOverrides(t, pagePath, "de")["nav.profile"]; got != "Seite DE" {
			t.Fatalf("Seiten-Override verändert: %q", got)
		}
	})

	t.Run("DELETE auf Seite/Sprache/Key lässt alle anderen Umfänge stehen", func(t *testing.T) {
		w := doUiOverrideRequest(r, http.MethodDelete, pagePath+"?language_code=de&key=nav.profile", token)
		requireStatusCode(t, w, http.StatusOK)

		pageDE := readOverrides(t, pagePath, "de")
		if _, exists := pageDE["nav.profile"]; exists {
			t.Fatalf("Seiten-Override de nicht gelöscht: %+v", pageDE)
		}
		if pageDE["nav.home"] != "Start" {
			t.Fatalf("anderer Key der Seite mitgelöscht: %+v", pageDE)
		}
		expect := map[[2]string]string{
			{globalPath, "de"}:    "Global DE",
			{globalPath, "en"}:    "Global EN",
			{pagePath, "en"}:      "Seite EN",
			{otherPagePath, "de"}: "Andere DE",
		}
		for scope, want := range expect {
			if got := readOverrides(t, scope[0], scope[1])["nav.profile"]; got != want {
				t.Fatalf("%s/%s: erwartet %q, bekommen %q", scope[0], scope[1], want, got)
			}
		}
	})

	t.Run("globales DELETE lässt Seiten und andere Sprache stehen", func(t *testing.T) {
		w := doUiOverrideRequest(r, http.MethodDelete, globalPath+"?language_code=de&key=nav.profile", token)
		requireStatusCode(t, w, http.StatusOK)
		if _, exists := readOverrides(t, globalPath, "de")["nav.profile"]; exists {
			t.Fatal("globaler Override de nicht gelöscht")
		}
		if got := readOverrides(t, globalPath, "en")["nav.profile"]; got != "Global EN" {
			t.Fatalf("globaler Override en mitgelöscht: %q", got)
		}
		if got := readOverrides(t, otherPagePath, "de")["nav.profile"]; got != "Andere DE" {
			t.Fatalf("Seiten-Override mitgelöscht: %q", got)
		}
	})
}
