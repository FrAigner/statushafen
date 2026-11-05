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

// TestGenerateTokenFull_IncludesPreferredLanguage deckt einen Bug ab, bei dem
// SSO-Logins die serverseitig gespeicherte Sprachpräferenz (User.PreferredLanguage)
// nie an das Frontend weitergeben konnten: /api/auth/sso-exchange liefert nur
// {"token": ...} zurück, das Frontend liest preferred_language ausschließlich
// per Client-seitigem JWT-Decode aus dem Token selbst (anders als beim
// Passwort-Login, wo der Login-Response-Body das Feld separat enthält).
// GenerateTokenFull muss die Präferenz daher als eigenen Claim einbetten.
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

	// Zwei Routen wie im echten Router: global (kein page_id im Pfad) und
	// seitenbezogen (:page_id im Pfad) – RequirePermission liest die Seiten-ID
	// ausschließlich aus c.Param("page_id"), seit die App von einem
	// X-Status-Page-ID-Header auf RESTful :page_id-URL-Parameter umgestellt wurde.
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
			// Kein page_id im Pfad ist eine fehlerhafte Anfrage (400), nicht eine
			// fehlende Berechtigung (403) – die Middleware kann für einen
			// Nicht-Global-Admin ohne page_id gar nicht erst prüfen, ob eine
			// Berechtigung vorliegt.
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

// TestUserHasAccessToAnyTag deckt die geforderten Edge-Cases für
// den Mehrfach-Tag-Nachfolger von UserHasAccessToCountry ab.
func TestUserHasAccessToAnyTag(t *testing.T) {
	pageID := uint(1)
	otherPageID := uint(2)

	newTag := func(id uint, name string, statusPageID *uint) models.Tag {
		return models.Tag{Model: gorm.Model{ID: id}, Name: name, StatusPageID: statusPageID}
	}

	// Der reservierte globale Tag: Name "global" (hier bewusst gemischte
	// Groß-/Kleinschreibung, um Case-Insensitivität mitzutesten) UND
	// StatusPageID == nil.
	globalTag := newTag(1, "Global", nil)

	euTag := newTag(10, "eu", &pageID)
	usTag := newTag(11, "us", &pageID)
	deTag := newTag(12, "de", &otherPageID)

	// Seitenbezogener Tag, der zufällig ebenfalls "global" heißt, aber NICHT
	// der reservierte globale Tag ist (StatusPageID != nil). Muss NICHT als
	// Wildcard wirken.
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
			// Entspricht der alten Schleife: ein "global"-Eintrag beim User
			// (cTrimmed == "global") gewährt Zugriff unabhängig vom
			// Ressourcen-Tag.
			name:         "User mit globalem Tag gegen Ressource mit spezifischem Tag -> muss durchgehen",
			userTags:     []models.Tag{globalTag},
			resourceTags: []models.Tag{deTag},
			want:         true,
		},
		{
			// Entspricht dem alten frühen Return `if targetCountry == "global": return true`:
			// die Ressourcen-Seite ist "global" und die Funktion gewährt Zugriff
			// sofort, unabhängig vom Inhalt der User-Seite. Im alten Code führen
			// beide Wildcard-Fälle (targetCountry == "global" UND cTrimmed ==
			// "global") zum selben Ergebnis (true) - die Wildcard ist im Effekt
			// symmetrisch, auch wenn sie im alten Code über zwei verschiedene
			// Codepfade (früher Return vs. Schleifentreffer) läuft.
			name:         "Ressource mit globalem Tag gegen User mit nur spezifischem Tag -> muss durchgehen",
			userTags:     []models.Tag{deTag},
			resourceTags: []models.Tag{globalTag},
			want:         true,
		},
		{
			// Seitenbezogener Tag, der nur zufällig "global" heißt, ist NICHT der
			// reservierte globale Tag (StatusPageID != nil) und darf daher keine
			// Wildcard-Wirkung entfalten.
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
