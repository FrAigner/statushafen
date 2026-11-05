package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Admin-Nachrichtencenter (Glocke).

type anFixture struct {
	*extFixture
	now    time.Time
	subTok string // Rolle nur mit subscribers:write auf f.page
	// zweiter Admin der Seite (components:write), für den Lesestatus je Benutzer
	pageAdmin2Tok string
}

func newAnFixture(t *testing.T) *anFixture {
	t.Helper()
	f := &anFixture{extFixture: newExtFixture(t), now: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	f.clock = f.now
	f.h.Now = func() time.Time { return f.now }
	auth := f.r.Group("/api")
	auth.Use(middleware.AuthMiddleware())
	for _, prefix := range []string{"/pages/:page_id/admin-notifications", "/admin-notifications"} {
		an := auth.Group(prefix)
		an.GET("", f.h.ListAdminNotificationsHandler)
		an.GET("/unread-count", f.h.AdminNotificationUnreadCountHandler)
		an.POST("/read-all", f.h.MarkAllAdminNotificationsReadHandler)
		an.POST("/:id/read", f.h.MarkAdminNotificationReadHandler)
	}
	sub := models.Role{Name: "Abo-Betreuer", Permissions: "subscribers:read,subscribers:write"}
	require.NoError(t, f.db.Create(&sub).Error)
	f.subTok = f.user("sub@example.com", false, map[uint]uint{f.page.ID: sub.ID})
	var rw models.Role
	require.NoError(t, f.db.Where("name = ?", "Page Admin").First(&rw).Error)
	f.pageAdmin2Tok = f.user("pa2@example.com", false, map[uint]uint{f.page.ID: rw.ID})
	return f
}

func (f *anFixture) notify(pageID *uint, key string) {
	f.h.NotifyAdmins(pageID, "webhook_rejected", "error", "adminNotifications.webhook_rejected.title",
		"adminNotifications.webhook_rejected.body", map[string]any{"channel": "google_chat", "subscriber_id": 7}, "/admin/subscribers", key)
}

func (f *anFixture) all() []models.AdminNotification {
	var rows []models.AdminNotification
	require.NoError(f.t, f.db.Order("id").Find(&rows).Error)
	return rows
}

type anListResp struct {
	Items []struct {
		ID         uint            `json:"id"`
		Count      int             `json:"count"`
		Read       bool            `json:"read"`
		Params     json.RawMessage `json:"params"`
		ResolvedAt *time.Time      `json:"resolved_at"`
		LinkPath   string          `json:"link_path"`
	} `json:"items"`
}

func (f *anFixture) list(path, tok string) (int, anListResp) {
	f.t.Helper()
	w := f.do("GET", path, tok, nil)
	var out anListResp
	if w.Code == http.StatusOK {
		require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &out))
	}
	return w.Code, out
}

func (f *anFixture) unread(path, tok string) int {
	f.t.Helper()
	w := f.do("GET", path, tok, nil)
	require.Equal(f.t, http.StatusOK, w.Code, w.Body.String())
	var out struct {
		N int `json:"unread_count"`
	}
	require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &out))
	return out.N
}

func (f *anFixture) pagePath(p models.StatusPage, suffix string) string {
	return "/api/pages/" + pid(p) + "/admin-notifications" + suffix
}

// --- Dienst: Zusammenfassen -------------------------------------------------

func TestNotifyAdmins_FasstGleicheMeldungenInnerhalbVonFuenfzehnMinutenZusammen(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "k")
	f.now = f.now.Add(10 * time.Minute)
	f.notify(&p, "k")
	f.notify(&p, "k")

	rows := f.all()
	require.Len(t, rows, 1)
	assert.Equal(t, 3, rows[0].Count)
	assert.True(t, rows[0].LastSeenAt.Equal(f.now))
	assert.True(t, rows[0].FirstSeenAt.Equal(f.now.Add(-10*time.Minute)))
}

