package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func checkDomain(r http.Handler, domain, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/internal/custom-domain-check?domain="+domain, nil)
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Der "ask"-Endpunkt für Caddy ist ein Orakel für Kundendomains. Außerhalb der
// Entwicklung verlangt er deshalb ein gesetztes CADDY_INTERNAL_TOKEN; ohne
// Token antwortet er 503 und nie mit einer Freigabe, auch nicht mit leerem Header.
func TestInternalCustomDomainCheck_TokenPflichtAusserhalbDerEntwicklung(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	require.NoError(t, db.Create(&models.StatusPage{Name: "Acme", Slug: "acme-gate", CustomDomain: "status.acme.example", CustomDomainStatus: "verified"}).Error)

	for _, env := range []string{"production", "prod", "staging", ""} {
		t.Run("ohne Token, APP_ENV="+env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			t.Setenv("CADDY_INTERNAL_TOKEN", "")
			assert.Equal(t, http.StatusServiceUnavailable, checkDomain(r, "status.acme.example", "").Code)
			assert.Equal(t, http.StatusServiceUnavailable, checkDomain(r, "unbekannt.example", "").Code, "auch unbekannte Domains: kein Orakel")
			assert.Equal(t, http.StatusServiceUnavailable, checkDomain(r, "status.acme.example", "irgendwas").Code)
		})
	}

	t.Run("mit Token in Produktion: richtiger Token 200, falscher/fehlender 404", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		t.Setenv("CADDY_INTERNAL_TOKEN", "s3cret-token")
		assert.Equal(t, http.StatusOK, checkDomain(r, "status.acme.example", "s3cret-token").Code)
		assert.Equal(t, http.StatusNotFound, checkDomain(r, "status.acme.example", "falsch").Code)
		assert.Equal(t, http.StatusNotFound, checkDomain(r, "status.acme.example", "").Code)
		assert.Equal(t, http.StatusNotFound, checkDomain(r, "unbekannt.example", "s3cret-token").Code)
	})

	// Gegenprobe: In der Entwicklung reicht der Aufruf ohne Token.
	for _, env := range []string{"development", "test", "local"} {
		t.Run("Entwicklung ohne Token, APP_ENV="+env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			t.Setenv("CADDY_INTERNAL_TOKEN", "")
			assert.Equal(t, http.StatusOK, checkDomain(r, "status.acme.example", "").Code)
			assert.Equal(t, http.StatusNotFound, checkDomain(r, "unbekannt.example", "").Code)
		})
	}
}

// Der Wechsel verified -> active geschieht nur über den internen Check
// (Caddy fragt, bevor es das Zertifikat holt), nicht durch einen anonymen
// Aufruf von resolve-domain.
func TestCustomDomain_StatusWechselNurUeberInternenCheck(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("CADDY_INTERNAL_TOKEN", "s3cret-token")
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	page := models.StatusPage{Name: "Acme", Slug: "acme-promote", CustomDomain: "status.acme.example", CustomDomainStatus: "verified"}
	pending := models.StatusPage{Name: "Beta", Slug: "beta-promote", CustomDomain: "status.beta.example", CustomDomainStatus: "pending"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&pending).Error)

	doJSONRequestExpect(t, r, "GET", "/api/public/resolve-domain?host=status.acme.example", nil, http.StatusOK)
	assertCustomDomainState(t, db, page.ID, "status.acme.example", "verified")

	assert.Equal(t, http.StatusNotFound, checkDomain(r, "status.acme.example", "falsch").Code)
	assertCustomDomainState(t, db, page.ID, "status.acme.example", "verified")

	assert.Equal(t, http.StatusOK, checkDomain(r, "status.acme.example", "s3cret-token").Code)
	assertCustomDomainState(t, db, page.ID, "status.acme.example", "active")

	assert.Equal(t, http.StatusNotFound, checkDomain(r, "status.beta.example", "s3cret-token").Code)
	assertCustomDomainState(t, db, pending.ID, "status.beta.example", "pending")
}

// resolve-domain liefert den Slug einer privaten Seite nicht an Anonyme und
// nicht an Angemeldete ohne Rolle auf der Seite; Mitglieder bekommen ihn.
func TestResolvePublicDomain_PrivateSeiteOhneSlugFuerAnonyme(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		if v := c.GetHeader("X-Mock-User"); v != "" {
			id, _ := strconv.ParseUint(v, 10, 32)
			c.Set("userID", uint(id))
		}
	})
	r.GET("/api/public/resolve-domain", h.ResolvePublicDomainHandler)

	private := models.StatusPage{Name: "Intern", Slug: "intern-geheim", IsPrivate: true, CustomDomain: "status.intern.example", CustomDomainStatus: "active"}
	public := models.StatusPage{Name: "Offen", Slug: "offen", CustomDomain: "status.offen.example", CustomDomainStatus: "active"}
	require.NoError(t, db.Create(&private).Error)
	require.NoError(t, db.Create(&public).Error)
	role := models.Role{Name: "Leser", Permissions: "incidents:read"}
	require.NoError(t, db.Create(&role).Error)
	member := models.User{Email: "mitglied@example.com"}
	outsider := models.User{Email: "fremd@example.com"}
	require.NoError(t, db.Create(&member).Error)
	require.NoError(t, db.Create(&outsider).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: member.ID, StatusPageID: private.ID, RoleID: role.ID}).Error)

	get := func(host string, user *models.User) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/public/resolve-domain?host="+host, nil)
		if user != nil {
			req.Header.Set("X-Mock-User", strconv.FormatUint(uint64(user.ID), 10))
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	unknown := get("status.unbekannt.example", nil)
	anon := get("status.intern.example", nil)
	assert.Equal(t, http.StatusNotFound, anon.Code)
	assert.NotContains(t, anon.Body.String(), "intern-geheim")
	assert.Equal(t, unknown.Body.String(), anon.Body.String(), "private Seite sieht aus wie eine unbekannte Domain")
	assert.Equal(t, http.StatusNotFound, get("status.intern.example", &outsider).Code)

	w := get("status.intern.example", &member)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "intern-geheim")

	// Gegenprobe: öffentliche Seiten lösen weiter für alle auf.
	w = get("status.offen.example", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"slug":"offen"`)
}

// Das Hochstufen verified -> active gilt nur für die Zeile, die der Check
// gelesen hat. Ändert sich die Domain zwischen Lesen und Schreiben (hier: der
// Status wird zurückgesetzt), bleibt die neue Lage bestehen und es wird nichts
// hochgestuft.
func TestInternalCustomDomainCheck_HochstufenNurFuerUnveraenderteZeile(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("CADDY_INTERNAL_TOKEN", "s3cret-token")
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	page := models.StatusPage{Name: "Acme", Slug: "acme-race", CustomDomain: "status.acme.example", CustomDomainStatus: "verified"}
	require.NoError(t, db.Create(&page).Error)

	flipped := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:flip_after_read", func(tx *gorm.DB) {
		if tx.Statement.Table != "status_pages" || flipped {
			return
		}
		flipped = true
		// Zwischen Lesen und Schreiben setzt ein anderer Vorgang den Status zurück.
		tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE status_pages SET custom_domain_status = 'pending' WHERE id = ?", page.ID)
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove("test:flip_after_read") })

	assert.Equal(t, http.StatusOK, checkDomain(r, "status.acme.example", "s3cret-token").Code)
	require.True(t, flipped, "der Zwischenschritt muss gelaufen sein")
	assertCustomDomainState(t, db, page.ID, "status.acme.example", "pending")
}
