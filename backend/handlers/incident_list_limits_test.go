package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Seite mit sehr vielen Vorfällen: Die Listen sind begrenzt, ein anonymer
// Aufruf darf nicht alle Zeilen laden (Speicher- und Antwortgröße).

type incidentListEnv struct {
	db   *gorm.DB
	r    http.Handler
	page models.StatusPage
}

func newIncidentListEnv(t *testing.T) incidentListEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/incidents", middleware.OptionalAuthMiddleware(), h.GetIncidentsHandler)
	r.GET("/api/public/pages/:slug/dashboard", middleware.OptionalAuthMiddleware(), h.GetPublicDashboardHandler)
	page := models.StatusPage{Name: "Viele", Slug: "viele-vorfaelle"}
	require.NoError(t, db.Create(&page).Error)
	return incidentListEnv{db: db, r: r, page: page}
}

// seed legt n Vorfälle an; Titel "v<i>", i=0 ist der älteste. Jeder zehnte ist
// eine Wartung, die Status folgen status(i).
func (e incidentListEnv) seed(t *testing.T, n int, status func(i int) string) {
	t.Helper()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]models.Incident, n)
	for i := range rows {
		started := base.Add(time.Duration(i) * time.Hour)
		rows[i] = models.Incident{
			StatusPageID: e.page.ID, Title: fmt.Sprintf("v%d", i), Message: "Text",
			Status: status(i), StartedAt: &started, IsMaintenance: i%10 == 9,
		}
	}
	require.NoError(t, e.db.CreateInBatches(&rows, 100).Error)
}

func (e incidentListEnv) get(t *testing.T, url string) (int, []string, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	var titles []string
	var obj map[string]any
	if w.Code == http.StatusOK {
		var list []struct{ Title string }
		if json.Unmarshal(w.Body.Bytes(), &list) == nil {
			for _, i := range list {
				titles = append(titles, i.Title)
			}
		} else {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &obj))
		}
	}
	return w.Code, titles, obj
}

func dashboardTitles(t *testing.T, obj map[string]any) []string {
	t.Helper()
	var out []string
	for _, raw := range obj["incidents"].([]any) {
		out = append(out, raw.(map[string]any)["Title"].(string))
	}
	return out
}

func allResolved(int) string { return "resolved" }

func TestGetIncidentsHandler_LimitsResultSize(t *testing.T) {
	e := newIncidentListEnv(t)
	e.seed(t, 600, allResolved)
	base := "/api/pages/" + strconv.Itoa(int(e.page.ID)) + "/incidents"

	t.Run("Standard: die neuesten 50 Normalvorfälle", func(t *testing.T) {
		code, titles, _ := e.get(t, base)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, titles, 50)
		assert.Equal(t, "v598", titles[0], "neueste zuerst (v599 ist eine Wartung)")
		assert.Equal(t, "v544", titles[49], "Wartungen (jeder zehnte) zählen nicht mit")
	})
	t.Run("Obergrenze 200, auch bei größerem Wunsch", func(t *testing.T) {
		for _, limit := range []string{"200", "201", "100000"} {
			code, titles, _ := e.get(t, base+"?limit="+limit)
			require.Equal(t, http.StatusOK, code)
			assert.Len(t, titles, 200, "limit=%s", limit)
		}
	})
	t.Run("Gegenprobe: kleines limit und offset blättern lückenlos bis zum ältesten", func(t *testing.T) {
		_, first, _ := e.get(t, base+"?limit=3")
		assert.Equal(t, []string{"v598", "v597", "v596"}, first)
		_, next, _ := e.get(t, base+"?limit=3&offset=3")
		assert.Equal(t, []string{"v595", "v594", "v593"}, next)
		// 540 Normalvorfälle: der älteste (v0) liegt bei offset 539
		_, last, _ := e.get(t, base+"?limit=50&offset=539")
		assert.Equal(t, []string{"v0"}, last)
		_, none, _ := e.get(t, base+"?limit=50&offset=540")
		assert.Empty(t, none)
	})
	t.Run("Wartungen und Status-Filter", func(t *testing.T) {
		_, maint, _ := e.get(t, base+"?is_maintenance=true&limit=2")
		assert.Equal(t, []string{"v599", "v589"}, maint)
		_, anyKind, _ := e.get(t, base+"?is_maintenance=any&limit=2")
		assert.Equal(t, []string{"v599", "v598"}, anyKind)
	})
	t.Run("ungültige Parameter ergeben 400", func(t *testing.T) {
		for _, q := range []string{"limit=0", "limit=-1", "limit=abc", "offset=-1", "offset=x", "offset=99999999", "state=kaputt"} {
			code, _, _ := e.get(t, base+"?"+q)
			assert.Equal(t, http.StatusBadRequest, code, q)
		}
	})
}

