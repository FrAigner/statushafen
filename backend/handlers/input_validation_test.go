package handlers_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Das Backend prüft, was das Frontend prüft (leerer Name,
// Passwort-Mindestlänge, Sprachcode-Format, reservierte Slugs). Jeder Verstoß
// liefert 400 mit `code`, ohne etwas anzulegen oder zu ändern.

func newInputValidationRouter(t *testing.T) (*gorm.DB, http.Handler) {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/admin/pages", h.CreateStatusPageHandler)
	r.PUT("/api/admin/pages/:page_id", h.UpdateStatusPageHandler)
	r.POST("/api/users", h.CreateUserHandler)
	r.PUT("/api/users/:id", h.UpdateUserHandler)
	r.POST("/api/languages", h.CreateLanguageHandler)
	r.PUT("/api/languages/:id", h.UpdateLanguageHandler)
	r.POST("/api/public/orders", h.CreateOrderHandler)
	r.POST("/api/public/orders/set-password", h.SetOrderPasswordHandler)
	return db, r
}

func count(db *gorm.DB, model any, query string, args ...any) int64 {
	var n int64
	db.Model(model).Where(query, args...).Count(&n)
	return n
}

func TestStatusPageCreate_NameRequired(t *testing.T) {
	db, r := newInputValidationRouter(t)
	for _, name := range []string{"", "   "} {
		w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]any{"Name": name})
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		requireErrorCode(t, w.Body.Bytes(), "name_required")
	}
	assert.Zero(t, count(db, &models.StatusPage{}, "1 = 1"), "keine Seite ohne Namen")
}

func TestStatusPageUpdate_NameRequired(t *testing.T) {
	db, r := newInputValidationRouter(t)
	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	require.NoError(t, db.Create(&page).Error)

	w := doStatusPageRequest(r, "PUT", "/api/admin/pages/"+itoa(page.ID), map[string]any{"Name": " ", "Slug": "acme"})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "name_required")
	require.NoError(t, db.First(&page, page.ID).Error)
	assert.Equal(t, "Acme", page.Name)
}

func TestStatusPageUpdate_UnknownPageIs404(t *testing.T) {
	_, r := newInputValidationRouter(t)
	w := doStatusPageRequest(r, "PUT", "/api/admin/pages/9999", map[string]any{"Name": "X", "Slug": "x"})
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStatusPageCreate_ReservedSlugRejected(t *testing.T) {
	db, r := newInputValidationRouter(t)
	for _, slug := range []string{"www", "staging", "admin", "api", "login", "docs", "status", "healthz", "static", "assets", "system", "WWW"} {
		w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]any{"Name": "Reserviert", "Slug": slug})
		require.Equal(t, http.StatusBadRequest, w.Code, slug+": "+w.Body.String())
		requireErrorCode(t, w.Body.Bytes(), "slug_reserved")
	}
	assert.Zero(t, count(db, &models.StatusPage{}, "1 = 1"))

	// Ein freier Slug und ein aus dem Namen erzeugter Slug bleiben möglich.
	w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]any{"Name": "Acme", "Slug": "acme"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func TestStatusPageUpdate_ReservedSlugOnlyBlockedWhenChanged(t *testing.T) {
	db, r := newInputValidationRouter(t)
	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	require.NoError(t, db.Create(&page).Error)
	path := "/api/admin/pages/" + itoa(page.ID)

	w := doStatusPageRequest(r, "PUT", path, map[string]any{"Name": "Acme", "Slug": "www"})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "slug_reserved")
	require.NoError(t, db.First(&page, page.ID).Error)
	assert.Equal(t, "acme", page.Slug)

	// Bestandsdaten: eine Seite, die schon einen reservierten Slug trägt, darf
	// ihn behalten und weiter bearbeitet werden.
	legacy := models.StatusPage{Name: "Alt", Slug: "docs"}
	require.NoError(t, db.Create(&legacy).Error)
	w = doStatusPageRequest(r, "PUT", "/api/admin/pages/"+itoa(legacy.ID), map[string]any{"Name": "Alt neu", "Slug": "docs"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestStatusPageUpdate_SystemPageKeepsReservedSlug(t *testing.T) {
	db, r := newInputValidationRouter(t)
	database.SeedSystemStatusPage(db)
	var system models.StatusPage
	require.NoError(t, db.Where("is_system = ?", true).First(&system).Error)

	w := doStatusPageRequest(r, "PUT", "/api/admin/pages/"+itoa(system.ID), map[string]any{"Name": "Unser Status", "Slug": database.SystemStatusPageSlug})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestUserPassword_MinLength(t *testing.T) {
	db, r := newInputValidationRouter(t)

	w := doStatusPageRequest(r, "POST", "/api/users", map[string]any{"email": "kurz@example.com", "password": "kurz"})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "password_too_short")
	assert.Zero(t, count(db, &models.User{}, "email = ?", "kurz@example.com"))

	// Genau 8 Zeichen (Umlaute zählen als ein Zeichen) sind erlaubt.
	w = doStatusPageRequest(r, "POST", "/api/users", map[string]any{"email": "ok@example.com", "password": "äöüäöüäö"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	w = doStatusPageRequest(r, "POST", "/api/users", map[string]any{"email": "sieben@example.com", "password": "äöüäöüä"})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestUserUpdate_PasswordMinLengthOnlyWhenSet(t *testing.T) {
	db, r := newInputValidationRouter(t)
	user := models.User{Email: "u@example.com", PasswordHash: "alt-hash"}
	require.NoError(t, db.Create(&user).Error)
	path := "/api/users/" + itoa(user.ID)

	w := doStatusPageRequest(r, "PUT", path, map[string]any{"email": "u@example.com", "password": "kurz"})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "password_too_short")
	require.NoError(t, db.First(&user, user.ID).Error)
	assert.Equal(t, "alt-hash", user.PasswordHash)

	// Leer = unverändert.
	w = doStatusPageRequest(r, "PUT", path, map[string]any{"email": "u@example.com", "password": ""})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, db.First(&user, user.ID).Error)
	assert.Equal(t, "alt-hash", user.PasswordHash)

	w = doStatusPageRequest(r, "PUT", path, map[string]any{"email": "u@example.com", "password": "lang-genug-123"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, db.First(&user, user.ID).Error)
	assert.NotEqual(t, "alt-hash", user.PasswordHash)
}

func TestSetOrderPassword_TooShortHasCode(t *testing.T) {
	_, r := newInputValidationRouter(t)
	w := doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]any{"token": "egal", "password": "1234567"})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "password_too_short")
}

func TestOrderCreate_ReservedSlugRejected(t *testing.T) {
	db, r := newInputValidationRouter(t)
	for _, slug := range []string{"www", "staging", "WWW"} {
		w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]any{"company_name": "Acme", "email": "o@example.com", "slug": slug})
		require.Equal(t, http.StatusBadRequest, w.Code, slug+": "+w.Body.String())
		requireErrorCode(t, w.Body.Bytes(), "slug_reserved")
	}
	assert.Zero(t, count(db, &models.PageOrder{}, "1 = 1"), "keine Bestellung mit reserviertem Slug")
}

