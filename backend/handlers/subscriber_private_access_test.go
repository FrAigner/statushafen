package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// awaitSMTPMessageTo liest Mails vom Fake-SMTP-Server, bis eine an recipient
// adressierte ankommt (DispatchNotifications schickt parallel auch interne
// Mails an Seitenmitglieder).
func awaitSMTPMessageTo(t *testing.T, srv *fakeSMTPServer, recipient string) string {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-srv.dataCh:
			if strings.Contains(msg, "To: "+recipient) {
				return msg
			}
		case <-deadline:
			t.Fatalf("expected an email to %s within the timeout", recipient)
			return ""
		}
	}
}

// Das Subscriber-Token gewährt auf
// privaten Seiten nur noch Lesezugriff, solange der User, der das Abo angelegt
// hat, die Seite selbst sehen darf (Global Admin oder Rolle auf der Seite).
// Sonst behielten über die Lücke angelegte Altabos, Abos von vor einer
// Umstellung öffentlich -> privat und Abos nach einem Rollenentzug dauerhaft
// Zugriff.
func TestPrivatePage_SubscriberTokenRequiresAuthorizedCreator(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	public := r.Group("/api/public")
	public.Use(middleware.OptionalAuthMiddleware())
	public.GET("/pages/:slug/dashboard", h.GetPublicDashboardHandler)

	page := models.StatusPage{Name: "Intern Token", Slug: "intern-token-327", IsPrivate: true}
	db.Create(&page)
	role := models.Role{Name: "Viewer 327", Permissions: "components:read"}
	db.Create(&role)
	member := models.User{Email: "member-327@example.com"}
	db.Create(&member)
	db.Create(&models.UserStatusPageRole{UserID: member.ID, StatusPageID: page.ID, RoleID: role.ID})
	admin := models.User{Email: "admin-327@example.com", IsGlobalAdmin: true}
	db.Create(&admin)

	dashboardWithToken := func(t *testing.T, sub models.Subscriber, slug string) int {
		t.Helper()
		token, err := middleware.GenerateSubscriberToken(sub.ID, sub.StatusPageID)
		if err != nil {
			t.Fatalf("token: %v", err)
		}
		req, _ := http.NewRequest("GET", "/api/public/pages/"+slug+"/dashboard?token="+token, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	t.Run("Altabo ohne Ersteller: kein Zugriff", func(t *testing.T) {
		sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "legacy-327@example.com", SubscribedComponentIDs: "all"}
		db.Create(&sub)
		if code := dashboardWithToken(t, sub, page.Slug); code == http.StatusOK {
			t.Fatalf("expected no dashboard access for a legacy anonymous subscriber, got %d", code)
		}
	})

	t.Run("Ersteller ohne Rolle mehr: kein Zugriff", func(t *testing.T) {
		former := models.User{Email: "former-327@example.com"}
		db.Create(&former)
		link := models.UserStatusPageRole{UserID: former.ID, StatusPageID: page.ID, RoleID: role.ID}
		db.Create(&link)
		sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: former.Email, SubscribedComponentIDs: "all", CreatedByUserID: &former.ID}
		db.Create(&sub)
		db.Unscoped().Delete(&link)
		if code := dashboardWithToken(t, sub, page.Slug); code == http.StatusOK {
			t.Fatalf("expected no dashboard access after the creator's role was revoked, got %d", code)
		}
	})

	t.Run("Ersteller existiert nicht mehr: kein Zugriff", func(t *testing.T) {
		gone := uint(999999)
		sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "gone-327@example.com", SubscribedComponentIDs: "all", CreatedByUserID: &gone}
		db.Create(&sub)
		if code := dashboardWithToken(t, sub, page.Slug); code == http.StatusOK {
			t.Fatalf("expected no dashboard access for a deleted creator, got %d", code)
		}
	})

	t.Run("öffentliche Seite mit anonymem Abo wird privat: kein Zugriff", func(t *testing.T) {
		switched := models.StatusPage{Name: "Wird privat", Slug: "wird-privat-327"}
		db.Create(&switched)
		sub := models.Subscriber{StatusPageID: switched.ID, Channel: "email", Email: "early-327@example.com", SubscribedComponentIDs: "all"}
		db.Create(&sub)
		db.Model(&switched).Update("is_private", true)
		if code := dashboardWithToken(t, sub, switched.Slug); code == http.StatusOK {
			t.Fatalf("expected no dashboard access after switching the page to private, got %d", code)
		}
	})

	t.Run("Gegenprobe: Abo eines Mitglieds mit Rolle: 200", func(t *testing.T) {
		sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: member.Email, SubscribedComponentIDs: "all", CreatedByUserID: &member.ID}
		db.Create(&sub)
		if code := dashboardWithToken(t, sub, page.Slug); code != http.StatusOK {
			t.Fatalf("expected 200 for a member's subscription, got %d", code)
		}
	})

	t.Run("Gegenprobe: vom Global Admin angelegtes Abo für Externe: 200", func(t *testing.T) {
		sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "extern-327@example.com", SubscribedComponentIDs: "all", CreatedByUserID: &admin.ID}
		db.Create(&sub)
		if code := dashboardWithToken(t, sub, page.Slug); code != http.StatusOK {
			t.Fatalf("expected 200 for an admin-created subscription, got %d", code)
		}
	})
}

