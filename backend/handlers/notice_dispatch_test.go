package handlers_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Regression tests for the notice mail dispatch + the
// associated templates.
//
// Testmatrix:
//   - A notice without NotifySubscribers triggers NO mail dispatch
//     (backward compatibility: previous behavior for all existing customers).
//   - A notice with NotifySubscribers sends a mail to email subscribers
//     of the status page - with the default body if no template is stored.
//   - A custom template for notice_created is used if stored.
//   - {{.Title}} and {{.Level}} are available in notice templates.

// setupNoticeDispatch corresponds to setupNotificationDispatch, but
// additionally creates an email subscriber - notices are aimed at
// subscribers, not at internal users.
func setupNoticeDispatch(t *testing.T, slug string) (*fakeSMTPServer, *handlers.Handler, models.StatusPage, *fakeSMTPServer) {
	t.Helper()
	srv := startFakeSMTPWithEnv(t)

	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "Notice " + slug, Slug: slug}
	if err := db.Create(&page).Error; err != nil {
		t.Fatalf("create page: %v", err)
	}
	sub := models.Subscriber{
		StatusPageID:           page.ID,
		Email:                  "notice-sub-" + slug + "@example.com",
		Channel:                "email",
		SubscribedComponentIDs: "all",
	}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatalf("create subscriber: %v", err)
	}
	return srv, handlers.NewHandler(db), page, srv
}

func TestDispatchNoticeNotifications_OptOut_NoMailSent(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "opt-out")
	notice := models.GeneralNotice{
		StatusPageID:      page.ID,
		Title:             "Silent notice",
		Message:           "Should not send.",
		Level:             "info",
		IsActive:          true,
		NotifySubscribers: false,
	}
	h.DispatchNoticeNotifications(notice, "created")

	// Nothing should arrive at the fake SMTP server. Short wait so that an
	// accidental goroutine dispatch would actually have time.
	select {
	case msg := <-srv.dataCh:
		t.Fatalf("expected no mail for opt-out notice, got:\n%s", msg)
	case <-time.After(300 * time.Millisecond):
		// expected
	}
}

func TestDispatchNoticeNotifications_DefaultBody(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "default-body")
	notice := models.GeneralNotice{
		StatusPageID:      page.ID,
		Title:             "Neue Datenschutzrichtlinie",
		Message:           "Bitte lesen.",
		Level:             "warning",
		IsActive:          true,
		NotifySubscribers: true,
	}
	h.DispatchNoticeNotifications(notice, "created")

	msg := waitForEmail(t, srv)
	if !strings.Contains(msg, "Subject: [WARN] Neue Datenschutzrichtlinie") {
		t.Fatalf("expected default WARN subject, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Warnung") {
		t.Fatalf("expected level badge label 'Warnung' in default body, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Neue Datenschutzrichtlinie") {
		t.Fatalf("expected notice title in body, got:\n%s", msg)
	}
}

func TestDispatchNoticeNotifications_CustomTemplate(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "custom-tpl")

	// Save the custom template (the test goes directly to the DB because the
	// wiring of the handler chain is not the subject here - there is
	// notification_template_admin_test.go).
	tmpl := models.NotificationTemplate{
		StatusPageID: page.ID,
		Kind:         handlers.NotificationKindNoticeCreated,
		Audience:     handlers.NotificationAudienceSubscriber,
		Language:     "de",
		Subject:      "NOTICE :: {{.Title}} :: {{.Level}}",
		BodyHTML:     "<p>NOTICE-BODY :: {{.Message}}</p>",
	}
	if err := h.DB.Create(&tmpl).Error; err != nil {
		t.Fatalf("create template: %v", err)
	}

	notice := models.GeneralNotice{
		StatusPageID:      page.ID,
		Title:             "Wichtiger Hinweis",
		Message:           "Alles neu.",
		Level:             "critical",
		IsActive:          true,
		NotifySubscribers: true,
	}
	h.DispatchNoticeNotifications(notice, "created")

	msg := waitForEmail(t, srv)
	if !strings.Contains(msg, "Subject: NOTICE :: Wichtiger Hinweis :: critical") {
		t.Fatalf("expected custom notice subject with Title+Level placeholders, got:\n%s", msg)
	}
	if !strings.Contains(msg, "NOTICE-BODY :: Alles neu.") {
		t.Fatalf("expected custom notice body with Message placeholder, got:\n%s", msg)
	}
	// The default body header must not appear when the custom template
	// is active.
	if strings.Contains(msg, "Ankündigung") && strings.Contains(msg, "Statushafen") {
		t.Fatalf("expected the default template NOT to be used when a custom template exists, got:\n%s", msg)
	}
}

