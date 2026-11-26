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
	"gorm.io/gorm"
)

// TestGenerateTokenFull_IncludesPreferredLanguage covers a bug where
// SSO logins could never pass the server-side stored language preference (User.PreferredLanguage)
// on to the frontend: /api/auth/sso-exchange returns only
// {"token": ...}, the frontend reads preferred_language exclusively
// via client-side JWT decoding from the token itself (unlike
// password login, where the login response body contains the field separately).
// GenerateTokenFull must therefore embed the preference as its own claim.
func TestGenerateTokenFull_IncludesPreferredLanguage(t *testing.T) {
	testutils.SetupTestDB()

	user := models.User{
		Email:             "sso@example.com",
		PreferredLanguage: "en",
		Role:              models.Role{Name: "Viewer"},
	}

	tokenString, err := middleware.GenerateTokenFull(user, "incidents:read", "SSO User", "okta")
	if err != nil {
		t.Fatalf("GenerateTokenFull failed: %v", err)
	}

	claims := &middleware.Claims{}
	parsed, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte("test-jwt-secret-not-for-production"), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("failed to parse generated token: %v", err)
	}

	if claims.PreferredLanguage != "en" {
		t.Errorf("Expected PreferredLanguage claim 'en', got %q", claims.PreferredLanguage)
	}
}