func TestNotifyAdmins_NachDemFensterEntstehtEineNeueZeileUndDieAlteVerliertDenSchluessel(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "k")
	f.now = f.now.Add(16 * time.Minute)
	f.notify(&p, "k")
	f.notify(&p, "k")

	rows := f.all()
	require.Len(t, rows, 2)
	assert.Equal(t, 1, rows[0].Count)
	assert.Empty(t, rows[0].DedupKey, "abgelöste Zeile nimmt nicht mehr am Unique-Index teil")
	assert.Equal(t, 2, rows[1].Count)
}

func TestNotifyAdmins_VerschiedeneSeitenUndSchluesselWerdenNichtZusammengefasst(t *testing.T) {
	f := newAnFixture(t)
	a, b := f.page.ID, f.other.ID
	f.notify(&a, "k")
	f.notify(&b, "k") // Mandantentrennung: gleicher Schlüssel, andere Seite
	f.notify(&a, "k2")
	f.notify(nil, "k") // systemweit
	assert.Len(t, f.all(), 4)
}

func TestNotifyAdmins_LeererSchluesselFasstNieZusammen(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "")
	f.notify(&p, "")
	assert.Len(t, f.all(), 2)
}

func TestNotifyAdmins_GelesenesLebtBeiWiederholungFuerAlleAlsUngelesenAuf(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "k")
	id := f.all()[0].ID
	path := f.pagePath(f.page, "/"+strconv.Itoa(int(id))+"/read")
	require.Equal(t, http.StatusNoContent, f.do("POST", path, f.pageAdminTok, nil).Code)
	require.Equal(t, http.StatusNoContent, f.do("POST", path, f.pageAdmin2Tok, nil).Code)
	require.Equal(t, 0, f.unread(f.pagePath(f.page, "/unread-count"), f.pageAdminTok))

	f.now = f.now.Add(time.Minute)
	f.notify(&p, "k")

	assert.Equal(t, 1, f.unread(f.pagePath(f.page, "/unread-count"), f.pageAdminTok))
	assert.Equal(t, 1, f.unread(f.pagePath(f.page, "/unread-count"), f.pageAdmin2Tok))
	assert.Len(t, f.all(), 1)
}

func TestNotifyAdmins_ParallelAngelegteMeldungWirdZusammengefasstStattZuScheitern(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	// Konkurrenz: Kurz vor dem eigenen INSERT legt eine andere Anfrage dieselbe
	// offene Meldung an. Der Unique-Index schlägt zu, der Dienst zählt hoch.
	fired := false
	require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("an_race", func(tx *gorm.DB) {
		if !fired {
			fired = true
			rival := models.AdminNotification{
				StatusPageID: &p, Kind: "webhook_rejected", Severity: "error", TitleKey: "t", Params: "{}",
				DedupKey: "p" + strconv.Itoa(int(p)) + ":k", Count: 1, FirstSeenAt: f.now, LastSeenAt: f.now,
			}
			require.NoError(t, f.db.Session(&gorm.Session{NewDB: true}).Create(&rival).Error)
		}
	}))
	f.notify(&p, "k")

	rows := f.all()
	require.Len(t, rows, 1, "genau eine offene Meldung je Schlüssel")
	assert.Equal(t, 2, rows[0].Count)
}

func TestNotifyAdmins_ErledigteMeldungWirdNichtMehrZusammengefasst(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "k")
	f.h.ResolveAdminNotifications(&p, "k")
	f.notify(&p, "k")
	rows := f.all()
	require.Len(t, rows, 2)
	assert.NotNil(t, rows[0].ResolvedAt)
	assert.Nil(t, rows[1].ResolvedAt)
	assert.Equal(t, 1, f.unread(f.pagePath(f.page, "/unread-count"), f.pageAdminTok), "erledigte zählt nicht")
}

func TestNotifyAdmins_FehlerBlockiertNicht(t *testing.T) {
	f := newAnFixture(t)
	require.NoError(t, f.db.Migrator().DropTable(&models.NotificationRead{}, &models.AdminNotification{}))
	p := f.page.ID
	assert.NotPanics(t, func() { f.notify(&p, "k") })
	assert.NotPanics(t, func() { f.h.ResolveAdminNotifications(&p, "k") })
}