// Auch der Versand muss die Berechtigung prüfen,
// sonst bekäme ein Altabo weiter jeden Vorfall der privaten Seite per Mail.
func TestDispatchNotifications_PrivatePageSkipsUnauthorizedSubscriber(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.test")

	db, h, comp, inc := setupDispatchFixturesForPage(models.StatusPage{Name: "Private Skip", Slug: "private-skip-327", IsPrivate: true})
	db.Create(&models.Subscriber{StatusPageID: comp.StatusPageID, Channel: "email", Email: "legacy-skip@example.com", SubscribedComponentIDs: "all"})

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	select {
	case msg := <-srv.dataCh:
		t.Fatalf("expected no email for an unauthorized subscriber on a private page, got:\n%s", msg)
	case <-time.After(500 * time.Millisecond):
	}
}

// Dieselbe Lücke im Notice-Versand. Hinweise privater
// Seiten gehen nur an Abos mit berechtigtem Ersteller.
func TestDispatchNoticeNotifications_PrivatePageSkipsUnauthorizedSubscriber(t *testing.T) {
	srv := startFakeSMTPWithEnv(t)
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)

	page := models.StatusPage{Name: "Private Notice", Slug: "private-notice-327", IsPrivate: true}
	db.Create(&page)
	admin := models.User{Email: "notice-admin-327@example.com", IsGlobalAdmin: true}
	db.Create(&admin)
	db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "legacy-notice@example.com", SubscribedComponentIDs: "all"})
	db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "ok-notice@example.com", SubscribedComponentIDs: "all", CreatedByUserID: &admin.ID})

	h.DispatchNoticeNotifications(models.GeneralNotice{
		StatusPageID: page.ID, Title: "Wartung", Message: "Heute Abend.", Level: "info", IsActive: true, NotifySubscribers: true,
	}, "created")

	var got []string
	deadline := time.After(1500 * time.Millisecond)
collect:
	for {
		select {
		case m := <-srv.dataCh:
			got = append(got, m)
		case <-deadline:
			break collect
		}
	}
	okSeen := false
	for _, m := range got {
		if strings.Contains(m, "To: legacy-notice@example.com") {
			t.Fatalf("expected no notice email for the legacy subscriber, got:\n%s", m)
		}
		if strings.Contains(m, "To: ok-notice@example.com") && strings.Contains(m, "/status/private-notice-327?token=") {
			okSeen = true
		}
	}
	if !okSeen {
		t.Fatalf("expected a notice email with token link for the authorized subscriber, got %d mails", len(got))
	}
}
