package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// myPageEntry mirrors the JSON shape returned by GetMyPagesHandler for a
// single status page, including the page-scoped Permissions string.
type myPageEntry struct {
	models.StatusPage
	Permissions string
}

// decodeMyPages parses the /api/me/pages response body, failing the test on
// malformed JSON instead of requiring every call site to repeat the check.
func decodeMyPages(t *testing.T, w *httptest.ResponseRecorder) []myPageEntry {
	t.Helper()
	var responsePages []myPageEntry
	if err := json.Unmarshal(w.Body.Bytes(), &responsePages); err != nil {
		t.Fatalf("Failed to parse response body: %v", err)
	}
	return responsePages
}

func TestGetMyPagesHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)

	// Create users
	globalAdmin := models.User{Email: "global@test.com", IsGlobalAdmin: true}
	db.Create(&globalAdmin)

	normalUser := models.User{Email: "normal@test.com", IsGlobalAdmin: false}
	db.Create(&normalUser)

	noRightsUser := models.User{Email: "norights@test.com", IsGlobalAdmin: false}
	db.Create(&noRightsUser)

	// Create status pages
	page1 := models.StatusPage{Name: "Page 1", Slug: "page-1"}
	db.Create(&page1)
	page2 := models.StatusPage{Name: "Page 2", Slug: "page-2"}
	db.Create(&page2)

	// Create roles
	role := models.Role{Name: "Editor", Permissions: "incidents:write"}
	db.Create(&role)

	// Assign normal user to page 1
	db.Create(&models.UserStatusPageRole{UserID: normalUser.ID, StatusPageID: page1.ID, RoleID: role.ID})

	r := testutils.SetupRouter()

	r.Use(mockUserIDMiddleware)

	r.GET("/api/me/pages", h.GetMyPagesHandler)

	tests := []struct {
		name           string
		mockUserID     string
		expectedStatus int
		expectedCount  int
	}{
		{
			name:           "Global Admin gets all pages",
			mockUserID:     strconv.Itoa(int(globalAdmin.ID)),
			expectedStatus: http.StatusOK,
			expectedCount:  2,
		},
		{
			name:           "Normal user gets only assigned pages",
			mockUserID:     strconv.Itoa(int(normalUser.ID)),
			expectedStatus: http.StatusOK,
			expectedCount:  1, // Only Page 1
		},
		{
			name:           "User with no assigned pages gets empty list",
			mockUserID:     strconv.Itoa(int(noRightsUser.ID)),
			expectedStatus: http.StatusOK,
			expectedCount:  0,
		},
		{
			name:           "Unauthenticated returns unauthorized",
			mockUserID:     "",
			expectedStatus: http.StatusUnauthorized,
			expectedCount:  -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/me/pages", nil)
			if tc.mockUserID != "" {
				req.Header.Set("X-Mock-User-ID", tc.mockUserID)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			requireStatusCode(t, w, tc.expectedStatus)

			if tc.expectedStatus != http.StatusOK {
				return
			}
			responsePages := decodeMyPages(t, w)
			if len(responsePages) != tc.expectedCount {
				t.Errorf("Expected %d pages, got %d", tc.expectedCount, len(responsePages))
			}
		})
	}

	// Regressionstest: seitenbezogene Permissions müssen aus der zugewiesenen
	// UserStatusPageRole/Role stammen, nicht aus der globalen Rolle des Nutzers
	// (Frontend hasPerm() konnte sonst nie erkennen, dass z.B. ein "Viewer"
	// global auf einer bestimmten Seite als "Editor" eingesetzt wurde).
	t.Run("Seitenbezogene Permissions werden mitgeliefert", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me/pages", nil)
		req.Header.Set("X-Mock-User-ID", strconv.Itoa(int(normalUser.ID)))

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		responsePages := decodeMyPages(t, w)
		if len(responsePages) != 1 {
			t.Fatalf("Expected 1 page, got %d", len(responsePages))
		}
		if responsePages[0].Permissions != "incidents:write" {
			t.Errorf("Expected Permissions 'incidents:write', got %q", responsePages[0].Permissions)
		}
	})
}
