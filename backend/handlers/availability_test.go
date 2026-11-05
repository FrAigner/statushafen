package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 90-Tage-Verfügbarkeit.

type availResp struct {
	From          string   `json:"from"`
	To            string   `json:"to"`
	UptimePercent *float64 `json:"uptime_percent"`
	Days          []availDay
	Components    []struct {
		ID            uint       `json:"id"`
		Name          string     `json:"name"`
		UptimePercent *float64   `json:"uptime_percent"`
		Days          []availDay `json:"days"`
	} `json:"components"`
}

type availDay struct {
	Date            string            `json:"date"`
	Status          string            `json:"status"`
	Availability    *float64          `json:"availability"`
	DowntimeSeconds int64             `json:"downtime_seconds"`
	Impairments     []availImpairment `json:"impairments"`
}

type availImpairment struct {
	Status  string `json:"status"`
	Seconds int64  `json:"seconds"`
}

type availEnv struct {
	t   *testing.T
	db  *gorm.DB
	r   http.Handler
	now time.Time
}

func newAvailEnv(t *testing.T) *availEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	e := &availEnv{t: t, db: db, now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	h.Now = func() time.Time { return e.now }
	r := testutils.SetupRouter()
	g := r.Group("/api/public")
	g.Use(middleware.OptionalAuthMiddleware())
	g.GET("/pages/:slug/availability", h.GetPublicAvailabilityHandler)
	e.r = r
	return e
}

func (e *availEnv) get(path string, headers ...string) (*httptest.ResponseRecorder, availResp) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	var resp availResp
	if w.Code == http.StatusOK {
		require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &resp))
	}
	return w, resp
}

func (e *availEnv) page(slug string, private bool) models.StatusPage {
	p := models.StatusPage{Name: slug, Slug: slug, IsPrivate: private}
	require.NoError(e.t, e.db.Create(&p).Error)
	// Seite existiert seit langem, sonst fehlen Daten im 90-Tage-Fenster.
	require.NoError(e.t, e.db.Model(&p).Update("created_at", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).Error)
	return p
}

func (e *availEnv) comp(page uint, name, status string, parent *uint) models.Component {
	c := models.Component{StatusPageID: page, Name: name, Status: status, ParentID: parent}
	require.NoError(e.t, e.db.Create(&c).Error)
	return c
}

func (e *availEnv) incident(page uint, status string, start time.Time, end *time.Time, comps ...uint) models.Incident {
	i := models.Incident{StatusPageID: page, Title: "t", Message: "m", Status: status, StartedAt: &start, ResolvedAt: end}
	require.NoError(e.t, e.db.Create(&i).Error)
	for _, c := range comps {
		require.NoError(e.t, e.db.Create(&models.IncidentComponent{IncidentID: i.ID, ComponentID: c}).Error)
	}
	return i
}

func day(t *testing.T, days []availDay, date string) availDay {
	t.Helper()
	for _, d := range days {
		if d.Date == date {
			return d
		}
	}
	t.Fatalf("Tag %s fehlt", date)
	return availDay{}
}

func utc(day, hour int) *time.Time {
	v := time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC)
	return &v
}

func TestAvailability_FormUndLeereSeite(t *testing.T) {
	e := newAvailEnv(t)
	e.page("leer", false)
	w, resp := e.get("/api/public/pages/leer/availability")
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, resp.Days, 90)
	assert.Equal(t, "2026-08-01", resp.From)
	assert.Equal(t, "2026-10-29", resp.To)
	assert.Equal(t, "2026-10-29", resp.Days[89].Date)
	assert.Equal(t, "operational", resp.Days[89].Status)
	require.NotNil(t, resp.UptimePercent)
	assert.InDelta(t, 100, *resp.UptimePercent, 1e-9)
	assert.Empty(t, resp.Components, "Komponenten nur auf Wunsch")
	assert.NotContains(t, w.Body.String(), `"components"`)
}