func TestNotifyAdmins_UnbekannterSchweregradWirdInfo(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.h.NotifyAdmins(&p, "x", "kaputt", "t", "b", nil, "", "")
	assert.Equal(t, "info", f.all()[0].Severity)
}

// --- Geheimnisse und Links --------------------------------------------------

func TestNotifyAdmins_SpeichertKeineGeheimnisseOderAdressen(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.h.NotifyAdmins(&p, "webhook_rejected", "error", "t", "b", map[string]any{
		"channel":      "google_chat",
		"seconds":      30,
		"webhook_url":  "https://chat.googleapis.com/v1/spaces/X/messages?key=GEHEIM&token=T",
		"detail":       "POST https://chat.googleapis.com/x fehlgeschlagen",
		"email":        "abo@example.com",
		"recipient":    "abo@example.com",
		"Access_Token": "abc",
	}, "/admin/subscribers", "")
	raw := f.all()[0].Params
	assert.JSONEq(t, `{"channel":"google_chat","seconds":30}`, raw)
	for _, bad := range []string{"GEHEIM", "chat.googleapis", "abo@", "abc"} {
		assert.NotContains(t, raw, bad)
	}
}

func TestNotifyAdmins_LinkNurRelativerAdminPfad(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	for _, bad := range []string{"https://evil.example/admin/x", "//evil.example", "/admin//evil.example", "javascript:alert(1)", "/public", "/admin/\\evil"} {
		f.h.NotifyAdmins(&p, "x", "info", "t", "b", nil, bad, "")
	}
	f.h.NotifyAdmins(&p, "x", "info", "t", "b", nil, "/admin/subscribers", "")
	for i, r := range f.all() {
		if i < 6 {
			assert.Empty(t, r.LinkPath, "Link %d", i)
		} else {
			assert.Equal(t, "/admin/subscribers", r.LinkPath)
		}
	}
}

func TestWebhookNachrichten_TexteParamsUndSchluessel(t *testing.T) {
	f := newAnFixture(t)
	f.h.NotifyWebhookRateLimited(f.page.ID, "google_chat", 30)
	f.h.NotifyWebhookRateLimited(f.page.ID, "google_chat", 45)
	f.h.NotifyWebhookRejected(f.page.ID, "google_chat", 7)
	f.h.NotifyWebhookRejected(f.page.ID, "google_chat", 8)

	rows := f.all()
	require.Len(t, rows, 3, "Ratenlimit je Kanal zusammengefasst, Ablehnung je Abonnent")
	assert.Equal(t, "webhook_rate_limited", rows[0].Kind)
	assert.Equal(t, "warning", rows[0].Severity)
	assert.Equal(t, 2, rows[0].Count)
	assert.JSONEq(t, `{"channel":"google_chat","seconds":45}`, rows[0].Params)
	assert.Equal(t, "adminNotifications.webhook_rate_limited.title", rows[0].TitleKey)
	assert.Equal(t, "webhook_rejected", rows[1].Kind)
	assert.Equal(t, "error", rows[1].Severity)
	assert.JSONEq(t, `{"channel":"google_chat","subscriber_id":7}`, rows[1].Params)
	assert.Equal(t, "/admin/subscribers", rows[1].LinkPath)
}

// --- API: Sichtbarkeit und Mandantentrennung --------------------------------

func TestAdminNotificationsAPI_SichtbarkeitNachRolle(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "k")
	path := f.pagePath(f.page, "")

	cases := []struct {
		name string
		tok  string
		want int
	}{
		{"Global Admin", f.adminTok, http.StatusOK},
		{"Seiten-Admin components:write", f.pageAdminTok, http.StatusOK},
		{"Rolle mit subscribers:write", f.subTok, http.StatusOK},
		{"Betrachter (nur Lesen)", f.viewerTok, http.StatusForbidden},
		{"Admin einer anderen Seite", f.otherTok, http.StatusForbidden},
		{"ohne Anmeldung", "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, f.do("GET", path, tc.tok, nil).Code)
			assert.Equal(t, tc.want, f.do("GET", path+"/unread-count", tc.tok, nil).Code)
			wantPost := tc.want
			if wantPost == http.StatusOK {
				wantPost = http.StatusNoContent
			}
			assert.Equal(t, wantPost, f.do("POST", path+"/read-all", tc.tok, nil).Code)
		})
	}
	w := f.do("GET", path, f.viewerTok, nil)
	assert.Contains(t, w.Body.String(), `"code":"forbidden"`)
}

