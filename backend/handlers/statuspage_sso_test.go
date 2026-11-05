package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func setupSSORouter(h *Handler) http.Handler {
	r := testutils.SetupRouter()
	r.GET("/api/admin/pages/:page_id/sso", h.GetSSO)
	r.PUT("/api/admin/pages/:page_id/sso", h.UpdateSSO)
	r.DELETE("/api/admin/pages/:page_id/sso", h.DeleteSSO)
	return r
}

func doSSORequest(r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGetSSO(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := setupSSORouter(h)

	page := models.StatusPage{Name: "Test", Slug: "test"}
	db.Create(&page)

	t.Run("SSO-Config nicht vorhanden — Default zurückgeben", func(t *testing.T) {
		w := doSSORequest(r, "GET", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/sso", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["oidc_enabled"] != false {
			t.Errorf("expected oidc_enabled=false, got %v", resp["oidc_enabled"])
		}
	})

	t.Run("SSO-Config vorhanden — zurückgeben", func(t *testing.T) {
		ssoConfig := models.StatusPageSSOConfig{
			StatusPageID:       page.ID,
			OIDCEnabled:        true,
			OIDCDiscoveryURL:   "https://accounts.google.com/.well-known/openid-configuration",
			OIDCClientID:       "test-client-id",
			OIDCAttributeEmail: "email",
		}
		db.Create(&ssoConfig)

		w := doSSORequest(r, "GET", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/sso", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["oidc_enabled"] != true {
			t.Errorf("expected oidc_enabled=true, got %v", resp["oidc_enabled"])
		}
		if resp["oidc_client_id"] != "test-client-id" {
			t.Errorf("expected oidc_client_id=test-client-id, got %v", resp["oidc_client_id"])
		}
	})
}

func TestUpdateSSO(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := setupSSORouter(h)

	page1 := models.StatusPage{Name: "Test1", Slug: "test1"}
	db.Create(&page1)
	page2 := models.StatusPage{Name: "Test2", Slug: "test2"}
	db.Create(&page2)

	t.Run("OIDC aktivieren mit vollständiger Config", func(t *testing.T) {
		payload := map[string]interface{}{
			"oidc_enabled":         true,
			"oidc_discovery_url":   "https://accounts.google.com/.well-known/openid-configuration",
			"oidc_client_id":       "test-client-id",
			"oidc_client_secret":   "test-secret",
			"oidc_scopes":          "openid email profile",
			"oidc_attribute_email": "email",
			"oidc_attribute_name":  "name",
			"test_login_enabled":   true,
		}
		w := doSSORequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page1.ID))+"/sso", payload)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["oidc_enabled"] != true {
			t.Errorf("expected oidc_enabled=true, got %v", resp["oidc_enabled"])
		}

		var savedConfig models.StatusPageSSOConfig
		db.Where("status_page_id = ?", page1.ID).First(&savedConfig)
		if !savedConfig.OIDCEnabled {
			t.Errorf("expected saved OIDC to be enabled")
		}
	})

	t.Run("OIDC-Validierung: enabled aber keine Credentials wird abgelehnt", func(t *testing.T) {
		payload := map[string]interface{}{
			"oidc_enabled":       true,
			"oidc_discovery_url": "", // missing
			"oidc_client_id":     "test-client-id",
		}
		w := doSSORequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page1.ID))+"/sso", payload)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 (validation error), got %d", w.Code)
		}
	})

	t.Run("OIDC deaktivieren", func(t *testing.T) {
		payload := map[string]interface{}{
			"oidc_enabled": false,
		}
		w := doSSORequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page2.ID))+"/sso", payload)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var savedConfig models.StatusPageSSOConfig
		db.Where("status_page_id = ?", page2.ID).First(&savedConfig)
		if savedConfig.OIDCEnabled {
			t.Errorf("expected saved OIDC to be disabled")
		}
	})
}

