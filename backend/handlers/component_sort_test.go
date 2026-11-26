package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// A newly created component appeared before siblings with
// maintained order (seed: SortOrder 1, 2, 3 …) because it got the default 0.
// It should go to the end of its level, both at top level and under a
// parent component.
func TestCreateComponent_NewComponentSortsLastAmongSiblings(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)
	r.POST("/api/pages/:page_id/components", h.CreateComponentHandler)

	page := models.StatusPage{Name: "P496", Slug: "p496"}
	other := models.StatusPage{Name: "P496 other", Slug: "p496-other"}
	db.Create(&page)
	db.Create(&other)
	grp := models.Component{StatusPageID: page.ID, Name: "Gruppe", Status: "group", SortOrder: 1}
	db.Create(&grp)
	db.Create(&models.Component{StatusPageID: page.ID, Name: "Zweite", Status: "operational", SortOrder: 2})
	db.Create(&models.Component{StatusPageID: page.ID, ParentID: &grp.ID, Name: "Kind A", Status: "operational", SortOrder: 5})
	// A higher SortOrder on a foreign page must not count.
	db.Create(&models.Component{StatusPageID: other.ID, Name: "Fremd", Status: "operational", SortOrder: 50})

	base := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/components"
	post := func(payload map[string]interface{}) models.Component {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", base, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusCreated)
		var c models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &c)
		return c
	}

	root := post(map[string]interface{}{"name": "Neu oben"})
	if root.SortOrder != 3 {
		t.Fatalf("SortOrder oberste Ebene = %d, erwartet 3", root.SortOrder)
	}
	kid := post(map[string]interface{}{"name": "Kind B", "parent_id": grp.ID})
	if kid.SortOrder != 6 {
		t.Fatalf("SortOrder unter Gruppe = %d, erwartet 6", kid.SortOrder)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", base, nil)
	r.ServeHTTP(w, req)
	var tree []models.Component
	_ = json.Unmarshal(w.Body.Bytes(), &tree)
	names := []string{}
	for _, n := range tree {
		names = append(names, n.Name)
		for _, ch := range n.Children {
			names = append(names, "  "+ch.Name)
		}
	}
	want := []string{"Gruppe", "  Kind A", "  Kind B", "Zweite", "Neu oben"}
	if len(names) != len(want) {
		t.Fatalf("Baum = %q, erwartet %q", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("Baum = %q, erwartet %q", names, want)
		}
	}
}

// The changed query (sort_order, id) still reports a
// database error as 500.
func TestGetComponents_DBErrorReturns500(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)

	page := models.StatusPage{Name: "P496c", Slug: "p496c"}
	db.Create(&page)
	if err := db.Migrator().DropTable(&models.Component{}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/pages/"+strconv.Itoa(int(page.ID))+"/components", nil)
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusInternalServerError)
}