func TestAdminNotificationsAPI_UnbekannteSeiteIstFuerGlobalAdminsEine404(t *testing.T) {
	f := newAnFixture(t)
	w := f.do("GET", "/api/pages/99999/admin-notifications", f.adminTok, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"page_not_found"`)
	assert.Equal(t, http.StatusForbidden, f.do("GET", "/api/pages/99999/admin-notifications", f.pageAdminTok, nil).Code)
	assert.Equal(t, http.StatusBadRequest, f.do("GET", "/api/pages/abc/admin-notifications", f.adminTok, nil).Code)
}

func TestAdminNotificationsAPI_MandantentrennungBeiListeZaehlerUndLesen(t *testing.T) {
	f := newAnFixture(t)
	mine, theirs := f.page.ID, f.other.ID
	f.notify(&mine, "a")
	f.notify(&theirs, "a")
	f.notify(nil, "sys")
	rows := f.all()

	code, list := f.list(f.pagePath(f.page, ""), f.pageAdminTok)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, list.Items, 1)
	assert.Equal(t, rows[0].ID, list.Items[0].ID)
	assert.Equal(t, 1, f.unread(f.pagePath(f.page, "/unread-count"), f.pageAdminTok))

	// Nachricht einer anderen Seite oder systemweit: 404, auch für den Global Admin.
	for _, other := range []uint{rows[1].ID, rows[2].ID} {
		for _, tok := range []string{f.pageAdminTok, f.adminTok} {
			w := f.do("POST", f.pagePath(f.page, "/"+strconv.Itoa(int(other))+"/read"), tok, nil)
			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.Contains(t, w.Body.String(), `"code":"notification_not_found"`)
		}
	}
	// read-all der eigenen Seite lässt fremde Nachrichten unberührt.
	require.Equal(t, http.StatusNoContent, f.do("POST", f.pagePath(f.page, "/read-all"), f.adminTok, nil).Code)
	var reads []models.NotificationRead
	require.NoError(t, f.db.Find(&reads).Error)
	require.Len(t, reads, 1)
	assert.Equal(t, rows[0].ID, reads[0].NotificationID)
}

func TestAdminNotificationsAPI_SystemweitNurFuerGlobalAdmins(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(nil, "sys")
	f.notify(&p, "seite")

	for _, tok := range []string{f.pageAdminTok, f.subTok, f.viewerTok} {
		assert.Equal(t, http.StatusForbidden, f.do("GET", "/api/admin-notifications", tok, nil).Code)
		assert.Equal(t, http.StatusForbidden, f.do("GET", "/api/admin-notifications/unread-count", tok, nil).Code)
		assert.Equal(t, http.StatusForbidden, f.do("POST", "/api/admin-notifications/read-all", tok, nil).Code)
	}
	assert.Equal(t, http.StatusUnauthorized, f.do("GET", "/api/admin-notifications", "", nil).Code)

	code, list := f.list("/api/admin-notifications", f.adminTok)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, list.Items, 1, "nur systemweite Nachrichten, keine der Seite")
	assert.Equal(t, 1, f.unread("/api/admin-notifications/unread-count", f.adminTok))
	id := strconv.Itoa(int(list.Items[0].ID))
	assert.Equal(t, http.StatusForbidden, f.do("POST", "/api/admin-notifications/"+id+"/read", f.pageAdminTok, nil).Code)
	assert.Equal(t, http.StatusNoContent, f.do("POST", "/api/admin-notifications/"+id+"/read", f.adminTok, nil).Code)
	assert.Equal(t, 0, f.unread("/api/admin-notifications/unread-count", f.adminTok))
	// Seiten-Nachricht über die systemweite Route: 404.
	seiteID := strconv.Itoa(int(f.all()[1].ID))
	assert.Equal(t, http.StatusNotFound, f.do("POST", "/api/admin-notifications/"+seiteID+"/read", f.adminTok, nil).Code)
}

func TestAdminNotificationsAPI_LesestatusJeBenutzer(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "a")
	f.notify(&p, "b")
	rows := f.all()
	count := f.pagePath(f.page, "/unread-count")

	w := f.do("POST", f.pagePath(f.page, "/"+strconv.Itoa(int(rows[0].ID))+"/read"), f.pageAdminTok, nil)
	require.Equal(t, http.StatusNoContent, w.Code)
	// Wiederholen ist idempotent.
	require.Equal(t, http.StatusNoContent, f.do("POST", f.pagePath(f.page, "/"+strconv.Itoa(int(rows[0].ID))+"/read"), f.pageAdminTok, nil).Code)

	assert.Equal(t, 1, f.unread(count, f.pageAdminTok))
	assert.Equal(t, 2, f.unread(count, f.pageAdmin2Tok), "der andere Benutzer hat noch nichts gelesen")
	assert.Equal(t, 2, f.unread(count, f.adminTok))

	_, list := f.list(f.pagePath(f.page, ""), f.pageAdminTok)
	require.Len(t, list.Items, 2)
	byID := map[uint]bool{}
	for _, it := range list.Items {
		byID[it.ID] = it.Read
	}
	assert.True(t, byID[rows[0].ID])
	assert.False(t, byID[rows[1].ID])

	_, onlyUnread := f.list(f.pagePath(f.page, "?unread=1"), f.pageAdminTok)
	require.Len(t, onlyUnread.Items, 1)
	assert.Equal(t, rows[1].ID, onlyUnread.Items[0].ID)

	require.Equal(t, http.StatusNoContent, f.do("POST", f.pagePath(f.page, "/read-all"), f.pageAdmin2Tok, nil).Code)
	require.Equal(t, http.StatusNoContent, f.do("POST", f.pagePath(f.page, "/read-all"), f.pageAdmin2Tok, nil).Code)
	assert.Equal(t, 0, f.unread(count, f.pageAdmin2Tok))
	assert.Equal(t, 1, f.unread(count, f.pageAdminTok), "read-all eines anderen Benutzers ändert nichts")
}

func TestAdminNotificationsAPI_ListeBegrenztSortiertUndLiefertParams(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	for i := 0; i < 5; i++ {
		f.now = f.now.Add(time.Minute)
		f.notify(&p, "k"+strconv.Itoa(i))
	}
	rows := f.all()
	_, list := f.list(f.pagePath(f.page, "?limit=2"), f.pageAdminTok)
	require.Len(t, list.Items, 2)
	assert.Equal(t, rows[4].ID, list.Items[0].ID, "neueste zuerst")
	assert.JSONEq(t, `{"channel":"google_chat","subscriber_id":7}`, string(list.Items[0].Params))
	assert.Equal(t, "/admin/subscribers", list.Items[0].LinkPath)

	_, big := f.list(f.pagePath(f.page, "?limit=100000"), f.pageAdminTok)
	assert.Len(t, big.Items, 5)
	_, bad := f.list(f.pagePath(f.page, "?limit=abc"), f.pageAdminTok)
	assert.Len(t, bad.Items, 5, "ungültiges Limit fällt auf den Standard zurück")
}

func TestAdminNotificationsAPI_UngueltigeId(t *testing.T) {
	f := newAnFixture(t)
	w := f.do("POST", f.pagePath(f.page, "/abc/read"), f.pageAdminTok, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"invalid_id"`)
	assert.Equal(t, http.StatusNotFound, f.do("POST", f.pagePath(f.page, "/99999/read"), f.pageAdminTok, nil).Code)
}

