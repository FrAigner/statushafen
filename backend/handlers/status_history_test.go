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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Statushistorie der Komponenten. Jeder Schreibpfad des
// Komponentenstatus erzeugt genau einen Eintrag, keinen bei unverändertem
// Status; die 90-Tage-Verfügbarkeit nutzt die Historie als Intervallquelle.

func histOf(t *testing.T, db *gorm.DB, compID uint) []models.ComponentStatusChange {
	t.Helper()
	var r []models.ComponentStatusChange
	require.NoError(t, db.Where("component_id = ?", compID).Order("id").Find(&r).Error)
	return r
}

func putJSON(r http.Handler, method, url string, payload any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(method, url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestStatusHistory_AdminUndWebhookSchreibenGenauEinenEintrag(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/pages/:page_id/components", h.CreateComponentHandler)
	r.PUT("/api/pages/:page_id/components/:id", h.UpdateComponentHandler)
	r.POST("/api/webhooks/components/:component_id/status", h.ComponentStatusWebhookHandler)

	page := models.StatusPage{Name: "H", Slug: "hist", WebhookTokenHash: models.HashToken("tok")}
	require.NoError(t, db.Create(&page).Error)
	p := strconv.Itoa(int(page.ID))

	// Anlegen mit Anfangsstatus: erster Eintrag (Quelle created, alter Status leer).
	w := putJSON(r, "POST", "/api/pages/"+p+"/components", map[string]any{"name": "API", "status": "partial_outage"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var comp models.Component
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &comp))
	hist := histOf(t, db, comp.ID)
	require.Len(t, hist, 1)
	assert.Equal(t, "", hist[0].OldStatus)
	assert.Equal(t, "partial_outage", hist[0].NewStatus)
	assert.Equal(t, models.StatusSourceCreated, hist[0].Source)

	url := "/api/pages/" + p + "/components/" + strconv.Itoa(int(comp.ID))

	// Statuswechsel per Admin: ein Eintrag, Quelle manual.
	require.Equal(t, http.StatusOK, putJSON(r, "PUT", url, map[string]any{"name": "API", "status": "major_outage"}).Code)
	hist = histOf(t, db, comp.ID)
	require.Len(t, hist, 2)
	assert.Equal(t, "partial_outage", hist[1].OldStatus)
	assert.Equal(t, "major_outage", hist[1].NewStatus)
	assert.Equal(t, models.StatusSourceManual, hist[1].Source)

	// Gleicher Status erneut (und Umbenennen): kein neuer Eintrag.
	require.Equal(t, http.StatusOK, putJSON(r, "PUT", url, map[string]any{"name": "API neu", "status": "major_outage"}).Code)
	assert.Len(t, histOf(t, db, comp.ID), 2)

	// Ungültiger Status: abgelehnt, nichts geschrieben.
	require.Equal(t, http.StatusBadRequest, putJSON(r, "PUT", url, map[string]any{"name": "API", "status": "kaputt"}).Code)
	assert.Len(t, histOf(t, db, comp.ID), 2)

	// Webhook: Quelle webhook, genau ein Eintrag; Wiederholung ohne Wechsel keiner.
	hook := "/api/webhooks/components/" + strconv.Itoa(int(comp.ID)) + "/status"
	post := func(status string) int {
		body, _ := json.Marshal(map[string]string{"status": status})
		req, _ := http.NewRequest("POST", hook, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Token", "tok")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	require.Equal(t, http.StatusOK, post("operational"))
	require.Equal(t, http.StatusOK, post("operational"))
	hist = histOf(t, db, comp.ID)
	require.Len(t, hist, 3)
	assert.Equal(t, models.StatusSourceWebhook, hist[2].Source)
	assert.Equal(t, "major_outage", hist[2].OldStatus)
	assert.Equal(t, "operational", hist[2].NewStatus)

	// Gruppen haben keine Historie, auch nicht beim Anlegen/Umbenennen.
	w = putJSON(r, "POST", "/api/pages/"+p+"/components", map[string]any{"name": "Gruppe", "is_group": true})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var grp models.Component
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &grp))
	require.Equal(t, http.StatusOK, putJSON(r, "PUT", "/api/pages/"+p+"/components/"+strconv.Itoa(int(grp.ID)), map[string]any{"name": "Gruppe 2"}).Code)
	assert.Empty(t, histOf(t, db, grp.ID))
}

// Transaktionssicherheit des Admin-Pfads: scheitert der Historieneintrag,
// bleibt der Status unverändert und die Antwort ist ein Fehler.
func TestStatusHistory_AdminPfadRolltBeiHistorienfehlerZurueck(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.PUT("/api/pages/:page_id/components/:id", h.UpdateComponentHandler)
	page := models.StatusPage{Name: "H", Slug: "hist-tx"}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	require.NoError(t, db.Migrator().DropTable(&models.ComponentStatusChange{}))

	w := putJSON(r, "PUT", "/api/pages/"+strconv.Itoa(int(page.ID))+"/components/"+strconv.Itoa(int(comp.ID)),
		map[string]any{"name": "API", "status": "major_outage"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var cur models.Component
	require.NoError(t, db.First(&cur, comp.ID).Error)
	assert.Equal(t, "operational", cur.Status)
}

func TestStatusHistory_WartungSchreibtEintraegeBeiBeginnUndEnde(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	admin := models.User{Email: "admin-hist@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	r.Use(func(c *gin.Context) { c.Set("userID", admin.ID); c.Next() })
	r.POST("/api/pages/:page_id/incidents", h.CreateIncidentHandler)

	page := models.StatusPage{Name: "W", Slug: "hist-w"}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	grp := models.Component{StatusPageID: page.ID, Name: "G", Status: "group"}
	require.NoError(t, db.Create(&comp).Error)
	require.NoError(t, db.Create(&grp).Error)

	ts := func(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }
	w := putJSON(r, "POST", "/api/pages/"+strconv.Itoa(int(page.ID))+"/incidents", map[string]any{
		"Title": "W", "Message": "M", "IsMaintenance": true, "Status": "in_progress",
		"ScheduledStartAt": ts(-time.Hour), "ScheduledEndAt": ts(time.Hour), "ComponentIDs": []uint{comp.ID, grp.ID},
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	hist := histOf(t, db, comp.ID)
	require.Len(t, hist, 1)
	assert.Equal(t, "under_maintenance", hist[0].NewStatus)
	assert.Equal(t, models.StatusSourceMaintenance, hist[0].Source)
	assert.Empty(t, histOf(t, db, grp.ID), "Gruppen bleiben ohne Historie")
}

func TestStatusHistory_ImportSchreibtAnfangsstatus(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	a := f.extComp(src, "a", "Compute", "operational")
	w, resp := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(a)})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	hist := histOf(t, f.db, resp.Created[0].ComponentID)
	require.Len(t, hist, 1)
	assert.Equal(t, models.StatusSourceCreated, hist[0].Source)
	assert.Equal(t, "operational", hist[0].NewStatus)
}

// ---- Verfügbarkeit ----

func (e *availEnv) change(comp uint, to string, at *time.Time) {
	e.t.Helper()
	require.NoError(e.t, e.db.Create(&models.ComponentStatusChange{ComponentID: comp, NewStatus: to, ChangedAt: *at}).Error)
}

// Die Lücke: ein per Webhook/von Hand gesetzter Ausfall ohne Vorfall
// erscheint in der Verfügbarkeit, mit dem Status der Historie.
func TestAvailability_StatushistorieOhneVorfall(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("hist", false)
	web := e.comp(p.ID, "Web", "operational", nil)
	api := e.comp(p.ID, "API", "operational", nil)
	e.change(web.ID, "major_outage", utc(20, 6))
	e.change(web.ID, "operational", utc(20, 12))

	_, resp := e.get("/api/public/pages/hist/availability?include=components")
	assert.Equal(t, "major_outage", day(t, resp.Days, "2026-10-20").Status)
	for _, c := range resp.Components {
		d := day(t, c.Days, "2026-10-20")
		if c.ID == web.ID {
			assert.Equal(t, int64(6*3600), d.DowntimeSeconds)
			assert.Equal(t, []availImpairment{{Status: "major_outage", Seconds: 6 * 3600}}, d.Impairments)
			assert.Equal(t, "operational", day(t, c.Days, "2026-10-21").Status)
		} else {
			assert.Equal(t, api.ID, c.ID)
			assert.Equal(t, "operational", d.Status, "fremde Komponente bleibt unberührt")
		}
	}
}

// Überlappung mit einem Vorfall: der schlimmste Status zählt, nichts doppelt.
func TestAvailability_StatushistorieUeberlapptVorfall(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("ueber", false)
	web := e.comp(p.ID, "Web", "operational", nil)
	e.incident(p.ID, "resolved", *utc(20, 4), utc(20, 10), web.ID) // degraded 04-10
	e.change(web.ID, "major_outage", utc(20, 8))                   // major 08-12
	e.change(web.ID, "operational", utc(20, 12))

	_, resp := e.get("/api/public/pages/ueber/availability?include=components")
	d := day(t, resp.Components[0].Days, "2026-10-20")
	// 04-08 degraded (4 h x 0,3) + 08-12 major (4 h x 1,0) = 5,2 h.
	assert.Equal(t, int64(5.2*3600), d.DowntimeSeconds)
	assert.Equal(t, "major_outage", d.Status)
}

// Ein vor dem Fenster begonnener Ausfall läuft über den Fensterbeginn hinein
// (letzter Eintrag davor ist der Ausgangsstatus).
func TestAvailability_StatushistorieAusgangsstatusVorDemFenster(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("basis", false)
	web := e.comp(p.ID, "Web", "operational", nil)
	vorher := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	e.change(web.ID, "operational", &vorher)
	spaeter := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	e.change(web.ID, "partial_outage", &spaeter)
	e.change(web.ID, "operational", utc(3, 0)) // 3. Oktober

	_, resp := e.get("/api/public/pages/basis/availability?include=components")
	days := resp.Components[0].Days
	assert.Equal(t, "partial_outage", day(t, days, "2026-08-01").Status, "erster Fenstertag gehört zum Ausfall vom Juli")
	assert.Equal(t, "partial_outage", day(t, days, "2026-10-02").Status)
	assert.Equal(t, "operational", day(t, days, "2026-10-04").Status)
}

// Mandantentrennung: Historie einer fremden Seite fließt nie ein.
func TestAvailability_StatushistorieFremdeSeiteUndVersteckte(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("eigen", false)
	q := e.page("fremd", false)
	own := e.comp(p.ID, "Eigen", "operational", nil)
	foreign := e.comp(q.ID, "Fremd", "operational", nil)
	hidden := e.comp(p.ID, "Versteckt", "operational", nil)
	require.NoError(t, e.db.Model(&hidden).Update("visible", false).Error)
	e.change(foreign.ID, "major_outage", utc(20, 0))
	e.change(hidden.ID, "major_outage", utc(21, 0))

	_, resp := e.get("/api/public/pages/eigen/availability?include=components")
	require.Len(t, resp.Components, 1)
	assert.Equal(t, own.ID, resp.Components[0].ID)
	for _, d := range resp.Days {
		assert.NotEqual(t, "major_outage", d.Status, d.Date)
	}
}

func TestAvailability_StatushistorieLesefehlerIst500(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("kaputt", false)
	e.comp(p.ID, "Web", "operational", nil)
	require.NoError(t, e.db.Migrator().DropTable(&models.ComponentStatusChange{}))
	w, _ := e.get("/api/public/pages/kaputt/availability")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Historienfehler beim Anlegen: kein halb angelegter Zustand (Transaktion), der
// Vorfall einer laufenden Wartung wird trotz Fehlers beim Statussetzen angelegt.
func TestStatusHistory_HistorienfehlerBeimAnlegen(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	admin := models.User{Email: "admin-hist2@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	r.Use(func(c *gin.Context) { c.Set("userID", admin.ID); c.Next() })
	r.POST("/api/pages/:page_id/components", h.CreateComponentHandler)
	r.POST("/api/pages/:page_id/incidents", h.CreateIncidentHandler)
	page := models.StatusPage{Name: "F", Slug: "hist-f"}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	p := strconv.Itoa(int(page.ID))
	require.NoError(t, db.Migrator().DropTable(&models.ComponentStatusChange{}))

	w := putJSON(r, "POST", "/api/pages/"+p+"/components", map[string]any{"name": "Neu"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var n int64
	db.Model(&models.Component{}).Where("name = ?", "Neu").Count(&n)
	assert.Zero(t, n, "Komponente wurde zurückgerollt")

	ts := func(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }
	w = putJSON(r, "POST", "/api/pages/"+p+"/incidents", map[string]any{
		"Title": "W", "Message": "M", "IsMaintenance": true, "Status": "in_progress",
		"ScheduledStartAt": ts(-time.Hour), "ScheduledEndAt": ts(time.Hour), "ComponentIDs": []uint{comp.ID},
	})
	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var cur models.Component
	require.NoError(t, db.First(&cur, comp.ID).Error)
	assert.Equal(t, "operational", cur.Status, "Statuswechsel und Historie gehören zusammen")
}

func TestStatusHistory_ImportMitHistorienfehlerIst500(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.pageAdminTok, "https://status.example.org")
	a := f.extComp(src, "a", "Compute", "operational")
	require.NoError(t, f.db.Migrator().DropTable(&models.ComponentStatusChange{}))
	w, _ := f.doImport(src, f.pageAdminTok, map[string]any{"external_component_ids": ids(a)})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var n int64
	f.db.Model(&models.Component{}).Where("name = ?", "Compute").Count(&n)
	assert.Zero(t, n)
}

// Der Ausgangsstatus ist der Eintrag mit dem neuesten changed_at,
// nicht der mit der höchsten ID (nachträglich eingefügte ältere Zeile).
func TestAvailability_StatushistorieAusgangsstatusNachZeitNichtNachID(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("zeitid", false)
	web := e.comp(p.ID, "Web", "operational", nil)
	neu := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	alt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	e.change(web.ID, "partial_outage", &neu) // kleinere ID, neuere Zeit
	e.change(web.ID, "operational", &alt)    // größere ID, ältere Zeit
	_, resp := e.get("/api/public/pages/zeitid/availability?include=components")
	assert.Equal(t, "partial_outage", day(t, resp.Components[0].Days, "2026-08-01").Status)
}

// Webhook-Flattern (86.400 Wechsel in 3 Tagen) darf den
// öffentlichen Endpunkt nicht lahmlegen (vorher ca. 6,5 s CPU je Komponente).
func TestAvailability_StatushistorieFlatternBleibtSchnell(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("flatter", false)
	web := e.comp(p.ID, "Web", "operational", nil)
	start := e.now.Add(-3 * 24 * time.Hour)
	rows := make([]models.ComponentStatusChange, 0, 86400)
	for i := 0; i < 86400; i++ {
		st := "major_outage"
		if i%2 == 1 {
			st = "operational"
		}
		rows = append(rows, models.ComponentStatusChange{ComponentID: web.ID, NewStatus: st, ChangedAt: start.Add(time.Duration(i) * 3 * time.Second)})
	}
	require.NoError(t, e.db.CreateInBatches(rows, 1000).Error)

	began := time.Now()
	w, resp := e.get("/api/public/pages/flatter/availability?include=components")
	elapsed := time.Since(began)
	require.Equal(t, http.StatusOK, w.Code)
	t.Logf("86.400 Wechsel, Antwortzeit: %v", elapsed)
	assert.Less(t, elapsed, 3*time.Second*raceZeitfaktor)
	require.NotNil(t, resp.Components[0].UptimePercent)
}
