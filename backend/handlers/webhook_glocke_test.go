package handlers

import (
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Ratenlimit und dauerhafte Ablehnung eines Webhooks erscheinen in
// der Glocke der Admins (Nachrichtencenter). Vor der Verdrahtung
// wurde der Fehler nur geloggt; niemand erfuhr davon.

type glockeFixture struct {
	h   *Handler
	sub models.Subscriber
}

func newGlockeFixture(t *testing.T, url string) glockeFixture {
	t.Helper()
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "Glocke", Slug: "glocke"}
	if err := db.Create(&page).Error; err != nil {
		t.Fatal(err)
	}
	sub := models.Subscriber{StatusPageID: page.ID, Channel: "google_chat", WebhookURL: url, Status: "active"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	return glockeFixture{h: &Handler{DB: db}, sub: sub}
}

func (f glockeFixture) nachrichten(t *testing.T, kind string) []models.AdminNotification {
	t.Helper()
	var out []models.AdminNotification
	if err := f.h.DB.Where("kind = ?", kind).Find(&out).Error; err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWebhookGlocke_RatenlimitErzeugtNachricht(t *testing.T) {
	newWebhookEnv(t)
	srv, _ := sequenceServer(t, []fakeResp{{429, "60"}}) // länger als das Wartebudget: sofort aufgeben
	f := newGlockeFixture(t, srv.URL+"/geheim?key=GEHEIM&token=GEHEIM")

	f.h.notifySubscriberGoogleChat(notifyCtx(), f.sub)

	n := f.nachrichten(t, AdminNotifWebhookRateLimited)
	if len(n) != 1 {
		t.Fatalf("erwartet 1 Ratenlimit-Nachricht, gefunden %d", len(n))
	}
	if n[0].StatusPageID == nil || *n[0].StatusPageID != f.sub.StatusPageID {
		t.Errorf("Nachricht gehört nicht zur Seite des Abonnenten: %+v", n[0].StatusPageID)
	}
	if strings.Contains(n[0].Params, "GEHEIM") || strings.Contains(n[0].Params, "127.0.0.1") || strings.Contains(n[0].Params, "://") {
		t.Errorf("Parameter enthalten URL, Key oder Token: %s", n[0].Params)
	}
	if got := len(f.nachrichten(t, AdminNotifWebhookRejected)); got != 0 {
		t.Errorf("bei 429 darf keine Ablehnungs-Nachricht entstehen, gefunden %d", got)
	}
}

func TestWebhookGlocke_AblehnungErzeugtNachrichtJeAbonnent(t *testing.T) {
	newWebhookEnv(t)
	srv, calls := sequenceServer(t, []fakeResp{{403, ""}})
	f := newGlockeFixture(t, srv.URL+"/geheim?key=GEHEIM")

	f.h.notifySubscriberGoogleChat(notifyCtx(), f.sub)
	f.h.notifySubscriberGoogleChat(notifyCtx(), f.sub) // zweiter Versand: zusammengefasst

	n := f.nachrichten(t, AdminNotifWebhookRejected)
	if len(n) != 1 || n[0].Count < 2 {
		t.Fatalf("erwartet 1 zusammengefasste Nachricht mit Zähler >= 2, gefunden %d (%+v)", len(n), n)
	}
	if *calls != 2 {
		t.Errorf("403 darf nicht wiederholt werden, Aufrufe = %d", *calls)
	}
	if strings.Contains(n[0].Params, "GEHEIM") || strings.Contains(n[0].Params, "://") {
		t.Errorf("Parameter enthalten URL oder Key: %s", n[0].Params)
	}
}

func TestWebhookGlocke_ErfolgUndTransientErzeugenKeineNachricht(t *testing.T) {
	for name, seq := range map[string][]fakeResp{"Erfolg": {{200, ""}}, "503": {{503, ""}}} {
		t.Run(name, func(t *testing.T) {
			newWebhookEnv(t)
			srv, _ := sequenceServer(t, seq)
			f := newGlockeFixture(t, srv.URL+"/hook")
			f.h.notifySubscriberGoogleChat(notifyCtx(), f.sub)
			var n int64
			f.h.DB.Model(&models.AdminNotification{}).Count(&n)
			if n != 0 {
				t.Errorf("erwartet keine Nachricht, gefunden %d", n)
			}
		})
	}
}

func TestWebhookGlocke_ExternerVorfallMeldetAblehnung(t *testing.T) {
	newWebhookEnv(t)
	srv, _ := sequenceServer(t, []fakeResp{{404, ""}})
	f := newGlockeFixture(t, srv.URL+"/hook")
	m := &extIncidentMail{}
	m.ev.Incident.Title = "Ext"

	if err := f.h.sendExternalIncidentWebhook(m, f.sub); err == nil {
		t.Fatal("bei 404 muss weiter ein Fehler zurückkommen")
	}
	if got := len(f.nachrichten(t, AdminNotifWebhookRejected)); got != 1 {
		t.Errorf("erwartet 1 Ablehnungs-Nachricht für den externen Vorfall, gefunden %d", got)
	}
}