func TestGetIncidentsHandler_StateFilterAndNewIncidents(t *testing.T) {
	e := newIncidentListEnv(t)
	// 300 behobene, dazu zuletzt ein offener Vorfall und eine geplante Wartung
	e.seed(t, 300, func(i int) string {
		if i == 295 {
			return "investigating"
		}
		if i == 299 {
			return "scheduled"
		}
		return "resolved"
	})
	base := "/api/pages/" + strconv.Itoa(int(e.page.ID)) + "/incidents?is_maintenance=any"

	_, open, _ := e.get(t, base+"&state=open")
	assert.Equal(t, []string{"v299", "v295"}, open)
	_, resolved, _ := e.get(t, base+"&state=resolved&limit=200")
	assert.Len(t, resolved, 200)
	assert.NotContains(t, resolved, "v295")

	// Gegenprobe: Ein neu angelegter Vorfall ist sofort ganz vorn.
	started := time.Now()
	require.NoError(t, e.db.Create(&models.Incident{StatusPageID: e.page.ID, Title: "neu", Message: "x", Status: "investigating", StartedAt: &started}).Error)
	_, all, _ := e.get(t, base+"&limit=1")
	assert.Equal(t, []string{"neu"}, all)
}

func TestPublicDashboard_LimitsIncidents(t *testing.T) {
	e := newIncidentListEnv(t)
	// 600 Vorfälle: v0 bis v599; drei alte sind noch offen, der Rest behoben.
	open := map[int]bool{3: true, 7: true, 11: true}
	e.seed(t, 600, func(i int) string {
		if open[i] {
			return "monitoring"
		}
		return "resolved"
	})
	url := "/api/public/pages/" + e.page.Slug + "/dashboard"

	t.Run("Standard: alle offenen plus die neuesten 50 behobenen", func(t *testing.T) {
		code, _, obj := e.get(t, url)
		require.Equal(t, http.StatusOK, code)
		titles := dashboardTitles(t, obj)
		assert.Len(t, titles, 53)
		assert.Subset(t, titles, []string{"v3", "v7", "v11"}, "alte offene Vorfälle fehlen nie")
		assert.Contains(t, titles, "v599")
		assert.Contains(t, titles, "v550")
		assert.NotContains(t, titles, "v549")
		assert.Equal(t, true, obj["incidents_more"])
	})
	t.Run("Obergrenze 200 behobene", func(t *testing.T) {
		for _, limit := range []string{"200", "5000"} {
			_, _, obj := e.get(t, url+"?incident_limit="+limit)
			assert.Len(t, dashboardTitles(t, obj), 203, "incident_limit=%s", limit)
		}
	})
	t.Run("ungültiges incident_limit ergibt 400", func(t *testing.T) {
		for _, v := range []string{"0", "-3", "abc"} {
			code, _, _ := e.get(t, url+"?incident_limit="+v)
			assert.Equal(t, http.StatusBadRequest, code, v)
		}
	})
	t.Run("Gegenprobe: ein neuer Vorfall erscheint, kleine Seiten bleiben vollständig", func(t *testing.T) {
		started := time.Now()
		require.NoError(t, e.db.Create(&models.Incident{StatusPageID: e.page.ID, Title: "neu", Message: "x", Status: "investigating", StartedAt: &started}).Error)
		_, _, obj := e.get(t, url)
		assert.Contains(t, dashboardTitles(t, obj), "neu")

		small := incidentListEnv{db: e.db, r: e.r, page: models.StatusPage{Name: "Klein", Slug: "klein"}}
		require.NoError(t, e.db.Create(&small.page).Error)
		small.seed(t, 12, allResolved)
		_, _, obj = small.get(t, "/api/public/pages/klein/dashboard")
		assert.Len(t, dashboardTitles(t, obj), 12)
		assert.Equal(t, false, obj["incidents_more"])
	})
}

// Der größte erlaubte offset ist 100000; ein Wert darüber ist ungültig.
func TestGetIncidentsHandler_OffsetBoundary(t *testing.T) {
	e := newIncidentListEnv(t)
	base := "/api/pages/" + strconv.Itoa(int(e.page.ID)) + "/incidents"
	code, titles, _ := e.get(t, base+"?offset=100000")
	assert.Equal(t, http.StatusOK, code)
	assert.Empty(t, titles)
	code, _, _ = e.get(t, base+"?offset=100001")
	assert.Equal(t, http.StatusBadRequest, code)
}