func TestAdminNotificationsAPI_DatenbankfehlerGebenFehlercode500(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "k")
	require.NoError(t, f.db.Migrator().DropTable(&models.NotificationRead{}))
	for _, c := range []struct{ method, suffix string }{
		{"GET", ""}, {"GET", "/unread-count"}, {"POST", "/read-all"}, {"POST", "/1/read"},
	} {
		w := f.do(c.method, f.pagePath(f.page, c.suffix), f.pageAdminTok, nil)
		assert.Equal(t, http.StatusInternalServerError, w.Code, c.method+c.suffix)
		assert.Contains(t, w.Body.String(), `"code":"server_error"`)
	}
	require.NoError(t, f.db.Migrator().DropTable(&models.AdminNotification{}))
	assert.Equal(t, http.StatusInternalServerError, f.do("GET", f.pagePath(f.page, "/unread-count"), f.pageAdminTok, nil).Code)
	assert.Equal(t, http.StatusInternalServerError, f.do("POST", f.pagePath(f.page, "/1/read"), f.pageAdminTok, nil).Code)
}

// --- Ausfall externer Quellen -----------------------------------------------

func TestAusfallExternerQuelle_ErzeugtNachrichtUndEntwarnungErledigtSie(t *testing.T) {
	f := newAnFixture(t)
	f.clock = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	f.now = f.clock
	srcID := f.createSource(f.page, f.adminTok, "https://status.example.com")
	vm := f.extComp(srcID, "vm", "VM", "major_outage")
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.pageAdminTok, f.comp.ID, vm.ID).Code)
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", srcID).
		Update("last_success_at", f.clock.Add(-50*time.Minute)).Error)
	f.provider.err = errBoom

	start := f.clock
	f.now = start.Add(10 * time.Minute)
	f.fetchAt(srcID, f.now)
	rows := f.all()
	require.Len(t, rows, 1)
	n := rows[0]
	assert.Equal(t, "external_source_outage", n.Kind)
	assert.Equal(t, "error", n.Severity)
	assert.Equal(t, f.page.ID, *n.StatusPageID)
	assert.Equal(t, "/admin/external-sources", n.LinkPath)
	assert.NotContains(t, n.Params, "example.com", "keine Adresse der Fremdseite")
	assert.NotContains(t, n.Params, "HTTP 503", "kein Fehlertext der Fremdseite")
	assert.Equal(t, 1, f.unread(f.pagePath(f.page, "/unread-count"), f.pageAdminTok))

	// Erinnerung nach 24 Stunden: eine weitere offene Meldung.
	f.now = start.Add(25 * time.Hour)
	f.fetchAt(srcID, f.now)
	require.GreaterOrEqual(t, len(f.all()), 2)

	// Entwarnung: Ausfall-Nachricht erledigt, Info "wieder erreichbar".
	f.provider.err = nil
	f.provider.sum = &externalstatus.Summary{Components: []externalstatus.Component{{ExternalID: "vm", Name: "VM", Status: "operational", RawStatus: "operational"}}}
	f.now = start.Add(26 * time.Hour)
	f.fetchAt(srcID, f.now)
	var open, resolved, recovered int
	for _, r := range f.all() {
		switch {
		case r.Kind == "external_source_recovered":
			recovered++
			assert.Equal(t, "info", r.Severity)
		case r.ResolvedAt != nil:
			resolved++
		default:
			open++
		}
	}
	assert.Equal(t, 1, recovered)
	assert.Zero(t, open, "keine offene Ausfall-Nachricht nach der Entwarnung")
	assert.Positive(t, resolved)
	assert.Equal(t, 1, f.unread(f.pagePath(f.page, "/unread-count"), f.pageAdminTok), "nur die Entwarnung ist ungelesen")
}

