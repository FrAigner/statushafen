package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Die Notice-Admin-Handler hatten nur den Normalfall getestet.
// Ergänzt: ungültige/fehlende Parameter, Validierung, Mandantentrennung,
// Standardwerte und Datenbankfehler. NotifySubscribers bleibt bewusst aus,
// damit kein Versand-Goroutine in den Tests läuft.
func TestNoticeAdminHandlers_Fehlerpfade(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/notices/all", h.GetNoticesHandler)
	r.POST("/api/pages/:page_id/notices", h.CreateNoticeHandler)
	r.PUT("/api/pages/:page_id/notices/:id", h.UpdateNoticeHandler)
	r.DELETE("/api/pages/:page_id/notices/:id", h.DeleteNoticeHandler)

	page := models.StatusPage{Name: "Notice Err", Slug: "notice-err"}
	other := models.StatusPage{Name: "Notice Err Other", Slug: "notice-err-other"}
	db.Create(&page)
	db.Create(&other)
	pid := strconv.Itoa(int(page.ID))
	oid := strconv.Itoa(int(other.ID))

	newNotice := func(title string) models.GeneralNotice {
		n := models.GeneralNotice{StatusPageID: page.ID, Title: title, Message: "Text", Level: "info", IsActive: true}
		db.Create(&n)
		return n
	}
	idOf := func(n models.GeneralNotice) string { return strconv.Itoa(int(n.ID)) }
	noticeCount := func(id uint) int64 {
		var n int64
		db.Model(&models.GeneralNotice{}).Where("id = ?", id).Count(&n)
		return n
	}
	auditCount := func(action string) int64 {
		var n int64
		db.Model(&models.AuditLog{}).Where("action = ?", action).Count(&n)
		return n
	}
	failOn := func(t *testing.T, name, table string) {
		cb := func(tx *gorm.DB) {
			if tx.Statement.Table == table {
				_ = tx.AddError(errors.New("simulierter Fehler"))
			}
		}
		regs := []struct {
			remove func(string) error
			name   string
			err    error
		}{
			{func(n string) error { return db.Callback().Query().Remove(n) }, name + "_q", db.Callback().Query().Before("gorm:query").Register(name+"_q", cb)},
			{func(n string) error { return db.Callback().Update().Remove(n) }, name + "_u", db.Callback().Update().Before("gorm:update").Register(name+"_u", cb)},
			{func(n string) error { return db.Callback().Delete().Remove(n) }, name + "_d", db.Callback().Delete().Before("gorm:delete").Register(name+"_d", cb)},
			{func(n string) error { return db.Callback().Create().Remove(n) }, name + "_c", db.Callback().Create().Before("gorm:create").Register(name+"_c", cb)},
		}
		for _, reg := range regs {
			if reg.err != nil {
				t.Fatalf("Callback nicht registriert: %v", reg.err)
			}
		}
		t.Cleanup(func() {
			for _, reg := range regs {
				_ = reg.remove(reg.name)
			}
		})
	}
	// failOnWrites lässt Lesezugriffe durch und scheitert nur bei Schreibzugriffen.
	failOnWrites := func(t *testing.T, name, table string) {
		cb := func(tx *gorm.DB) {
			if tx.Statement.Table == table {
				_ = tx.AddError(errors.New("simulierter Fehler"))
			}
		}
		if err := db.Callback().Update().Before("gorm:update").Register(name+"_u", cb); err != nil {
			t.Fatalf("Callback nicht registriert: %v", err)
		}
		if err := db.Callback().Delete().Before("gorm:delete").Register(name+"_d", cb); err != nil {
			t.Fatalf("Callback nicht registriert: %v", err)
		}
		if err := db.Callback().Create().Before("gorm:create").Register(name+"_c", cb); err != nil {
			t.Fatalf("Callback nicht registriert: %v", err)
		}
		t.Cleanup(func() {
			_ = db.Callback().Update().Remove(name + "_u")
			_ = db.Callback().Delete().Remove(name + "_d")
			_ = db.Callback().Create().Remove(name + "_c")
		})
	}

	t.Run("Fehlender page_id-Parameter gibt 400", func(t *testing.T) {
		handlersByName := map[string]gin.HandlerFunc{
			"Get": h.GetNoticesHandler, "Create": h.CreateNoticeHandler,
			"Update": h.UpdateNoticeHandler, "Delete": h.DeleteNoticeHandler,
		}
		for name, fn := range handlersByName {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request, _ = http.NewRequest("GET", "/", nil)
			fn(c)
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s: erwartet 400, erhalten %d", name, w.Code)
			}
			requireBodyContains(t, w, "page_id parameter missing")
		}
	})

	t.Run("Ungültige page_id gibt 400", func(t *testing.T) {
		for _, tc := range []struct{ method, path string }{
			{"GET", "/api/pages/abc/notices/all"},
			{"POST", "/api/pages/abc/notices"},
			{"PUT", "/api/pages/abc/notices/1"},
			{"DELETE", "/api/pages/abc/notices/1"},
		} {
			w := doNoticeRequest(r, tc.method, tc.path, []byte(`{"Title":"t","Message":"m"}`))
			requireStatusCode(t, w, http.StatusBadRequest)
			requireBodyContains(t, w, "Invalid page_id")
		}
	})

	t.Run("Ungültige Notice-ID gibt 400", func(t *testing.T) {
		w := doNoticeRequest(r, "PUT", "/api/pages/"+pid+"/notices/abc", []byte(`{"Title":"t","Message":"m"}`))
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Invalid notice ID")
		w = doNoticeRequest(r, "DELETE", "/api/pages/"+pid+"/notices/abc", nil)
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Invalid notice ID")
	})

	t.Run("Get: Datenbankfehler gibt 500", func(t *testing.T) {
		failOn(t, "notice_get", "general_notices")
		w := doNoticeRequest(r, "GET", "/api/pages/"+pid+"/notices/all", nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		requireBodyContains(t, w, "Failed to fetch notices")
	})

	t.Run("Get: Seite ohne Notices liefert leere Liste und keine fremden", func(t *testing.T) {
		db.Create(&models.GeneralNotice{StatusPageID: other.ID, Title: "Fremd", Message: "x", Level: "info", IsActive: true})
		w := doNoticeRequest(r, "GET", "/api/pages/"+pid+"/notices/all", nil)
		requireStatusCode(t, w, http.StatusOK)
		if body := w.Body.String(); body != "[]" {
			t.Errorf("erwartet leere Liste, erhalten %s", body)
		}
	})

	t.Run("Create: fehlende Pflichtfelder geben 400", func(t *testing.T) {
		for _, body := range []string{`{"Message":"m"}`, `{"Title":"t"}`, `kein json`} {
			w := doNoticeRequest(r, "POST", "/api/pages/"+pid+"/notices", []byte(body))
			requireStatusCode(t, w, http.StatusBadRequest)
			requireBodyContains(t, w, "Invalid request fields")
		}
	})

	t.Run("Create: Ende vor Start gibt 400 und legt nichts an", func(t *testing.T) {
		w := doNoticeRequest(r, "POST", "/api/pages/"+pid+"/notices",
			[]byte(`{"Title":"Zeit","Message":"m","StartAt":"2030-01-02T00:00:00Z","EndAt":"2030-01-01T00:00:00Z"}`))
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Endzeitpunkt")
		var n int64
		db.Model(&models.GeneralNotice{}).Where("title = ?", "Zeit").Count(&n)
		if n != 0 {
			t.Errorf("Notice darf nicht angelegt werden, gefunden: %d", n)
		}
	})

	t.Run("Create: Ende gleich Start gibt 400", func(t *testing.T) {
		w := doNoticeRequest(r, "POST", "/api/pages/"+pid+"/notices",
			[]byte(`{"Title":"Gleich","Message":"m","StartAt":"2030-01-01T00:00:00Z","EndAt":"2030-01-01T00:00:00Z"}`))
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Create: Standard-Level info, IsActive=false wird übernommen, Audit geschrieben", func(t *testing.T) {
		before := auditCount("notice.created")
		w := doNoticeRequest(r, "POST", "/api/pages/"+pid+"/notices",
			[]byte(`{"Title":"Standard","Message":"m","IsActive":false}`))
		requireStatusCode(t, w, http.StatusCreated)
		var created models.GeneralNotice
		db.Where("title = ?", "Standard").First(&created)
		if created.Level != "info" {
			t.Errorf("erwartet Level=info, erhalten %q", created.Level)
		}
		if created.IsActive {
			t.Errorf("IsActive=false muss übernommen werden")
		}
		if created.StatusPageID != page.ID {
			t.Errorf("Notice gehört zur falschen Seite: %d", created.StatusPageID)
		}
		if got := auditCount("notice.created"); got != before+1 {
			t.Errorf("erwartet einen Audit-Eintrag mehr, vorher %d, nachher %d", before, got)
		}
	})

	t.Run("Create: Datenbankfehler gibt 500", func(t *testing.T) {
		failOnWrites(t, "notice_create", "general_notices")
		w := doNoticeRequest(r, "POST", "/api/pages/"+pid+"/notices", []byte(`{"Title":"Boom","Message":"m"}`))
		requireStatusCode(t, w, http.StatusInternalServerError)
		requireBodyContains(t, w, "Failed to create notice")
	})

	t.Run("Update: unbekannte Notice gibt 404", func(t *testing.T) {
		w := doNoticeRequest(r, "PUT", "/api/pages/"+pid+"/notices/99999", []byte(`{"Title":"t","Message":"m"}`))
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Update: Notice einer fremden Seite gibt 404 und bleibt unverändert", func(t *testing.T) {
		n := newNotice("Mandant")
		w := doNoticeRequest(r, "PUT", "/api/pages/"+oid+"/notices/"+idOf(n), []byte(`{"Title":"Gekapert","Message":"m"}`))
		requireStatusCode(t, w, http.StatusNotFound)
		var reloaded models.GeneralNotice
		db.First(&reloaded, n.ID)
		if reloaded.Title != "Mandant" {
			t.Errorf("Titel darf sich nicht ändern, erhalten %q", reloaded.Title)
		}
	})

	t.Run("Update: ungültige Eingaben geben 400 und ändern nichts", func(t *testing.T) {
		n := newNotice("Unverändert")
		for _, tc := range []struct{ name, body, msg string }{
			{"kein Titel", `{"Message":"m"}`, "Invalid request fields"},
			{"unbekanntes Level", `{"Title":"x","Message":"m","Level":"bogus"}`, "Level muss"},
			{"Ende vor Start", `{"Title":"x","Message":"m","StartAt":"2030-01-02T00:00:00Z","EndAt":"2030-01-01T00:00:00Z"}`, "Endzeitpunkt"},
		} {
			w := doNoticeRequest(r, "PUT", "/api/pages/"+pid+"/notices/"+idOf(n), []byte(tc.body))
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s: erwartet 400, erhalten %d", tc.name, w.Code)
			}
			requireBodyContains(t, w, tc.msg)
		}
		var reloaded models.GeneralNotice
		db.First(&reloaded, n.ID)
		if reloaded.Title != "Unverändert" {
			t.Errorf("Titel darf sich nicht ändern, erhalten %q", reloaded.Title)
		}
	})

	t.Run("Update: ohne Level gilt info, IsActive bleibt ohne Angabe", func(t *testing.T) {
		n := models.GeneralNotice{StatusPageID: page.ID, Title: "Alt", Message: "m", Level: "critical", IsActive: true, NotifySubscribers: false}
		db.Create(&n)
		before := auditCount("notice.updated")
		w := doNoticeRequest(r, "PUT", "/api/pages/"+pid+"/notices/"+idOf(n), []byte(`{"Title":"Neu","Message":"m2"}`))
		requireStatusCode(t, w, http.StatusOK)
		var reloaded models.GeneralNotice
		db.First(&reloaded, n.ID)
		if reloaded.Level != "info" || reloaded.Title != "Neu" || reloaded.Message != "m2" {
			t.Errorf("Felder nicht übernommen: %+v", reloaded)
		}
		if !reloaded.IsActive {
			t.Errorf("IsActive muss ohne Angabe erhalten bleiben")
		}
		if got := auditCount("notice.updated"); got != before+1 {
			t.Errorf("erwartet einen Audit-Eintrag mehr, vorher %d, nachher %d", before, got)
		}
	})

	// Inaktiv, damit beim Update kein Versand startet. Das Feld wird per Update gesetzt,
	// weil GORM beim Create einen false-Zero-Value/Default sonst nicht von true unterscheidet.
	newInactiveNotifying := func(title string) models.GeneralNotice {
		n := models.GeneralNotice{StatusPageID: page.ID, Title: title, Message: "m", Level: "info", IsActive: false}
		db.Create(&n)
		db.Model(&n).Update("notify_subscribers", true)
		return n
	}

	t.Run("Update: NotifySubscribers=false überschreibt true", func(t *testing.T) {
		n := newInactiveNotifying("Mail")
		w := doNoticeRequest(r, "PUT", "/api/pages/"+pid+"/notices/"+idOf(n), []byte(`{"Title":"Mail","Message":"m","IsActive":false,"NotifySubscribers":false}`))
		requireStatusCode(t, w, http.StatusOK)
		var reloaded models.GeneralNotice
		db.First(&reloaded, n.ID)
		if reloaded.NotifySubscribers {
			t.Errorf("NotifySubscribers muss auf false gesetzt werden")
		}
	})

	t.Run("Update: NotifySubscribers bleibt ohne Angabe erhalten", func(t *testing.T) {
		n := newInactiveNotifying("Mail-Erhalt")
		w := doNoticeRequest(r, "PUT", "/api/pages/"+pid+"/notices/"+idOf(n), []byte(`{"Title":"Mail-Erhalt","Message":"m","IsActive":false}`))
		requireStatusCode(t, w, http.StatusOK)
		var reloaded models.GeneralNotice
		db.First(&reloaded, n.ID)
		if !reloaded.NotifySubscribers {
			t.Errorf("NotifySubscribers muss ohne Angabe true bleiben")
		}
	})

	t.Run("Update: Collapsed, StartAt und EndAt werden übernommen und wieder entfernt", func(t *testing.T) {
		n := models.GeneralNotice{StatusPageID: page.ID, Title: "Fenster", Message: "m", Level: "info", IsActive: true}
		db.Create(&n)
		url := "/api/pages/" + pid + "/notices/" + idOf(n)
		start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)

		w := doNoticeRequest(r, "PUT", url, []byte(`{"Title":"Fenster","Message":"m","Collapsed":true,"StartAt":"2030-01-01T00:00:00Z","EndAt":"2030-01-02T00:00:00Z"}`))
		requireStatusCode(t, w, http.StatusOK)
		var reloaded models.GeneralNotice
		db.First(&reloaded, n.ID)
		if !reloaded.Collapsed {
			t.Errorf("Collapsed muss übernommen werden")
		}
		if reloaded.StartAt == nil || !reloaded.StartAt.Equal(start) {
			t.Errorf("StartAt = %v, erwartet %v", reloaded.StartAt, start)
		}
		if reloaded.EndAt == nil || !reloaded.EndAt.Equal(end) {
			t.Errorf("EndAt = %v, erwartet %v", reloaded.EndAt, end)
		}

		w = doNoticeRequest(r, "PUT", url, []byte(`{"Title":"Fenster","Message":"m"}`))
		requireStatusCode(t, w, http.StatusOK)
		var cleared models.GeneralNotice
		db.First(&cleared, n.ID)
		if cleared.Collapsed {
			t.Errorf("Collapsed muss ohne Angabe false werden")
		}
		if cleared.StartAt != nil || cleared.EndAt != nil {
			t.Errorf("Zeitfenster muss ohne Angabe entfernt werden, StartAt=%v EndAt=%v", cleared.StartAt, cleared.EndAt)
		}
	})

	t.Run("Update: Datenbankfehler gibt 500", func(t *testing.T) {
		n := newNotice("Update-Fehler")
		failOnWrites(t, "notice_update", "general_notices")
		w := doNoticeRequest(r, "PUT", "/api/pages/"+pid+"/notices/"+idOf(n), []byte(`{"Title":"Boom","Message":"m"}`))
		requireStatusCode(t, w, http.StatusInternalServerError)
		requireBodyContains(t, w, "Failed to update notice")
	})

	t.Run("Delete: unbekannte Notice gibt 404", func(t *testing.T) {
		w := doNoticeRequest(r, "DELETE", "/api/pages/"+pid+"/notices/99999", nil)
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Delete: Notice einer fremden Seite gibt 404 und bleibt bestehen", func(t *testing.T) {
		n := newNotice("Fremd-Löschen")
		w := doNoticeRequest(r, "DELETE", "/api/pages/"+oid+"/notices/"+idOf(n), nil)
		requireStatusCode(t, w, http.StatusNotFound)
		if noticeCount(n.ID) != 1 {
			t.Errorf("Notice darf nicht gelöscht werden")
		}
	})

	t.Run("Delete: Datenbankfehler gibt 500 und die Notice bleibt", func(t *testing.T) {
		n := newNotice("Delete-Fehler")
		before := auditCount("notice.deleted")
		failOnWrites(t, "notice_delete", "general_notices")
		w := doNoticeRequest(r, "DELETE", "/api/pages/"+pid+"/notices/"+idOf(n), nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		requireBodyContains(t, w, "Failed to delete notice")
		if noticeCount(n.ID) != 1 {
			t.Errorf("Notice darf nicht gelöscht werden")
		}
		if got := auditCount("notice.deleted"); got != before {
			t.Errorf("bei Fehler darf kein Audit-Eintrag entstehen")
		}
	})

	t.Run("Delete: schreibt Audit-Eintrag", func(t *testing.T) {
		n := newNotice("Audit-Löschen")
		before := auditCount("notice.deleted")
		w := doNoticeRequest(r, "DELETE", "/api/pages/"+pid+"/notices/"+idOf(n), nil)
		requireStatusCode(t, w, http.StatusOK)
		if noticeCount(n.ID) != 0 {
			t.Errorf("Notice muss gelöscht sein")
		}
		if got := auditCount("notice.deleted"); got != before+1 {
			t.Errorf("erwartet einen Audit-Eintrag mehr, vorher %d, nachher %d", before, got)
		}
	})
}
