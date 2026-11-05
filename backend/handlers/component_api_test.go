package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Komponenten-API - Tag-Zuordnung ohne Duplikate/ersetzbar, Status
// und Eltern-Komponente beim Anlegen, parent_id bei Create und Update.
func TestComponentAPI(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)
	r.POST("/api/pages/:page_id/components", h.CreateComponentHandler)
	r.PUT("/api/pages/:page_id/components/:id", h.UpdateComponentHandler)

	page := models.StatusPage{Name: "P394", Slug: "p394"}
	other := models.StatusPage{Name: "P394 other", Slug: "p394-other"}
	db.Create(&page)
	db.Create(&other)
	tagA := models.Tag{Name: "A", StatusPageID: &page.ID}
	tagB := models.Tag{Name: "B", StatusPageID: &page.ID}
	db.Create(&tagA)
	db.Create(&tagB)

	base := func(p uint) string { return "/api/pages/" + strconv.Itoa(int(p)) + "/components" }
	call := func(method, url string, payload map[string]interface{}) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(method, url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) models.Component {
		var c models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &c)
		return c
	}
	create := func(payload map[string]interface{}) models.Component {
		w := call("POST", base(page.ID), payload)
		requireStatusCode(t, w, http.StatusCreated)
		return decode(w)
	}
	put := func(c models.Component, payload map[string]interface{}) *httptest.ResponseRecorder {
		if _, ok := payload["status"]; !ok {
			payload["status"] = "operational"
		}
		return call("PUT", base(page.ID)+"/"+strconv.Itoa(int(c.ID)), payload)
	}
	getTags := func(c models.Component) []models.Tag {
		w := call("GET", base(page.ID), nil)
		var tree []models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &tree)
		for _, n := range tree {
			if n.ID == c.ID {
				return n.Tags
			}
		}
		t.Fatalf("component %d not in GET", c.ID)
		return nil
	}
	tagIDs := func(tags []models.Tag) []uint {
		ids := []uint{}
		for _, tg := range tags {
			ids = append(ids, tg.ID)
		}
		return ids
	}
	expectIDs := func(t *testing.T, got []models.Tag, want ...uint) {
		t.Helper()
		ids := tagIDs(got)
		if len(ids) != len(want) {
			t.Fatalf("tags = %v, want %v", ids, want)
		}
		for i := range want {
			if ids[i] != want[i] {
				t.Fatalf("tags = %v, want %v", ids, want)
			}
		}
	}

	t.Run("1 dreimal PUT mit gleichem Tag ergibt genau einen Tag", func(t *testing.T) {
		c := create(map[string]interface{}{"name": "c1"})
		for i := 0; i < 3; i++ {
			w := put(c, map[string]interface{}{"tag_ids": []uint{tagA.ID}})
			requireStatusCode(t, w, http.StatusOK)
			expectIDs(t, decode(w).Tags, tagA.ID)
		}
		expectIDs(t, getTags(c), tagA.ID)
	})

	t.Run("2 PUT ersetzt die Tags", func(t *testing.T) {
		c := create(map[string]interface{}{"name": "c2"})
		put(c, map[string]interface{}{"tag_ids": []uint{tagA.ID, tagB.ID}})
		w := put(c, map[string]interface{}{"tag_ids": []uint{tagB.ID}})
		expectIDs(t, decode(w).Tags, tagB.ID)
		expectIDs(t, getTags(c), tagB.ID)
	})

	t.Run("3 leeres tag_ids entfernt alle, fehlendes lässt unverändert", func(t *testing.T) {
		c := create(map[string]interface{}{"name": "c3", "tag_ids": []uint{tagA.ID}})
		w := put(c, map[string]interface{}{"description": "x"})
		expectIDs(t, decode(w).Tags, tagA.ID)
		w = put(c, map[string]interface{}{"tag_ids": []uint{}})
		requireStatusCode(t, w, http.StatusOK)
		if !bytes.Contains(w.Body.Bytes(), []byte(`"Tags":[]`)) {
			t.Errorf("Tags soll [] sein: %s", w.Body.String())
		}
		expectIDs(t, getTags(c))
		if !bytes.Contains(mustGet(call("GET", base(page.ID), nil)), []byte(`"Tags":[]`)) {
			t.Errorf("GET: Tags soll [] statt null sein")
		}
	})

	t.Run("6 POST übernimmt status", func(t *testing.T) {
		c := create(map[string]interface{}{"name": "s1", "status": "partial_outage"})
		if c.Status != "partial_outage" {
			t.Errorf("status = %q", c.Status)
		}
		var stored models.Component
		db.First(&stored, c.ID)
		if stored.Status != "partial_outage" {
			t.Errorf("gespeichert = %q", stored.Status)
		}
	})

	t.Run("6 POST ohne status ist operational", func(t *testing.T) {
		if c := create(map[string]interface{}{"name": "s2"}); c.Status != "operational" {
			t.Errorf("status = %q", c.Status)
		}
	})

	t.Run("6 POST mit ungültigem Status oder group gibt 400, nichts angelegt", func(t *testing.T) {
		for _, st := range []string{"bogus", "group"} {
			w := call("POST", base(page.ID), map[string]interface{}{"name": "bad-" + st, "status": st})
			requireStatusCode(t, w, http.StatusBadRequest)
			var n int64
			db.Model(&models.Component{}).Where("name = ?", "bad-"+st).Count(&n)
			if n != 0 {
				t.Errorf("%s: Komponente wurde angelegt", st)
			}
		}
	})

	t.Run("7 parent_id bei POST und PUT, 0 macht zur Wurzel, fehlend unverändert", func(t *testing.T) {
		p1 := create(map[string]interface{}{"name": "par1"})
		p2 := create(map[string]interface{}{"name": "par2"})
		child := create(map[string]interface{}{"name": "kid", "parent_id": p1.ID})
		if child.ParentID == nil || *child.ParentID != p1.ID {
			t.Fatalf("POST parent_id = %v", child.ParentID)
		}
		w := put(child, map[string]interface{}{"parent_id": p2.ID})
		requireStatusCode(t, w, http.StatusOK)
		if got := decode(w); got.ParentID == nil || *got.ParentID != p2.ID {
			t.Fatalf("PUT parent_id = %v", got.ParentID)
		}
		w = put(child, map[string]interface{}{"description": "d"})
		if got := decode(w); got.ParentID == nil || *got.ParentID != p2.ID {
			t.Fatalf("fehlendes parent_id änderte Eltern: %v", got.ParentID)
		}
		w = put(child, map[string]interface{}{"parent_id": 0})
		requireStatusCode(t, w, http.StatusOK)
		if got := decode(w); got.ParentID != nil {
			t.Fatalf("parent_id 0 soll Wurzel machen: %v", *got.ParentID)
		}
		var stored models.Component
		db.First(&stored, child.ID)
		if stored.ParentID != nil {
			t.Errorf("gespeichert: %v", *stored.ParentID)
		}
	})

	t.Run("8 parent_id Fehlerfälle", func(t *testing.T) {
		foreign := models.Component{StatusPageID: other.ID, Name: "fremd", Status: "operational"}
		db.Create(&foreign)
		root := create(map[string]interface{}{"name": "root"})
		kid := create(map[string]interface{}{"name": "kid8", "parent_id": root.ID})
		grandkid := create(map[string]interface{}{"name": "gk8", "parent_id": kid.ID})

		requireStatusCode(t, call("POST", base(page.ID), map[string]interface{}{"name": "x1", "parent_id": 999999}), http.StatusBadRequest)
		requireStatusCode(t, call("POST", base(page.ID), map[string]interface{}{"name": "x2", "parent_id": foreign.ID}), http.StatusBadRequest)
		var n int64
		db.Model(&models.Component{}).Where("name IN ?", []string{"x1", "x2"}).Count(&n)
		if n != 0 {
			t.Errorf("trotz 400 angelegt: %d", n)
		}
		requireStatusCode(t, put(kid, map[string]interface{}{"parent_id": 999999}), http.StatusBadRequest)
		requireStatusCode(t, put(kid, map[string]interface{}{"parent_id": foreign.ID}), http.StatusBadRequest)
		requireStatusCode(t, put(kid, map[string]interface{}{"parent_id": kid.ID}), http.StatusBadRequest)
		requireStatusCode(t, put(root, map[string]interface{}{"parent_id": grandkid.ID}), http.StatusBadRequest)
		var stored models.Component
		db.First(&stored, root.ID)
		if stored.ParentID != nil {
			t.Errorf("Zyklus wurde gespeichert")
		}
	})

	t.Run("10 bestehender Zyklus in der DB lässt PUT und POST nicht hängen", func(t *testing.T) {
		a := create(map[string]interface{}{"name": "zyk-a"})
		b := create(map[string]interface{}{"name": "zyk-b", "parent_id": a.ID})
		third := create(map[string]interface{}{"name": "zyk-c"})
		// Zyklus A -> B -> A direkt in der DB (Altlast/Race), vorbei an der API.
		db.Model(&models.Component{}).Where("id = ?", a.ID).Update("parent_id", b.ID)

		withTimeout := func(fn func() *httptest.ResponseRecorder) *httptest.ResponseRecorder {
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- fn() }()
			select {
			case w := <-done:
				return w
			case <-time.After(5 * time.Second):
				t.Fatal("validateParent hängt bei bestehendem Zyklus")
				return nil
			}
		}
		requireStatusCode(t, withTimeout(func() *httptest.ResponseRecorder {
			return put(third, map[string]interface{}{"parent_id": a.ID})
		}), http.StatusBadRequest)
		requireStatusCode(t, withTimeout(func() *httptest.ResponseRecorder {
			return call("POST", base(page.ID), map[string]interface{}{"name": "zyk-d", "parent_id": a.ID})
		}), http.StatusBadRequest)
	})

	t.Run("10 gelöschter Zwischenknoten beendet die Vorfahrenkette mit 200", func(t *testing.T) {
		top := create(map[string]interface{}{"name": "del-top"})
		mid := create(map[string]interface{}{"name": "del-mid", "parent_id": top.ID})
		leaf := create(map[string]interface{}{"name": "del-leaf", "parent_id": mid.ID})
		free := create(map[string]interface{}{"name": "del-free"})
		db.Delete(&models.Component{}, top.ID)
		requireStatusCode(t, put(free, map[string]interface{}{"parent_id": leaf.ID}), http.StatusOK)
	})

	t.Run("9 Statusseite zeigt Kind unter Eltern", func(t *testing.T) {
		parent := create(map[string]interface{}{"name": "tree-parent"})
		kid := create(map[string]interface{}{"name": "tree-kid", "parent_id": parent.ID})
		var tree []models.Component
		_ = json.Unmarshal(mustGet(call("GET", base(page.ID), nil)), &tree)
		for _, n := range tree {
			if n.ID == parent.ID {
				if len(n.Children) != 1 || n.Children[0].ID != kid.ID {
					t.Fatalf("Kinder = %+v", n.Children)
				}
				return
			}
		}
		t.Fatal("Eltern nicht im Baum")
	})
}

func mustGet(w *httptest.ResponseRecorder) []byte { return w.Body.Bytes() }