// Haben viele Vorfälle denselben Zeitpunkt (Massenimport), muss die Reihenfolge
// trotzdem eindeutig sein, sonst gehen beim Blättern Zeilen doppelt oder gar
// nicht durch.
func TestGetIncidentsHandler_PagingIsStableForEqualTimestamps(t *testing.T) {
	e := newIncidentListEnv(t)
	started := time.Date(2025, 5, 1, 12, 0, 0, 0, time.UTC)
	const n = 25
	rows := make([]models.Incident, n)
	for i := range rows {
		rows[i] = models.Incident{StatusPageID: e.page.ID, Title: fmt.Sprintf("gleich%02d", i), Message: "Text", Status: "resolved", StartedAt: &started}
	}
	require.NoError(t, e.db.CreateInBatches(&rows, 100).Error)
	base := "/api/pages/" + strconv.Itoa(int(e.page.ID)) + "/incidents"

	var all []string
	for offset := 0; offset < n; offset += 2 {
		code, titles, _ := e.get(t, base+"?limit=2&offset="+strconv.Itoa(offset))
		require.Equal(t, http.StatusOK, code)
		all = append(all, titles...)
	}
	require.Len(t, all, n)
	assert.Len(t, uniqueStrings(all), n, "keine Zeile doppelt oder ausgelassen")
	// Bei gleichem Zeitpunkt entscheidet die ID absteigend (neueste zuerst).
	assert.Equal(t, "gleich24", all[0])
	assert.Equal(t, "gleich00", all[n-1])
}

func uniqueStrings(in []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}

// Auch Seiten mit sehr vielen offenen Vorfällen liefern höchstens 200 davon;
// die behobenen kommen zusätzlich, begrenzt durch incident_limit.
func TestPublicDashboard_OpenIncidentsAreCapped(t *testing.T) {
	e := newIncidentListEnv(t)
	e.seed(t, 260, func(i int) string {
		if i < 250 {
			return "investigating"
		}
		return "resolved"
	})
	_, _, obj := e.get(t, "/api/public/pages/"+e.page.Slug+"/dashboard")
	titles := dashboardTitles(t, obj)
	assert.Len(t, titles, 200+10, "200 offene (die neuesten) plus alle 10 behobenen")
	assert.Contains(t, titles, "v249")
	assert.NotContains(t, titles, "v49", "der 201. offene Vorfall fehlt")
}

// Jede Abfrage der Vorfälle im Dashboard und in der Liste trägt ein LIMIT: Die
// Begrenzung muss in der Datenbank greifen, nicht erst nach dem Laden aller Zeilen.
func TestIncidentReads_QueriesCarryALimit(t *testing.T) {
	e := newIncidentListEnv(t)
	e.seed(t, 30, allResolved)
	var queries []string
	require.NoError(t, e.db.Callback().Query().After("gorm:query").Register("test:capture_incident_sql", func(tx *gorm.DB) {
		if tx.Statement.Table == "incidents" && tx.Statement.SQL.Len() > 0 {
			queries = append(queries, tx.Statement.SQL.String())
		}
	}))
	t.Cleanup(func() { _ = e.db.Callback().Query().Remove("test:capture_incident_sql") })

	for _, url := range []string{
		"/api/public/pages/" + e.page.Slug + "/dashboard",
		"/api/pages/" + strconv.Itoa(int(e.page.ID)) + "/incidents",
	} {
		queries = nil
		code, _, _ := e.get(t, url)
		require.Equal(t, http.StatusOK, code, url)
		require.NotEmpty(t, queries, url)
		for _, q := range queries {
			assert.Contains(t, strings.ToUpper(q), "LIMIT", "%s: Abfrage ohne LIMIT: %s", url, q)
		}
	}
}

// incidents_more ist nur wahr, wenn es tatsächlich mehr behobene gibt als geliefert
// werden: genau incident_limit behobene Vorfälle sind noch "alles".
func TestPublicDashboard_MoreFlagAtExactLimit(t *testing.T) {
	e := newIncidentListEnv(t)
	e.seed(t, 50, allResolved)
	url := "/api/public/pages/" + e.page.Slug + "/dashboard"
	_, _, obj := e.get(t, url)
	assert.Len(t, dashboardTitles(t, obj), 50)
	assert.Equal(t, false, obj["incidents_more"], "genau 50 behobene: nichts nachzuladen")

	started := time.Now().Add(-1000 * time.Hour)
	require.NoError(t, e.db.Create(&models.Incident{StatusPageID: e.page.ID, Title: "alt", Message: "x", Status: "resolved", StartedAt: &started}).Error)
	_, _, obj = e.get(t, url)
	assert.Len(t, dashboardTitles(t, obj), 50)
	assert.Equal(t, true, obj["incidents_more"], "51 behobene: der älteste fehlt")
}