// --- Fehler- und Randpfade ---------------------------------------------------

func TestNotifyAdmins_ParamsMitNichtSerialisierbaremWertFallenAufLeeresObjekt(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.h.NotifyAdmins(&p, "x", "info", "t", "b", map[string]any{"kanal": make(chan int)}, "", "")
	assert.Equal(t, "{}", f.all()[0].Params)
}

func TestResolveAdminNotifications_LeererSchluesselIstEinNoOp(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "")
	f.h.ResolveAdminNotifications(&p, "")
	assert.Nil(t, f.all()[0].ResolvedAt)
}

func TestNotifyAdmins_ParallelErledigteMeldungWirdNeuAngelegtStattHochgezaehlt(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "k")
	// Zwischen Lesen und Hochzählen löst eine parallele Anfrage die Zeile ab.
	fired := false
	require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("an_detach", func(tx *gorm.DB) {
		if fired {
			return
		}
		fired = true
		require.NoError(t, f.db.Session(&gorm.Session{NewDB: true}).Exec("UPDATE admin_notifications SET dedup_key = ''").Error)
	}))
	f.notify(&p, "k")
	rows := f.all()
	require.Len(t, rows, 2)
	assert.Equal(t, 1, rows[0].Count)
	assert.Equal(t, 1, rows[1].Count)
}