// Review finding 3: the update handler chooses the mail type (created/updated/ended)
// based on the old and new state. Unique
// subjects per template show which case applied.
func TestUpdateNoticeHandler_VersandArt(t *testing.T) {
	type fall struct {
		name        string
		slug        string
		wasActive   bool
		notify      bool
		body        string
		wantSubject string // empty = no mail expected
	}
	faelle := []fall{
		{"inaktiv zu aktiv sendet created", "upd-created", false, true, `{"Title":"T","Message":"m","IsActive":true}`, "KIND-CREATED :: T"},
		{"aktiv zu aktiv sendet updated", "upd-updated", true, true, `{"Title":"T","Message":"neu","IsActive":true}`, "KIND-UPDATED :: T"},
		{"aktiv zu inaktiv sendet ended", "upd-ended", true, true, `{"Title":"T","Message":"m","IsActive":false}`, "KIND-ENDED :: T"},
		{"inaktiv zu inaktiv sendet nichts", "upd-none", false, true, `{"Title":"T","Message":"m","IsActive":false}`, ""},
		{"ohne NotifySubscribers sendet nichts", "upd-nonotify", false, false, `{"Title":"T","Message":"m","IsActive":true}`, ""},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			srv, h, page, _ := setupNoticeDispatch(t, f.slug)
			createKindTemplates(t, h, page.ID)
			n := models.GeneralNotice{StatusPageID: page.ID, Title: "T", Message: "m", Level: "info", IsActive: f.wasActive}
			if err := h.DB.Create(&n).Error; err != nil {
				t.Fatalf("create notice: %v", err)
			}
			// Set via update, because GORM cannot distinguish the zero value from true on create.
			if err := h.DB.Model(&n).Updates(map[string]any{"is_active": f.wasActive, "notify_subscribers": f.notify}).Error; err != nil {
				t.Fatalf("set flags: %v", err)
			}

			r := testutils.SetupRouter()
			r.PUT("/api/pages/:page_id/notices/:id", h.UpdateNoticeHandler)
			w := doNoticeRequest(r, "PUT", "/api/pages/"+strconv.Itoa(int(page.ID))+"/notices/"+strconv.Itoa(int(n.ID)), []byte(f.body))
			requireStatusCode(t, w, http.StatusOK)

			if f.wantSubject == "" {
				select {
				case msg := <-srv.dataCh:
					t.Fatalf("keine Mail erwartet, erhalten:\n%s", msg)
				case <-time.After(300 * time.Millisecond):
				}
				return
			}
			msg := waitForEmail(t, srv)
			if !strings.Contains(msg, "Subject: "+f.wantSubject) {
				t.Fatalf("erwarteter Betreff %q, erhalten:\n%s", f.wantSubject, msg)
			}
		})
	}
}

// createKindTemplates creates a subscriber template per notice kind with a
// distinct subject (KIND-CREATED/UPDATED/ENDED :: title).
func createKindTemplates(t *testing.T, h *handlers.Handler, pageID uint) {
	t.Helper()
	for kind, prefix := range map[string]string{
		handlers.NotificationKindNoticeCreated: "KIND-CREATED",
		handlers.NotificationKindNoticeUpdated: "KIND-UPDATED",
		handlers.NotificationKindNoticeEnded:   "KIND-ENDED",
	} {
		tmpl := models.NotificationTemplate{
			StatusPageID: pageID,
			Kind:         kind,
			Audience:     handlers.NotificationAudienceSubscriber,
			Language:     "de",
			Subject:      prefix + " :: {{.Title}}",
			BodyHTML:     "<p>{{.Message}}</p>",
		}
		if err := h.DB.Create(&tmpl).Error; err != nil {
			t.Fatalf("create template: %v", err)
		}
	}
}

