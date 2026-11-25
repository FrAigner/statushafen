package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func parseTestClaims(t *testing.T, token string) *middleware.Claims {
	t.Helper()
	claims := &middleware.Claims{}
	if _, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (interface{}, error) {
		return []byte("test-jwt-secret-not-for-production"), nil
	}); err != nil {
		t.Fatalf("Token nicht lesbar: %v", err)
	}
	return claims
}

// A page SSO token carries the IdP's page, never global admin
// rights, and otherwise the same claims as GenerateTokenFull.
func TestGeneratePageSSOToken_SetsPageClaim(t *testing.T) {
	testutils.SetupTestDB()
	user := models.User{Email: "v@example.com", IsGlobalAdmin: true, PreferredLanguage: "de",
		Role: models.Role{Name: "Viewer", Permissions: "incidents:read"}}
	user.ID = 7

	token, err := middleware.GeneratePageSSOToken(user, "V", models.SSOProtocolOIDC, 42)
	if err != nil {
		t.Fatalf("GeneratePageSSOToken: %v", err)
	}
	c := parseTestClaims(t, token)
	if c.SSOPageID != 42 || c.IsGlobalAdmin || c.SSOProvider != "oidc" || c.UserID != 7 ||
		c.Permissions != "incidents:read" || c.Name != "V" || c.PreferredLanguage != "de" || c.ExpiresAt == nil {
		t.Errorf("Claims = %+v", c)
	}

	full, _ := middleware.GenerateTokenFull(user, "", "V", "google")
	if fc := parseTestClaims(t, full); fc.SSOPageID != 0 || !fc.IsGlobalAdmin {
		t.Errorf("GenerateTokenFull darf keinen Seitenbezug setzen: %+v", fc)
	}
}

// SSO logins take the user directly from the token. The
// tooltip setting must therefore be in the claim, otherwise the
// tooltips would be on again after every SSO login.
func TestGenerateTokenFull_HideHelpTooltips(t *testing.T) {
	testutils.SetupTestDB()
	off, _ := middleware.GenerateTokenFull(models.User{Email: "h@example.com", HideHelpTooltips: true}, "", "", "google")
	if !parseTestClaims(t, off).HideHelpTooltips {
		t.Error("Claim hide_help_tooltips fehlt")
	}
	on, _ := middleware.GenerateTokenFull(models.User{Email: "h@example.com"}, "", "", "google")
	if parseTestClaims(t, on).HideHelpTooltips {
		t.Error("Claim hide_help_tooltips bei Default gesetzt")
	}
}

// AuthMiddleware and OptionalAuthMiddleware take the claim
// into the context, other logins stay without page scope.
func TestAuthMiddlewares_SetSSOPageID(t *testing.T) {
	db := testutils.SetupTestDB()
	// The account must exist: AuthMiddleware checks the token against the DB.
	owner := models.User{Email: "v@example.com"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	scoped, _ := middleware.GeneratePageSSOToken(owner, "", "oidc", 5)
	plain, _ := middleware.GenerateToken(1, "a@example.com", "", "", false)

	r := testutils.SetupRouter()
	echo := func(c *gin.Context) { c.String(http.StatusOK, strconv.Itoa(int(middleware.SSOPageID(c)))) }
	r.GET("/auth", middleware.AuthMiddleware(), echo)
	r.GET("/opt", middleware.OptionalAuthMiddleware(), echo)

	for _, path := range []string{"/auth", "/opt"} {
		for token, want := range map[string]string{scoped: "5", plain: "0"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Body.String() != want {
				t.Errorf("%s: SSOPageID = %q, want %q", path, w.Body.String(), want)
			}
		}
	}
}

func TestSSOScopeHelpers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	admin := models.User{IsGlobalAdmin: true}
	if !middleware.EffectiveGlobalAdmin(c, admin) || !middleware.SSOAllowsPage(c, 9) {
		t.Error("ohne Seitenbezug: Global Admin und jede Seite erlaubt")
	}
	if middleware.EffectiveGlobalAdmin(c, models.User{}) {
		t.Error("Nicht-Admin ist kein Global Admin")
	}
	c.Set("ssoPageID", uint(3))
	if middleware.EffectiveGlobalAdmin(c, admin) {
		t.Error("Seiten-SSO-Sitzung darf nie Global Admin sein")
	}
	if !middleware.SSOAllowsPage(c, 3) || middleware.SSOAllowsPage(c, 9) {
		t.Error("Seiten-SSO-Sitzung nur für die eigene Seite")
	}
}

// RequirePermission/RequirePageRole and RequireGlobalAdmin with
// a real page SSO token. V has roles on both pages and is
// now Global Admin - the token still applies only to page A.
func TestSSOPageScope_PermissionChecks(t *testing.T) {
	db := testutils.SetupTestDB()
	role := models.Role{Name: "Page Admin", Permissions: "incidents:write"}
	db.Create(&role)
	pageA := models.StatusPage{Name: "A", Slug: "a"}
	pageB := models.StatusPage{Name: "B", Slug: "b"}
	pageC := models.StatusPage{Name: "C", Slug: "c"}
	db.Create(&pageA)
	db.Create(&pageB)
	db.Create(&pageC)
	v := models.User{Email: "v@example.com", IsGlobalAdmin: true}
	db.Create(&v)
	db.Create(&models.UserStatusPageRole{UserID: v.ID, StatusPageID: pageA.ID, RoleID: role.ID})
	db.Create(&models.UserStatusPageRole{UserID: v.ID, StatusPageID: pageB.ID, RoleID: role.ID})
	scopedA, _ := middleware.GeneratePageSSOToken(v, "", "oidc", pageA.ID)
	scopedC, _ := middleware.GeneratePageSSOToken(v, "", "oidc", pageC.ID)
	plain, _ := middleware.GenerateToken(v.ID, v.Email, "", "", true)

	r := testutils.SetupRouter()
	ok := func(c *gin.Context) { c.String(http.StatusOK, "ok") }
	auth := r.Group("", middleware.AuthMiddleware())
	auth.GET("/w/:page_id", middleware.RequirePermission("incidents:write"), ok)
	auth.GET("/r/:page_id", middleware.RequirePageRole(), ok)
	auth.GET("/global", middleware.RequireGlobalAdmin(), ok)

	id := func(p models.StatusPage) string { return strconv.Itoa(int(p.ID)) }
	tests := []struct {
		name, token, path string
		want              int
	}{
		{"eigene Seite, Permission", scopedA, "/w/" + id(pageA), http.StatusOK},
		{"eigene Seite, Rolle", scopedA, "/r/" + id(pageA), http.StatusOK},
		{"fremde Seite trotz Rolle", scopedA, "/w/" + id(pageB), http.StatusForbidden},
		{"fremde Seite, RequirePageRole", scopedA, "/r/" + id(pageB), http.StatusForbidden},
		{"globale Route trotz Global Admin", scopedA, "/global", http.StatusForbidden},
		// Without a role on the SSO page, Global Admin does not help.
		{"SSO-Seite ohne Rolle", scopedC, "/w/" + id(pageC), http.StatusForbidden},
		{"normales Token: fremde Seite", plain, "/w/" + id(pageB), http.StatusOK},
		{"normales Token: globale Route", plain, "/global", http.StatusOK},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s: got %d, want %d (%s)", tc.name, w.Code, tc.want, w.Body.String())
		}
	}
}
