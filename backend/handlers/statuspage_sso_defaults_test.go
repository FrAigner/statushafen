package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Bei der ersten Anlage übersprang GORM Nullwerte für Spalten mit
// default-Tag (test_login_enabled=false, leere Strings) und speicherte den
// DB-Default. false muss ankommen; leere Scopes/Attribute bekommen bewusst die
// Model-Defaults – beim Anlegen wie beim Aktualisieren gleich.
func TestUpdateSSO_ErstanlageSpeichertNullwerte(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupSSORouter(NewHandler(db))
	page := models.StatusPage{Name: "Null", Slug: "null"}
	db.Create(&page)
	path := "/api/admin/pages/" + strconv.Itoa(int(page.ID)) + "/sso"

	payload := map[string]interface{}{
		"oidc_enabled":         false,
		"oidc_scopes":          "",
		"oidc_attribute_email": "",
		"oidc_attribute_name":  "",
		"test_login_enabled":   false,
	}
	for _, label := range []string{"Erstanlage", "Aktualisierung"} {
		w := doSSORequest(r, "PUT", path, payload)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", label, w.Code, w.Body.String())
		}
		var saved models.StatusPageSSOConfig
		if err := db.Where("status_page_id = ?", page.ID).First(&saved).Error; err != nil {
			t.Fatalf("%s: Config nicht gefunden: %v", label, err)
		}
		if saved.TestLoginEnabled {
			t.Errorf("%s: test_login_enabled=false wurde nicht gespeichert", label)
		}
		if saved.OIDCScopes != "openid email profile" {
			t.Errorf("%s: expected Default-Scopes, got %q", label, saved.OIDCScopes)
		}
		if saved.OIDCAttributeEmail != "email" || saved.OIDCAttributeName != "name" {
			t.Errorf("%s: expected Default-Attribute, got %q/%q", label, saved.OIDCAttributeEmail, saved.OIDCAttributeName)
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["test_login_enabled"] != false || resp["oidc_scopes"] != "openid email profile" {
			t.Errorf("%s: PUT-Antwort falsch: %v", label, resp)
		}
	}
}

// Gesetzte Scopes/Attribute werden unverändert übernommen (die
// Defaults greifen nur bei leeren Werten).
func TestUpdateSSO_ErstanlageUebernimmtGesetzteWerte(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupSSORouter(NewHandler(db))
	page := models.StatusPage{Name: "Werte", Slug: "werte"}
	db.Create(&page)
	w := doSSORequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/sso", map[string]interface{}{
		"oidc_scopes":          "openid groups",
		"oidc_attribute_email": "mail",
		"oidc_attribute_name":  "displayName",
		"test_login_enabled":   true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var saved models.StatusPageSSOConfig
	db.Where("status_page_id = ?", page.ID).First(&saved)
	if saved.OIDCScopes != "openid groups" || saved.OIDCAttributeEmail != "mail" ||
		saved.OIDCAttributeName != "displayName" || !saved.TestLoginEnabled {
		t.Errorf("gesetzte Werte nicht übernommen: %+v", saved)
	}
}

// Auch ohne gespeicherte Config liefert GET alle Formularfelder,
// damit das Admin-Formular die Antwort direkt übernehmen kann.
func TestGetSSO_DefaultEnthaeltAlleFelder(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupSSORouter(NewHandler(db))
	page := models.StatusPage{Name: "Def", Slug: "def"}
	db.Create(&page)
	w := doSSORequest(r, "GET", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/sso", nil)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	want := map[string]interface{}{
		"oidc_discovery_url": "", "oidc_client_id": "", "oidc_attribute_role": "",
		"oidc_scopes": "openid email profile", "oidc_attribute_email": "email", "oidc_attribute_name": "name",
		"test_login_enabled": true, "oidc_client_secret_set": false,
	}
	for k, v := range want {
		if got, ok := resp[k]; !ok || got != v {
			t.Errorf("%s: expected %v, got %v (vorhanden: %v)", k, v, got, ok)
		}
	}
}

// oidc_acs_url wurde beim Speichern aus BASE_URL festgeschrieben
// und veraltete, sobald sich BASE_URL änderte – die echte Redirect-URL des
// Logins entsteht aber zur Laufzeit (oidcRedirectURL). GET liefert deshalb
// immer die aktuell gültige URL statt des gespeicherten Werts.
func TestGetSSO_ACSUrlZurLaufzeit(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupSSORouter(NewHandler(db))
	page := models.StatusPage{Name: "ACS", Slug: "acs"}
	db.Create(&page)
	id := strconv.Itoa(int(page.ID))
	db.Create(&models.StatusPageSSOConfig{StatusPageID: page.ID, OIDCACSUrl: "https://alt.example/api/auth/oidc/callback?page=" + id})
	t.Setenv("BASE_URL", "https://neu.example")

	w := doSSORequest(r, "GET", "/api/admin/pages/"+id+"/sso", nil)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	want := "https://neu.example/api/auth/oidc/callback?page=" + id
	if resp["oidc_acs_url"] != want {
		t.Errorf("GET: expected %s, got %v", want, resp["oidc_acs_url"])
	}
	if want != oidcRedirectURL(page.ID) {
		t.Errorf("Redirect-URL des Logins weicht ab: %s", oidcRedirectURL(page.ID))
	}
}