func TestLanguageCreate_CodeFormatAndName(t *testing.T) {
	db, r := newInputValidationRouter(t)
	invalid := []string{"x1", "x", "abcd", "de_de", "de-", "1a", "de-x", "en-us-abcdefghi", "de de"}
	for _, code := range invalid {
		w := doStatusPageRequest(r, "POST", "/api/languages", map[string]any{"code": code, "name": "Test"})
		require.Equal(t, http.StatusBadRequest, w.Code, code+": "+w.Body.String())
		requireErrorCode(t, w.Body.Bytes(), "language_code_invalid")
	}
	w := doStatusPageRequest(r, "POST", "/api/languages", map[string]any{"code": "", "name": "Test"})
	require.Equal(t, http.StatusBadRequest, w.Code)
	requireErrorCode(t, w.Body.Bytes(), "language_code_invalid")
	w = doStatusPageRequest(r, "POST", "/api/languages", map[string]any{"code": "fr", "name": "  "})
	require.Equal(t, http.StatusBadRequest, w.Code)
	requireErrorCode(t, w.Body.Bytes(), "name_required")

	for i, code := range []string{"fr", "ZH-Hant-TW", "fil", "pt-br"} {
		w = doStatusPageRequest(r, "POST", "/api/languages", map[string]any{"code": code, "name": "Gültig " + strconv.Itoa(i)})
		require.Equal(t, http.StatusCreated, w.Code, code+": "+w.Body.String())
	}
	assert.Zero(t, count(db, &models.Language{}, "code IN ?", invalid))
}

func TestLanguageUpdate_NameRequiredHasCode(t *testing.T) {
	db, r := newInputValidationRouter(t)
	lang := models.Language{Code: "fr", Name: "Français", IsActive: true}
	require.NoError(t, db.Create(&lang).Error)

	w := doStatusPageRequest(r, "PUT", "/api/languages/"+itoa(lang.ID), map[string]any{"name": " "})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "name_required")
	w = doStatusPageRequest(r, "PUT", "/api/languages/9999", map[string]any{"name": "X"})
	assert.Equal(t, http.StatusNotFound, w.Code)
}