func TestDeleteSSO(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := setupSSORouter(h)

	page := models.StatusPage{Name: "Test", Slug: "test"}
	db.Create(&page)
	ssoConfig := models.StatusPageSSOConfig{
		StatusPageID:     page.ID,
		OIDCEnabled:      true,
		OIDCDiscoveryURL: "https://accounts.google.com/.well-known/openid-configuration",
		OIDCClientID:     "test-client-id",
	}
	db.Create(&ssoConfig)

	t.Run("SSO-Config löschen", func(t *testing.T) {
		w := doSSORequest(r, "DELETE", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/sso", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var savedConfig models.StatusPageSSOConfig
		err := db.Where("status_page_id = ?", page.ID).First(&savedConfig).Error
		if err != gorm.ErrRecordNotFound {
			t.Errorf("expected config to be deleted, but found it or error: %v", err)
		}
	})
}

// UpdateSSO legte bei jedem PUT eine neue Zeile an. Der
// Unique-Index auf status_page_id ließ deshalb jeden zweiten PUT (und jeden PUT
// nach DELETE, weil das Soft-Delete die Zeile stehen lässt) mit 500 scheitern –
// Admins konnten ihre OIDC-Einstellungen nach dem ersten Speichern nicht mehr
// ändern.
func TestUpdateSSO_Upsert(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := setupSSORouter(h)

	page := models.StatusPage{Name: "Upsert", Slug: "upsert"}
	db.Create(&page)
	path := "/api/admin/pages/" + strconv.Itoa(int(page.ID)) + "/sso"

	ssoPayload := func(clientID, secret string, testLogin bool) map[string]interface{} {
		return map[string]interface{}{
			"oidc_enabled":         true,
			"oidc_discovery_url":   "https://idp.example/.well-known/openid-configuration",
			"oidc_client_id":       clientID,
			"oidc_client_secret":   secret,
			"oidc_scopes":          "openid email",
			"oidc_attribute_email": "mail",
			"oidc_attribute_name":  "displayName",
			"oidc_attribute_role":  "groups",
			"test_login_enabled":   testLogin,
		}
	}
	getSSO := func(t *testing.T) map[string]interface{} {
		t.Helper()
		w := doSSORequest(r, "GET", path, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("GET: expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp
	}
	countRows := func(t *testing.T) int64 {
		t.Helper()
		var n int64
		db.Unscoped().Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", page.ID).Count(&n)
		return n
	}

	t.Run("zweiter PUT aktualisiert die bestehende Config", func(t *testing.T) {
		if w := doSSORequest(r, "PUT", path, ssoPayload("client-1", "secret-1", true)); w.Code != http.StatusOK {
			t.Fatalf("erster PUT: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		w := doSSORequest(r, "PUT", path, ssoPayload("client-2", "secret-2", false))
		if w.Code != http.StatusOK {
			t.Fatalf("zweiter PUT: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["oidc_client_id"] != "client-2" {
			t.Errorf("PUT-Antwort: expected client-2, got %v", resp["oidc_client_id"])
		}

		got := getSSO(t)
		if got["oidc_client_id"] != "client-2" || got["test_login_enabled"] != false {
			t.Errorf("GET nach zweitem PUT liefert alte Werte: %v", got)
		}
		if _, leaked := got["oidc_client_secret"]; leaked {
			t.Errorf("Secret darf nicht ausgeliefert werden")
		}
		var saved models.StatusPageSSOConfig
		db.Where("status_page_id = ?", page.ID).First(&saved)
		if saved.OIDCClientSecret != "secret-2" {
			t.Errorf("expected Secret secret-2, got %q", saved.OIDCClientSecret)
		}
		if n := countRows(t); n != 1 {
			t.Errorf("expected genau 1 Zeile, got %d", n)
		}
	})

	t.Run("PUT nach DELETE reaktiviert die soft-gelöschte Zeile", func(t *testing.T) {
		if w := doSSORequest(r, "DELETE", path, nil); w.Code != http.StatusOK {
			t.Fatalf("DELETE: expected 200, got %d", w.Code)
		}
		if got := getSSO(t); got["oidc_enabled"] != false {
			t.Fatalf("nach DELETE erwartet Default (deaktiviert), got %v", got)
		}

		w := doSSORequest(r, "PUT", path, ssoPayload("client-3", "secret-3", true))
		if w.Code != http.StatusOK {
			t.Fatalf("PUT nach DELETE: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		got := getSSO(t)
		if got["oidc_enabled"] != true || got["oidc_client_id"] != "client-3" ||
			got["oidc_attribute_role"] != "groups" || got["test_login_enabled"] != true {
			t.Errorf("GET nach Reaktivierung liefert falsche Werte: %v", got)
		}
		var saved models.StatusPageSSOConfig
		if err := db.Where("status_page_id = ?", page.ID).First(&saved).Error; err != nil {
			t.Fatalf("reaktivierte Config nicht gefunden: %v", err)
		}
		if saved.OIDCClientSecret != "secret-3" {
			t.Errorf("expected Secret secret-3, got %q", saved.OIDCClientSecret)
		}
		if n := countRows(t); n != 1 {
			t.Errorf("expected genau 1 Zeile (inkl. gelöschter), got %d", n)
		}
	})
}

// Fehlerpfade des Upserts – Lesen der bestehenden Zeile bzw.
// Speichern scheitert → 500 statt eines halben Zustands.
func TestUpdateSSO_DBErrors(t *testing.T) {
	payload := map[string]interface{}{"oidc_enabled": false}

	t.Run("Lesen der bestehenden Config scheitert", func(t *testing.T) {
		db := testutils.SetupTestDB()
		r := setupSSORouter(NewHandler(db))
		page := models.StatusPage{Name: "Err", Slug: "err-read"}
		db.Create(&page)
		if err := db.Migrator().DropTable(&models.StatusPageSSOConfig{}); err != nil {
			t.Fatalf("DropTable: %v", err)
		}
		w := doSSORequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/sso", payload)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})

	t.Run("Speichern scheitert", func(t *testing.T) {
		db := testutils.SetupTestDB()
		r := setupSSORouter(NewHandler(db))
		page := models.StatusPage{Name: "Err", Slug: "err-save"}
		db.Create(&page)
		cb := func(tx *gorm.DB) { _ = tx.AddError(gorm.ErrInvalidDB) }
		if err := db.Callback().Create().Before("gorm:create").Register("sso_fail_c", cb); err != nil {
			t.Fatalf("Callback nicht registriert: %v", err)
		}
		t.Cleanup(func() { _ = db.Callback().Create().Remove("sso_fail_c") })
		w := doSSORequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/sso", payload)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})
}