// Review finding 4: the create handler takes over Collapsed,
// StartAt, EndAt and NotifySubscribers and sends a "created" mail only for an active
// notice with NotifySubscribers.
func TestCreateNoticeHandler_Versand(t *testing.T) {
	type fall struct {
		name        string
		slug        string
		body        string
		wantSubject string // empty = no mail expected
		wantNotify  bool
	}
	faelle := []fall{
		{"aktiv mit NotifySubscribers sendet created", "cr-created", `{"Title":"T","Message":"m","IsActive":true,"NotifySubscribers":true}`, "KIND-CREATED :: T", true},
		{"Entwurf sendet nichts", "cr-draft", `{"Title":"T","Message":"m","IsActive":false,"NotifySubscribers":true}`, "", true},
		{"ohne NotifySubscribers sendet nichts", "cr-nonotify", `{"Title":"T","Message":"m","IsActive":true}`, "", false},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			srv, h, page, _ := setupNoticeDispatch(t, f.slug)
			createKindTemplates(t, h, page.ID)

			r := testutils.SetupRouter()
			r.POST("/api/pages/:page_id/notices", h.CreateNoticeHandler)
			w := doNoticeRequest(r, "POST", "/api/pages/"+strconv.Itoa(int(page.ID))+"/notices", []byte(f.body))
			requireStatusCode(t, w, http.StatusCreated)

			var n models.GeneralNotice
			if err := h.DB.Where("status_page_id = ?", page.ID).First(&n).Error; err != nil {
				t.Fatalf("notice laden: %v", err)
			}
			if n.NotifySubscribers != f.wantNotify {
				t.Fatalf("NotifySubscribers = %v, erwartet %v", n.NotifySubscribers, f.wantNotify)
			}

			if f.wantSubject == "" {
				select {
				case msg := <-srv.dataCh:
					t.Fatalf("keine Mail erwartet, erhalten:\n%s", msg)
				case <-time.After(300 * time.Millisecond):
				}
				return
			}
			msg := waitForEmail(t, srv)
			if !strings.Contains(msg, "Subject: "+f.wantSubject) {
				t.Fatalf("erwarteter Betreff %q, erhalten:\n%s", f.wantSubject, msg)
			}
		})
	}
}

func TestCreateNoticeHandler_UebernimmtCollapsedUndZeitfenster(t *testing.T) {
	_, h, page, _ := setupNoticeDispatch(t, "cr-fields")
	r := testutils.SetupRouter()
	r.POST("/api/pages/:page_id/notices", h.CreateNoticeHandler)
	body := `{"Title":"T","Message":"m","IsActive":false,"Collapsed":true,"StartAt":"2030-01-01T00:00:00Z","EndAt":"2030-01-02T00:00:00Z"}`
	w := doNoticeRequest(r, "POST", "/api/pages/"+strconv.Itoa(int(page.ID))+"/notices", []byte(body))
	requireStatusCode(t, w, http.StatusCreated)

	var n models.GeneralNotice
	if err := h.DB.Where("status_page_id = ?", page.ID).First(&n).Error; err != nil {
		t.Fatalf("notice laden: %v", err)
	}
	if !n.Collapsed {
		t.Error("Collapsed wurde nicht übernommen")
	}
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	if n.StartAt == nil || !n.StartAt.Equal(start) {
		t.Errorf("StartAt = %v, erwartet %v", n.StartAt, start)
	}
	if n.EndAt == nil || !n.EndAt.Equal(end) {
		t.Errorf("EndAt = %v, erwartet %v", n.EndAt, end)
	}
}