func TestAvailability_KeineDatenVorSeitenanlage(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("neu", false)
	require.NoError(t, e.db.Model(&p).Update("created_at", *utc(27, 0)).Error)
	_, resp := e.get("/api/public/pages/neu/availability")
	d := day(t, resp.Days, "2026-10-20")
	assert.Equal(t, "no_data", d.Status)
	assert.Nil(t, d.Availability, "keine Daten statt 100 %")
	assert.Equal(t, "operational", day(t, resp.Days, "2026-10-28").Status)
}

func TestAvailability_VorfallUndKomponenten(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("acme", false)
	web := e.comp(p.ID, "Web", "operational", nil)
	api := e.comp(p.ID, "API", "operational", nil)
	e.incident(p.ID, "resolved", *utc(20, 6), utc(20, 12), web.ID)

	_, resp := e.get("/api/public/pages/acme/availability?include=components")
	d := day(t, resp.Days, "2026-10-20")
	assert.Equal(t, "degraded_performance", d.Status)
	// Gesamtseite = Durchschnitt der Komponenten. Web: 6 h x 0,3 =
	// 1,8 h (92,5 %), API 100 % -> 96,25 %, gewichtete Ausfallzeit gemittelt.
	assert.Equal(t, int64(3240), d.DowntimeSeconds)
	require.NotNil(t, d.Availability)
	assert.InDelta(t, 96.25, *d.Availability, 1e-9)
	assert.Equal(t, []availImpairment{{Status: "degraded_performance", Seconds: 6 * 3600}}, d.Impairments)

	require.Len(t, resp.Components, 2)
	for _, c := range resp.Components {
		require.Len(t, c.Days, 90)
		cd := day(t, c.Days, "2026-10-20")
		if c.ID == web.ID {
			assert.Equal(t, "degraded_performance", cd.Status)
			assert.Equal(t, int64(6480), cd.DowntimeSeconds)
			assert.InDelta(t, 92.5, *cd.Availability, 1e-9)
		} else {
			assert.Equal(t, api.ID, c.ID)
			assert.Equal(t, "operational", cd.Status, "Vorfall an Web darf API nicht färben")
		}
	}
}

func TestAvailability_GruppeSeitenweiterVorfallUndGeloeschteKomponente(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("grp", false)
	group := e.comp(p.ID, "Gruppe", "group", nil)
	child := e.comp(p.ID, "Kind", "operational", &group.ID)
	other := e.comp(p.ID, "Andere", "operational", nil)
	gone := e.comp(p.ID, "Gelöscht", "operational", nil)
	require.NoError(t, e.db.Delete(&gone).Error)

	e.incident(p.ID, "resolved", *utc(10, 0), utc(10, 6), group.ID) // Gruppe -> Kind
	e.incident(p.ID, "resolved", *utc(11, 0), utc(11, 6))           // seitenweit
	e.incident(p.ID, "resolved", *utc(12, 0), utc(12, 6), gone.ID)  // nur gelöschte Komponente

	_, resp := e.get("/api/public/pages/grp/availability?include=components")
	assert.Equal(t, "degraded_performance", day(t, resp.Days, "2026-10-10").Status)
	assert.Equal(t, "degraded_performance", day(t, resp.Days, "2026-10-11").Status)
	assert.Equal(t, "operational", day(t, resp.Days, "2026-10-12").Status, "Vorfall gelöschter Komponente ist unsichtbar")

	require.Len(t, resp.Components, 2, "Gruppen und gelöschte Komponenten sind keine Zeilen")
	for _, c := range resp.Components {
		switch c.ID {
		case child.ID:
			assert.Equal(t, "degraded_performance", day(t, c.Days, "2026-10-10").Status)
			assert.Equal(t, "degraded_performance", day(t, c.Days, "2026-10-11").Status, "seitenweiter Vorfall betrifft jede sichtbare Komponente")
			assert.Equal(t, "operational", day(t, c.Days, "2026-10-12").Status, "Vorfall gelöschter Komponente zählt nicht")
		case other.ID:
			assert.Equal(t, "operational", day(t, c.Days, "2026-10-10").Status)
			assert.Equal(t, "degraded_performance", day(t, c.Days, "2026-10-11").Status)
		default:
			t.Fatalf("unerwartete Komponente %d", c.ID)
		}
	}
}

