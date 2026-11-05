package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/require"
)

// TestSystemStatusPage_PubliclyReachableAndIsolated
// "Verifikation" criteria via the same public handlers real customer
// tenants use (no special-case bypass code): the system status page must be
// reachable by its reserved slug and must only ever show its own
// components, never those of another tenant.
func TestSystemStatusPage_PubliclyReachableAndIsolated(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/pages/slug/:slug", h.GetStatusPageBySlugHandler)
	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)

	// Seed the system status page exactly like InitDB() does on startup.
	database.SeedSystemStatusPage(db)

	// A regular customer tenant with its own component, to prove isolation.
	otherPage := models.StatusPage{Name: "Other Corp", Slug: "othercorp", IsPublicIndexed: true}
	require.NoError(t, db.Create(&otherPage).Error)
	otherComponent := models.Component{StatusPageID: otherPage.ID, Name: "Other Component", Status: "operational"}
	require.NoError(t, db.Create(&otherComponent).Error)

	var page models.StatusPage

	t.Run("reachable via its reserved slug like any other tenant's public page", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/slug/"+database.SystemStatusPageSlug, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusOK)

		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		if page.Slug != database.SystemStatusPageSlug {
			t.Errorf("Expected slug %q, got %q", database.SystemStatusPageSlug, page.Slug)
		}
		// die öffentliche Antwort enthält die
		// Sichtbarkeits-Flags nicht mehr (öffentliches DTO), daher direkt
		// am gespeicherten Datensatz prüfen.
		var stored models.StatusPage
		require.NoError(t, db.First(&stored, page.ID).Error)
		if !stored.IsPublicIndexed || stored.IsPrivate {
			t.Errorf("Expected system status page to be public indexed and not private, got IsPublicIndexed=%v IsPrivate=%v", stored.IsPublicIndexed, stored.IsPrivate)
		}
	})

	// Its components endpoint returns exactly the capability groups
	// (database.SystemCapabilities) with the 4 standard components inside the
	// platform group, and nothing belonging to another tenant (normal tenant isolation via
	// status_page_id, no special-case code).
	t.Run("components endpoint only returns its own standard components", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/"+strconv.FormatUint(uint64(page.ID), 10)+"/components", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusOK)

		var components []models.Component
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &components))

		expectedNames := map[string]bool{}
		for _, g := range database.SystemCapabilities {
			expectedNames[g.Name] = false
		}
		if len(components) != len(expectedNames) {
			t.Fatalf("Expected %d components, got %d: %+v", len(expectedNames), len(components), components)
		}
		for _, comp := range components {
			assertSystemComponentIsolated(t, comp, page.ID, otherComponent.Name, expectedNames)
		}
		for name, seen := range expectedNames {
			if !seen {
				t.Errorf("Expected component %q not found on system status page", name)
			}
		}
		var platform []string
		for _, comp := range components {
			if comp.Name == "Plattform" {
				for _, child := range comp.Children {
					platform = append(platform, child.Name)
				}
			}
		}
		require.ElementsMatch(t, []string{"API", "Datenbank", "Frontend", "E-Mail-Versand", "Datenbank-Backup"}, platform)
	})
}

// assertSystemComponentIsolated checks a single component returned by the
// system status page's components endpoint against the tenant-isolation and
// seeding expectations, marking it as seen in expectedNames.
func assertSystemComponentIsolated(t *testing.T, comp models.Component, wantPageID uint, otherComponentName string, expectedNames map[string]bool) {
	t.Helper()
	if comp.StatusPageID != wantPageID {
		t.Errorf("Component %q belongs to status page %d, expected %d (tenant isolation violated)", comp.Name, comp.StatusPageID, wantPageID)
	}
	if comp.Name == otherComponentName {
		t.Errorf("System status page leaked another tenant's component: %q", comp.Name)
	}
	if _, ok := expectedNames[comp.Name]; !ok {
		t.Errorf("Unexpected component name %q on system status page", comp.Name)
	} else {
		expectedNames[comp.Name] = true
	}
	if comp.Status != "operational" {
		t.Errorf("Expected component %q to default to status 'operational', got %q", comp.Name, comp.Status)
	}
}