func TestRequirePermission(t *testing.T) {
	db := testutils.SetupTestDB()

	// Create roles
	adminRole := models.Role{Name: "Admin", Permissions: "incidents:write,incidents:read"}
	db.Create(&adminRole)
	viewerRole := models.Role{Name: "Viewer", Permissions: "incidents:read"}
	db.Create(&viewerRole)

	// Create users
	globalAdmin := models.User{Email: "global@test.com", IsGlobalAdmin: true}
	db.Create(&globalAdmin)

	normalUser := models.User{Email: "normal@test.com", IsGlobalAdmin: false}
	db.Create(&normalUser)

	noRightsUser := models.User{Email: "norights@test.com", IsGlobalAdmin: false}
	db.Create(&noRightsUser)

	// Create a status page
	page := models.StatusPage{Name: "Test Page", Slug: "test-page"}
	db.Create(&page)

	page2 := models.StatusPage{Name: "Other Page", Slug: "other-page"}
	db.Create(&page2)

	// Assign roles
	db.Create(&models.UserStatusPageRole{UserID: normalUser.ID, StatusPageID: page.ID, RoleID: adminRole.ID})
	db.Create(&models.UserStatusPageRole{UserID: normalUser.ID, StatusPageID: page2.ID, RoleID: viewerRole.ID})

	r := testutils.SetupRouter()

	// Mock auth context setter
	r.Use(func(c *gin.Context) {
		userIDStr := c.GetHeader("X-Mock-User-ID")
		if userIDStr != "" {
			uid, _ := strconv.Atoi(userIDStr)
			c.Set("userID", uint(uid))
		}
		c.Next()
	})

	// Two routes as in the real router: global (no page_id in the path) and
	// page-scoped (:page_id in the path) – RequirePermission reads the page ID
	// exclusively from c.Param("page_id"), since the app was switched from an
	// X-Status-Page-ID header to a RESTful :page_id URL parameter.
	protectedHandler := func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	}
	r.GET("/protected", middleware.RequirePermission("incidents:write"), protectedHandler)
	r.GET("/protected/:page_id", middleware.RequirePermission("incidents:write"), protectedHandler)

	tests := []struct {
		name           string
		mockUserID     string
		statusPageID   string
		expectedStatus int
	}{
		{
			name:           "Global Admin has access without page ID",
			mockUserID:     strconv.Itoa(int(globalAdmin.ID)),
			statusPageID:   "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Global Admin has access with page ID",
			mockUserID:     strconv.Itoa(int(globalAdmin.ID)),
			statusPageID:   strconv.Itoa(int(page.ID)),
			expectedStatus: http.StatusOK,
		},
		{
			// No page_id in the path is a malformed request (400), not a
			// missing permission (403) – for a non-global admin without page_id the middleware
			// cannot even check whether a
			// permission exists.
			name:           "Normal user ohne page_id im Pfad",
			mockUserID:     strconv.Itoa(int(normalUser.ID)),
			statusPageID:   "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Normal user with valid permission on page 1",
			mockUserID:     strconv.Itoa(int(normalUser.ID)),
			statusPageID:   strconv.Itoa(int(page.ID)),
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Normal user with invalid permission (Viewer) on page 2",
			mockUserID:     strconv.Itoa(int(normalUser.ID)),
			statusPageID:   strconv.Itoa(int(page2.ID)),
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "User with no assigned roles at all",
			mockUserID:     strconv.Itoa(int(noRightsUser.ID)),
			statusPageID:   strconv.Itoa(int(page.ID)),
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "Unauthenticated user (no context)",
			mockUserID:     "",
			statusPageID:   strconv.Itoa(int(page.ID)),
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "/protected"
			if tc.statusPageID != "" {
				url += "/" + tc.statusPageID
			}
			req, _ := http.NewRequest("GET", url, nil)
			if tc.mockUserID != "" {
				req.Header.Set("X-Mock-User-ID", tc.mockUserID)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tc.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}

// TestUserHasAccessToAnyTag covers the required edge cases for
// the multi-tag successor of UserHasAccessToCountry.
func TestUserHasAccessToAnyTag(t *testing.T) {
	pageID := uint(1)
	otherPageID := uint(2)

	newTag := func(id uint, name string, statusPageID *uint) models.Tag {
		return models.Tag{Model: gorm.Model{ID: id}, Name: name, StatusPageID: statusPageID}
	}

	// The reserved global tag: name "global" (deliberately mixed
	// upper/lower case here, to also test case insensitivity) AND
	// StatusPageID == nil.
	globalTag := newTag(1, "Global", nil)

	euTag := newTag(10, "eu", &pageID)
	usTag := newTag(11, "us", &pageID)
	deTag := newTag(12, "de", &otherPageID)

	// A page-scoped tag that also happens to be called "global" but is NOT
	// the reserved global tag (StatusPageID != nil). Must NOT act as a
	// wildcard.
	scopedTagNamedGlobal := newTag(13, "global", &pageID)

	tests := []struct {
		name         string
		userTags     []models.Tag
		resourceTags []models.Tag
		want         bool
	}{
		{
			name:         "kein Tag auf beiden Seiten -> fail closed",
			userTags:     nil,
			resourceTags: nil,
			want:         false,
		},
		{
			name:         "User ohne Tags gegen Ressource mit Tags -> fail closed",
			userTags:     []models.Tag{},
			resourceTags: []models.Tag{euTag},
			want:         false,
		},
		{
			name:         "Ressource ohne Tags gegen User mit Tags -> fail closed",
			userTags:     []models.Tag{euTag},
			resourceTags: []models.Tag{},
			want:         false,
		},
		{
			name:         "nur globaler Tag auf beiden Seiten",
			userTags:     []models.Tag{globalTag},
			resourceTags: []models.Tag{globalTag},
			want:         true,
		},
		{
			name:         "mehrere überlappende Tags",
			userTags:     []models.Tag{euTag, usTag},
			resourceTags: []models.Tag{deTag, usTag},
			want:         true,
		},
		{
			name:         "Tags ohne Übereinstimmung",
			userTags:     []models.Tag{euTag},
			resourceTags: []models.Tag{usTag, deTag},
			want:         false,
		},
		{
			// Corresponds to the old loop: a "global" entry on the user
			// (cTrimmed == "global") grants access regardless of the
			// resource tag.
			name:         "User mit globalem Tag gegen Ressource mit spezifischem Tag -> muss durchgehen",
			userTags:     []models.Tag{globalTag},
			resourceTags: []models.Tag{deTag},
			want:         true,
		},
		{
			// Corresponds to the old early return `if targetCountry == "global": return true`:
			// the resource side is "global" and the function grants access
			// immediately, regardless of the user side's content. In the old code both
			// wildcard cases (targetCountry == "global" AND cTrimmed ==
			// "global") lead to the same result (true) - the wildcard is effectively
			// symmetric, even though in the old code it runs through two different
			// code paths (early return vs. loop hit).
			name:         "Ressource mit globalem Tag gegen User mit nur spezifischem Tag -> muss durchgehen",
			userTags:     []models.Tag{deTag},
			resourceTags: []models.Tag{globalTag},
			want:         true,
		},
		{
			// A page-scoped tag that merely happens to be called "global" is NOT the
			// reserved global tag (StatusPageID != nil) and therefore must not have any
			// wildcard effect.
			name:         "seitenbezogener Tag namens 'global' wirkt nicht als Wildcard",
			userTags:     []models.Tag{scopedTagNamedGlobal},
			resourceTags: []models.Tag{deTag},
			want:         false,
		},
		{
			name:         "identische TagID auf beiden Seiten reicht aus",
			userTags:     []models.Tag{euTag},
			resourceTags: []models.Tag{euTag},
			want:         true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := middleware.UserHasAccessToAnyTag(tc.userTags, tc.resourceTags)
			if got != tc.want {
				t.Errorf("UserHasAccessToAnyTag() = %v, want %v", got, tc.want)
			}
		})
	}
}