func TestAvailability_LaufenderVorfallWartungUndAltdaten(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("lauf", false)
	// laufend: seit 06:00 heute, Jetzt 12:00 -> 6 von 12 beobachteten Stunden
	e.incident(p.ID, "investigating", *utc(29, 6), nil)
	// Wartung gestern: kein Ausfall
	m := models.Incident{StatusPageID: p.ID, Title: "W", Message: "m", Status: "resolved", IsMaintenance: true,
		ScheduledStartAt: utc(28, 2), ScheduledEndAt: utc(28, 4)}
	require.NoError(t, e.db.Create(&m).Error)
	// behoben ohne ResolvedAt (Altdaten): Ende = letzte Änderung
	old := e.incident(p.ID, "resolved", *utc(25, 0), nil)
	require.NoError(t, e.db.Model(&old).UpdateColumn("updated_at", *utc(25, 3)).Error)

	_, resp := e.get("/api/public/pages/lauf/availability")
	today := day(t, resp.Days, "2026-10-29")
	assert.Equal(t, "degraded_performance", today.Status)
	assert.InDelta(t, 85, *today.Availability, 1e-9, "6 von 12 h x 0,3")

	mt := day(t, resp.Days, "2026-10-28")
	assert.Equal(t, "under_maintenance", mt.Status)
	assert.InDelta(t, 100, *mt.Availability, 1e-9, "Wartung zählt nicht als Ausfall")

	assert.Equal(t, int64(3*3600*3/10), day(t, resp.Days, "2026-10-25").DowntimeSeconds)
}

func TestAvailability_MandantenTrennung(t *testing.T) {
	e := newAvailEnv(t)
	a := e.page("mandant-a", false)
	b := e.page("mandant-b", false)
	bComp := e.comp(b.ID, "Nur B", "operational", nil)
	e.incident(b.ID, "resolved", *utc(20, 0), utc(20, 12), bComp.ID)
	// Vorfall von A zeigt (fehlerhaft) auf Komponente von B: darf B nicht berühren und A nicht preisgeben
	e.incident(a.ID, "resolved", *utc(21, 0), utc(21, 12), bComp.ID)

	_, respA := e.get("/api/public/pages/mandant-a/availability?include=components")
	assert.Equal(t, "operational", day(t, respA.Days, "2026-10-20").Status, "Vorfall von B in A")
	assert.Empty(t, respA.Components)
	// Vorfall von A mit nur fremder Komponente wird ignoriert
	assert.Equal(t, "operational", day(t, respA.Days, "2026-10-21").Status)

	_, respB := e.get("/api/public/pages/mandant-b/availability?include=components")
	assert.Equal(t, "degraded_performance", day(t, respB.Days, "2026-10-20").Status)
	assert.Equal(t, "operational", day(t, respB.Days, "2026-10-21").Status, "Vorfall von A in B")
	require.Len(t, respB.Components, 1)
	assert.Equal(t, "operational", day(t, respB.Components[0].Days, "2026-10-21").Status)
}

func TestAvailability_UnbekannteSeite404(t *testing.T) {
	e := newAvailEnv(t)
	w, _ := e.get("/api/public/pages/gibt-es-nicht/availability")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAvailability_PrivateSeite(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("intern", true)
	e.incident(p.ID, "resolved", *utc(20, 0), utc(20, 12))

	role := models.Role{Name: "Viewer 547", Permissions: "components:read"}
	require.NoError(t, e.db.Create(&role).Error)
	member := models.User{Email: "member-547@example.com"}
	require.NoError(t, e.db.Create(&member).Error)
	require.NoError(t, e.db.Create(&models.UserStatusPageRole{UserID: member.ID, StatusPageID: p.ID, RoleID: role.ID}).Error)
	stranger := models.User{Email: "stranger-547@example.com"}
	require.NoError(t, e.db.Create(&stranger).Error)

	bearer := func(u models.User) string {
		tok, err := middleware.GenerateToken(u.ID, u.Email, "", "", false)
		require.NoError(t, err)
		return "Bearer " + tok
	}

	t.Run("anonym: 404 ohne Daten, wie bei einer fehlenden Seite", func(t *testing.T) {
		w, _ := e.get("/api/public/pages/intern/availability")
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NotContains(t, w.Body.String(), "days")
	})
	t.Run("fremder User ohne Rolle: kein Zugriff", func(t *testing.T) {
		w, _ := e.get("/api/public/pages/intern/availability", "Authorization", bearer(stranger))
		assert.NotEqual(t, http.StatusOK, w.Code)
		assert.NotContains(t, w.Body.String(), "days")
	})
	t.Run("Mitglied: 200", func(t *testing.T) {
		w, resp := e.get("/api/public/pages/intern/availability", "Authorization", bearer(member))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "degraded_performance", day(t, resp.Days, "2026-10-20").Status)
	})
	t.Run("Subscriber-Token einer anderen Seite: kein Zugriff", func(t *testing.T) {
		other := e.page("andere", true)
		tok, err := middleware.GenerateSubscriberToken(1, other.ID)
		require.NoError(t, err)
		w, _ := e.get("/api/public/pages/intern/availability?token=" + tok)
		assert.NotEqual(t, http.StatusOK, w.Code)
	})
}