func TestNotifyAdmins_FehlerBeimHochzaehlenBlockiertNichtUndLegtNeuAn(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "k")
	require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("an_fail", func(tx *gorm.DB) {
		_ = tx.AddError(errors.New("boom"))
	}))
	assert.NotPanics(t, func() { f.notify(&p, "k") })
}

func TestAdminNotificationsAPI_NichtAngemeldetUndGeloeschterBenutzer(t *testing.T) {
	f := newAnFixture(t)
	f.r.GET("/api/offen/pages/:page_id/admin-notifications", f.h.ListAdminNotificationsHandler)
	assert.Equal(t, http.StatusUnauthorized, f.do("GET", "/api/offen/pages/1/admin-notifications", "", nil).Code)

	var u models.User
	require.NoError(t, f.db.Where("email = ?", "pa@example.com").First(&u).Error)
	require.NoError(t, f.db.Unscoped().Delete(&u).Error)
	w := f.do("GET", f.pagePath(f.page, ""), f.pageAdminTok, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAdminNotificationsAPI_DatenbankfehlerBeiSeitenPruefungUndRollen(t *testing.T) {
	f := newAnFixture(t)
	// Rollenabfrage scheitert: kein Zugriff (403), kein 500 mit Details.
	require.NoError(t, f.db.Migrator().DropTable(&models.UserStatusPageRole{}))
	assert.Equal(t, http.StatusForbidden, f.do("GET", f.pagePath(f.page, ""), f.subTok, nil).Code)
	// Seitenabfrage des Global Admins scheitert: 500 mit Code.
	require.NoError(t, f.db.Migrator().DropTable(&models.StatusPage{}))
	w := f.do("GET", f.pagePath(f.page, ""), f.adminTok, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"server_error"`)
}

func TestAdminNotificationsAPI_ListeFehlerUndKaputteParams(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "k")
	require.NoError(t, f.db.Model(&models.AdminNotification{}).Where("1 = 1").Update("params", "kein json").Error)
	_, list := f.list(f.pagePath(f.page, ""), f.pageAdminTok)
	require.Len(t, list.Items, 1)
	assert.JSONEq(t, "{}", string(list.Items[0].Params))

	require.NoError(t, f.db.Migrator().DropTable(&models.AdminNotification{}))
	w := f.do("GET", f.pagePath(f.page, ""), f.pageAdminTok, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"server_error"`)
}

func TestNotifyAdmins_FehlerBeimZuruecksetzenDesLesestatusBlockiertNicht(t *testing.T) {
	f := newAnFixture(t)
	p := f.page.ID
	f.notify(&p, "k")
	require.NoError(t, f.db.Callback().Delete().Before("gorm:delete").Register("an_read_fail", func(tx *gorm.DB) {
		_ = tx.AddError(errors.New("boom"))
	}))
	f.notify(&p, "k")
	rows := f.all()
	require.Len(t, rows, 1)
	assert.Equal(t, 2, rows[0].Count, "hochgezählt trotz Fehler beim Lesestatus")
}