// GET liefert das Secret nie aus. Damit das Admin-Formular nicht
// bei jedem Speichern das Secret neu verlangt, behält ein leeres Secret beim
// Aktualisieren das gespeicherte; Pflicht ist es nur, solange keins gespeichert
// ist. GET meldet per oidc_client_secret_set, ob eins hinterlegt ist.
func TestUpdateSSO_SecretBeibehalten(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupSSORouter(NewHandler(db))
	page := models.StatusPage{Name: "Secret", Slug: "secret"}
	db.Create(&page)
	path := "/api/admin/pages/" + strconv.Itoa(int(page.ID)) + "/sso"

	payload := func(secret string) map[string]interface{} {
		return map[string]interface{}{
			"oidc_enabled":       true,
			"oidc_discovery_url": "https://idp.example/.well-known/openid-configuration",
			"oidc_client_id":     "client",
			"oidc_client_secret": secret,
			"test_login_enabled": true,
		}
	}
	secretSet := func(t *testing.T) interface{} {
		t.Helper()
		w := doSSORequest(r, "GET", path, nil)
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["oidc_client_secret_set"]
	}
	storedSecret := func(t *testing.T) string {
		t.Helper()
		var saved models.StatusPageSSOConfig
		db.Unscoped().Where("status_page_id = ?", page.ID).First(&saved)
		return saved.OIDCClientSecret
	}

	t.Run("ohne gespeichertes Secret meldet GET secret_set=false", func(t *testing.T) {
		if got := secretSet(t); got != false {
			t.Errorf("expected false, got %v", got)
		}
	})

	t.Run("Neuanlage mit aktiviertem OIDC ohne Secret wird abgelehnt", func(t *testing.T) {
		if w := doSSORequest(r, "PUT", path, payload("")); w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("leeres Secret beim Aktualisieren behält das gespeicherte", func(t *testing.T) {
		if w := doSSORequest(r, "PUT", path, payload("geheim")); w.Code != http.StatusOK {
			t.Fatalf("Anlage: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		w := doSSORequest(r, "PUT", path, payload(""))
		if w.Code != http.StatusOK {
			t.Fatalf("Update ohne Secret: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["oidc_client_secret_set"] != true {
			t.Errorf("PUT-Antwort: expected secret_set=true, got %v", resp["oidc_client_secret_set"])
		}
		if s := storedSecret(t); s != "geheim" {
			t.Errorf("expected gespeichertes Secret geheim, got %q", s)
		}
		if got := secretSet(t); got != true {
			t.Errorf("GET: expected secret_set=true, got %v", got)
		}
	})

	t.Run("neues Secret ersetzt das gespeicherte", func(t *testing.T) {
		if w := doSSORequest(r, "PUT", path, payload("neu")); w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if s := storedSecret(t); s != "neu" {
			t.Errorf("expected neu, got %q", s)
		}
	})

	t.Run("nach DELETE gilt das alte Secret nicht mehr", func(t *testing.T) {
		if w := doSSORequest(r, "DELETE", path, nil); w.Code != http.StatusOK {
			t.Fatalf("DELETE: expected 200, got %d", w.Code)
		}
		if w := doSSORequest(r, "PUT", path, payload("")); w.Code != http.StatusBadRequest {
			t.Fatalf("PUT ohne Secret nach DELETE: expected 400, got %d", w.Code)
		}
		if w := doSSORequest(r, "PUT", path, map[string]interface{}{"oidc_enabled": false}); w.Code != http.StatusOK {
			t.Fatalf("deaktivierter PUT nach DELETE: expected 200, got %d", w.Code)
		}
		if s := storedSecret(t); s != "" {
			t.Errorf("expected verworfenes Secret, got %q", s)
		}
	})
}

// Erstanlage läuft als INSERT + UPDATE in einer Transaktion;
// scheitert das UPDATE, bleibt keine halbe Zeile stehen.
func TestUpdateSSO_UpdateNachInsertScheitert(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupSSORouter(NewHandler(db))
	page := models.StatusPage{Name: "Err", Slug: "err-update"}
	db.Create(&page)
	cb := func(tx *gorm.DB) { _ = tx.AddError(gorm.ErrInvalidDB) }
	if err := db.Callback().Update().Before("gorm:update").Register("sso_fail_u", cb); err != nil {
		t.Fatalf("Callback nicht registriert: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove("sso_fail_u") })
	w := doSSORequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/sso", map[string]interface{}{"oidc_enabled": false})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	var n int64
	db.Unscoped().Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", page.ID).Count(&n)
	if n != 0 {
		t.Errorf("expected Rollback (0 Zeilen), got %d", n)
	}
}