func TestAvailability_CacheBisTTLDanachNeu(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("cache", false)
	_, first := e.get("/api/public/pages/cache/availability")
	assert.Equal(t, "operational", day(t, first.Days, "2026-10-28").Status)

	e.incident(p.ID, "resolved", *utc(28, 0), utc(28, 6))
	e.now = e.now.Add(30 * time.Second)
	_, cached := e.get("/api/public/pages/cache/availability")
	assert.Equal(t, "operational", day(t, cached.Days, "2026-10-28").Status, "innerhalb 60 s aus dem Cache")

	e.now = e.now.Add(31 * time.Second)
	_, fresh := e.get("/api/public/pages/cache/availability")
	assert.Equal(t, "degraded_performance", day(t, fresh.Days, "2026-10-28").Status, "nach Ablauf neu berechnet")

	// Uhr springt zurück: Eintrag gilt nicht als frisch
	e.now = e.now.Add(-time.Hour)
	_, back := e.get("/api/public/pages/cache/availability")
	assert.Equal(t, "2026-10-29", back.To)
}

// 400 ms nach 00:00 UTC lieferte die Route 500 (NaN im JSON).
func TestAvailability_KnappNachMitternachtAntwortet200(t *testing.T) {
	e := newAvailEnv(t)
	e.page("mitternacht", false)
	e.now = time.Date(2026, 10, 29, 0, 0, 0, 400*int(time.Millisecond), time.UTC)
	w, resp := e.get("/api/public/pages/mitternacht/availability")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "2026-10-29", resp.To)
	w2, _ := e.get("/api/public/pages/mitternacht/availability")
	assert.Equal(t, http.StatusOK, w2.Code, "auch aus dem Cache")
}

func TestAvailability_DBFehlerGibt500(t *testing.T) {
	e := newAvailEnv(t)
	e.page("kaputt", false)
	require.NoError(t, e.db.Exec("DROP TABLE incident_components").Error)
	w, _ := e.get("/api/public/pages/kaputt/availability")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAvailability_KomponentenFehlerGibt500(t *testing.T) {
	e := newAvailEnv(t)
	e.page("kaputt2", false)
	require.NoError(t, e.db.Exec("DROP TABLE components").Error)
	w, _ := e.get("/api/public/pages/kaputt2/availability")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Frisch angelegte Seite (6 h Beobachtung) mit offenem Vorfall
// lieferte uptime_percent 0 und damit "0,00 % verfügbar"; ohne belastbare
// Datenbasis (< 24 h) muss die Gesamtzahl null sein.
func TestAvailability_KeineGesamtzahlBeiKurzerBeobachtung(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("frisch", false)
	require.NoError(t, e.db.Model(&p).Update("created_at", *utc(29, 6)).Error)
	e.incident(p.ID, "investigating", *utc(29, 6), nil)
	_, resp := e.get("/api/public/pages/frisch/availability")
	assert.Nil(t, resp.UptimePercent)
	assert.Equal(t, "no_data", day(t, resp.Days, "2026-10-28").Status)
}
