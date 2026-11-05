package handlers_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Double-Opt-in für E-Mail-Abos.

// Token aus crypto/rand.Text (128 Bit, Base32).
var confirmLinkRe = regexp.MustCompile(`/subscribe/confirm\?token=([A-Z2-7]{26})`)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// doiHandlers ordnet jedem Router aus setupDOIRouter seinen Handler zu, damit
// Tests mit waitDOIMails auf den Versand im Hintergrund warten können.
var doiHandlers sync.Map

// waitDOIMails blockiert, bis alle Bestätigungsmails des Routers versendet oder
// gescheitert sind; erst danach sind Mail-Eingang, Audit und Log aussagekräftig.
func waitDOIMails(r http.Handler) {
	if h, ok := doiHandlers.Load(r); ok {
		h.(*handlers.Handler).WaitMails()
	}
}

// setupDOIRouter registriert die Abo-Endpunkte samt einer Mock-Auth, die
// userID/email aus X-Mock-UID/X-Mock-Email übernimmt (wie OptionalAuthMiddleware).
func setupDOIRouter(t *testing.T) (*gorm.DB, *gin.Engine, models.StatusPage) {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	// Die Bestätigungsmails laufen im Hintergrund. Vor dem Testende abwarten, damit
	// kein Versand in den nächsten Test (SMTP-Umgebung) hineinläuft.
	doiHandlers.Store(r, h)
	t.Cleanup(h.WaitMails)
	r.Use(func(c *gin.Context) {
		if uid := c.GetHeader("X-Mock-UID"); uid != "" {
			n, _ := strconv.Atoi(uid)
			c.Set("userID", uint(n))
		}
		if em := c.GetHeader("X-Mock-Email"); em != "" {
			c.Set("email", em)
		}
		c.Next()
	})
	r.POST("/api/subscribe", h.SubscribeHandler)
	r.POST("/api/public/subscribers/confirm", h.ConfirmSubscriptionHandler)
	r.POST("/api/pages/:page_id/subscribers", h.CreateSubscriberAdminHandler)
	r.PUT("/api/pages/:page_id/subscribers/:id", h.UpdateSubscriberHandler)
	r.GET("/api/me/subscription", h.GetMySubscriptionHandler)

	page := models.StatusPage{Name: "DOI Page", Slug: "doi-page"}
	db.Create(&page)
	return db, r, page
}

func postJSON(r http.Handler, path string, payload interface{}, headers map[string]string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, path, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func subscribeEmail(r http.Handler, email string, headers map[string]string) *httptest.ResponseRecorder {
	return postJSON(r, "/api/subscribe", map[string]string{
		"channel": "email", "email": email, "component_ids": "all", "slug": "doi-page",
	}, headers)
}

func responseField(t *testing.T, w *httptest.ResponseRecorder, key string) interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Antwort ist kein JSON: %v (%s)", err, w.Body.String())
	}
	return resp[key]
}

// expectNoSMTPMessage stellt sicher, dass innerhalb kurzer Zeit keine Mail ankommt.
func expectNoSMTPMessage(t *testing.T, srv *fakeSMTPServer) {
	t.Helper()
	select {
	case msg := <-srv.dataCh:
		t.Fatalf("keine Mail erwartet, erhalten: %s", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

func extractConfirmToken(t *testing.T, msg string) string {
	t.Helper()
	m := confirmLinkRe.FindStringSubmatch(msg)
	if m == nil {
		t.Fatalf("Bestätigungslink fehlt in der Mail: %s", msg)
	}
	return m[1]
}

func auditCountFor(db *gorm.DB, action string) int64 {
	var n int64
	db.Model(&models.AuditLog{}).Where("action = ?", action).Count(&n)
	return n
}

func TestSubscribe_AnonymousEmailStartsPendingAndSendsConfirmation(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.com")
	db, r, _ := setupDOIRouter(t)

	w := subscribeEmail(r, "anon@example.com", nil)
	requireStatusCode(t, w, http.StatusOK)
	if got := responseField(t, w, "status"); got != models.SubscriberStatusPending {
		t.Fatalf("status = %v, erwartet pending", got)
	}
	if got, _ := responseField(t, w, "message").(string); !strings.Contains(got, "Bitte bestätige dein Abo") {
		t.Fatalf("message = %q, erwartet Hinweis auf die Bestätigung", got)
	}

	var sub models.Subscriber
	db.Where("email = ?", "anon@example.com").First(&sub)
	if sub.Status != models.SubscriberStatusPending {
		t.Fatalf("DB-Status = %q, erwartet pending", sub.Status)
	}
	if sub.ConfirmTokenExpiry == nil || time.Until(*sub.ConfirmTokenExpiry) < 23*time.Hour || time.Until(*sub.ConfirmTokenExpiry) > 25*time.Hour {
		t.Fatalf("Ablaufzeit nicht ~24h: %v", sub.ConfirmTokenExpiry)
	}

	msg := awaitSMTPMessage(t, srv)
	if !strings.Contains(msg, "To: anon@example.com") {
		t.Fatalf("Empfänger falsch: %s", msg)
	}
	if !strings.Contains(msg, "https://status.example.com/subscribe/confirm?token=") {
		t.Fatalf("Link zeigt nicht aufs Frontend: %s", msg)
	}
	// Absenderadresse ist die des Betreibers (SMTP_FROM), der Seitenname der Anzeigename.
	if !strings.Contains(msg, `From: "DOI Page" <alerts@statushafen.local>`) {
		t.Fatalf("Absender der Seite fehlt: %s", msg)
	}
	token := extractConfirmToken(t, msg)
	// Nur der Hash steht in der DB, nie der Klartext-Token.
	if sub.ConfirmTokenHash != sha256Hex(token) {
		t.Fatalf("ConfirmTokenHash ist nicht sha256(token)")
	}
	if strings.Contains(sub.ConfirmTokenHash, token) {
		t.Fatalf("Klartext-Token in der DB gespeichert")
	}
}

func TestSubscribe_LoggedInWithOwnEmailSkipsConfirmation(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, _ := setupDOIRouter(t)
	user := models.User{Email: "owner@example.com"}
	db.Create(&user)

	// Groß-/Kleinschreibung der Adresse spielt keine Rolle.
	w := subscribeEmail(r, "Owner@Example.com", map[string]string{"X-Mock-UID": strconv.Itoa(int(user.ID)), "X-Mock-Email": "owner@example.com"})
	requireStatusCode(t, w, http.StatusOK)
	if got := responseField(t, w, "status"); got != models.SubscriberStatusActive {
		t.Fatalf("status = %v, erwartet active", got)
	}
	if got := responseField(t, w, "message"); got != "Erfolgreich abonniert" {
		t.Fatalf("message = %v", got)
	}
	var sub models.Subscriber
	db.Where("email = ?", "Owner@Example.com").First(&sub)
	if sub.Status != models.SubscriberStatusActive || sub.ConfirmTokenHash != "" || sub.ConfirmTokenExpiry != nil {
		t.Fatalf("eigenes Abo sollte ohne Token aktiv sein: %+v", sub)
	}
	waitDOIMails(r)
	expectNoSMTPMessage(t, srv)
}

func TestSubscribe_LoggedInWithForeignEmailNeedsConfirmation(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, _ := setupDOIRouter(t)
	user := models.User{Email: "owner@example.com"}
	db.Create(&user)

	w := subscribeEmail(r, "victim@example.com", map[string]string{"X-Mock-UID": strconv.Itoa(int(user.ID)), "X-Mock-Email": "owner@example.com"})
	requireStatusCode(t, w, http.StatusOK)
	if got := responseField(t, w, "status"); got != models.SubscriberStatusPending {
		t.Fatalf("status = %v, erwartet pending", got)
	}
	awaitSMTPMessage(t, srv)
}

// Ohne userID (z.B. kaputter Token-Claim) zählt eine passende email allein nicht.
func TestSubscribe_EmailClaimWithoutUserIDNeedsConfirmation(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	_, r, _ := setupDOIRouter(t)

	w := subscribeEmail(r, "owner@example.com", map[string]string{"X-Mock-Email": "owner@example.com"})
	requireStatusCode(t, w, http.StatusOK)
	if got := responseField(t, w, "status"); got != models.SubscriberStatusPending {
		t.Fatalf("status = %v, erwartet pending", got)
	}
	awaitSMTPMessage(t, srv)
}

func TestSubscribe_InvalidEmailIsRejected(t *testing.T) {
	db, r, _ := setupDOIRouter(t)
	w := subscribeEmail(r, "kein-mail-format", nil)
	requireStatusCode(t, w, http.StatusBadRequest)
	var n int64
	db.Model(&models.Subscriber{}).Count(&n)
	if n != 0 {
		t.Fatalf("es darf kein Abo angelegt werden")
	}
}

func TestSubscribe_GoogleChatStaysActiveWithoutConfirmation(t *testing.T) {
	db, r, _ := setupDOIRouter(t)

	// Die öffentliche Anmeldung nimmt nur echte Google-Chat-Webhooks an.
	w := postJSON(r, "/api/subscribe", map[string]string{
		"channel": "google_chat", "webhook_url": "https://chat.googleapis.com/v1/spaces/AAAA1234/messages?key=k&token=t", "component_ids": "all", "slug": "doi-page",
	}, nil)
	requireStatusCode(t, w, http.StatusOK)
	var sub models.Subscriber
	db.Where("channel = ?", "google_chat").First(&sub)
	if sub.Status != models.SubscriberStatusActive {
		t.Fatalf("Webhook-Abo sollte aktiv sein, ist %q", sub.Status)
	}
}

func TestSubscribe_PendingDuplicateDoesNotResendWhileTokenValid(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, _ := setupDOIRouter(t)

	requireStatusCode(t, subscribeEmail(r, "dup@example.com", nil), http.StatusOK)
	awaitSMTPMessage(t, srv)
	var before models.Subscriber
	db.Where("email = ?", "dup@example.com").First(&before)

	w := subscribeEmail(r, "dup@example.com", nil)
	// Gleiche Antwort wie beim ersten Mal: verrät nicht, dass schon ein Abo wartet.
	requireStatusCode(t, w, http.StatusOK)
	if got := responseField(t, w, "status"); got != models.SubscriberStatusPending {
		t.Fatalf("status = %v, erwartet pending", got)
	}
	waitDOIMails(r)
	expectNoSMTPMessage(t, srv)
	var after models.Subscriber
	db.Where("email = ?", "dup@example.com").First(&after)
	if after.ConfirmTokenHash != before.ConfirmTokenHash {
		t.Fatalf("Token darf bei gültigem Link nicht neu erzeugt werden")
	}
}

func TestSubscribe_PendingDuplicateWithExpiredTokenResends(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, _ := setupDOIRouter(t)

	requireStatusCode(t, subscribeEmail(r, "late@example.com", nil), http.StatusOK)
	awaitSMTPMessage(t, srv)
	past := time.Now().Add(-time.Hour)
	db.Model(&models.Subscriber{}).Where("email = ?", "late@example.com").Update("confirm_token_expiry", past)
	var before models.Subscriber
	db.Where("email = ?", "late@example.com").First(&before)

	requireStatusCode(t, subscribeEmail(r, "late@example.com", nil), http.StatusOK)
	token := extractConfirmToken(t, awaitSMTPMessage(t, srv))
	var after models.Subscriber
	db.Where("email = ?", "late@example.com").First(&after)
	if after.ConfirmTokenHash == before.ConfirmTokenHash || after.ConfirmTokenHash != sha256Hex(token) {
		t.Fatalf("abgelaufener Token muss ersetzt werden")
	}
	if after.ConfirmTokenExpiry == nil || after.ConfirmTokenExpiry.Before(time.Now()) {
		t.Fatalf("neue Ablaufzeit fehlt")
	}
}

func TestSubscribe_PendingDuplicateByOwnerActivatesDirectly(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, _ := setupDOIRouter(t)
	user := models.User{Email: "self@example.com"}
	db.Create(&user)

	requireStatusCode(t, subscribeEmail(r, "self@example.com", nil), http.StatusOK)
	awaitSMTPMessage(t, srv)

	w := subscribeEmail(r, "self@example.com", map[string]string{"X-Mock-UID": strconv.Itoa(int(user.ID)), "X-Mock-Email": "self@example.com"})
	requireStatusCode(t, w, http.StatusOK)
	if got := responseField(t, w, "status"); got != models.SubscriberStatusActive {
		t.Fatalf("status = %v, erwartet active", got)
	}
	expectNoSMTPMessage(t, srv)
	var sub models.Subscriber
	db.Where("email = ?", "self@example.com").First(&sub)
	if sub.Status != models.SubscriberStatusActive || sub.ConfirmTokenHash != "" {
		t.Fatalf("Abo sollte aktiv und ohne Token sein: %+v", sub)
	}
	if sub.CreatedByUserID == nil || *sub.CreatedByUserID != user.ID {
		t.Fatalf("Abo sollte dem Konto zugeordnet werden")
	}
	if auditCountFor(db, "subscriber.confirmed") != 1 {
		t.Fatalf("Audit subscriber.confirmed fehlt")
	}
}

// Bewusst geändert (vorher 409): Ein aktives Abo wird wie ein neues beantwortet,
// damit die Antwort nicht verrät, ob die Adresse abonniert ist; siehe
// subscribe_hardening_test.go.
func TestSubscribe_ActiveDuplicateIsAnsweredLikeNewAddress(t *testing.T) {
	db, r, page := setupDOIRouter(t)
	db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "active@example.com", SubscribedComponentIDs: "all"})

	w := subscribeEmail(r, "active@example.com", nil)
	requireStatusCode(t, w, http.StatusOK)
	if got := responseField(t, w, "status"); got != models.SubscriberStatusPending {
		t.Fatalf("status = %v, erwartet die Antwort wie für eine neue Adresse (pending)", got)
	}
}

func TestSubscribe_PendingDuplicateSaveErrorReturns500(t *testing.T) {
	db, r, page := setupDOIRouter(t)
	past := time.Now().Add(-time.Hour)
	db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "err@example.com", SubscribedComponentIDs: "all",
		Status: models.SubscriberStatusPending, ConfirmTokenHash: "x", ConfirmTokenExpiry: &past})
	failSubscriberWrites(t, db)

	requireStatusCode(t, subscribeEmail(r, "err@example.com", nil), http.StatusInternalServerError)
}

func TestSubscribe_CreateErrorReturns500(t *testing.T) {
	db, r, _ := setupDOIRouter(t)
	failSubscriberWrites(t, db)

	requireStatusCode(t, subscribeEmail(r, "create-err@example.com", nil), http.StatusInternalServerError)
}

// failSubscriberWrites lässt alle Updates/Creates/Deletes auf subscribers scheitern.
func failSubscriberWrites(t *testing.T, db *gorm.DB) {
	t.Helper()
	cb := func(tx *gorm.DB) {
		if tx.Statement.Table == "subscribers" {
			_ = tx.AddError(errors.New("simulierter Fehler"))
		}
	}
	name := "fail_subscribers_" + t.Name()
	if err := db.Callback().Update().Before("gorm:update").Register(name+"_u", cb); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register(name+"_c", cb); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register(name+"_d", cb); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove(name + "_u")
		_ = db.Callback().Create().Remove(name + "_c")
		_ = db.Callback().Delete().Remove(name + "_d")
	})
}

func TestConfirmSubscription_ActivatesWithValidToken(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, _ := setupDOIRouter(t)

	requireStatusCode(t, subscribeEmail(r, "confirm@example.com", nil), http.StatusOK)
	token := extractConfirmToken(t, awaitSMTPMessage(t, srv))

	w := postJSON(r, "/api/public/subscribers/confirm", map[string]string{"token": token}, nil)
	requireStatusCode(t, w, http.StatusOK)
	if got := responseField(t, w, "slug"); got != "doi-page" {
		t.Fatalf("slug = %v", got)
	}
	var sub models.Subscriber
	db.Where("email = ?", "confirm@example.com").First(&sub)
	if sub.Status != models.SubscriberStatusActive || sub.ConfirmTokenHash != "" || sub.ConfirmTokenExpiry != nil {
		t.Fatalf("Abo nach Bestätigung nicht aktiv/aufgeräumt: %+v", sub)
	}
	if auditCountFor(db, "subscriber.confirmed") != 1 {
		t.Fatalf("Audit subscriber.confirmed fehlt")
	}

	// Einmal-Token: ein zweiter Klick ist ungültig.
	requireStatusCode(t, postJSON(r, "/api/public/subscribers/confirm", map[string]string{"token": token}, nil), http.StatusBadRequest)
}

func TestConfirmSubscription_InvalidRequests(t *testing.T) {
	_, r, _ := setupDOIRouter(t)

	requireStatusCode(t, postJSON(r, "/api/public/subscribers/confirm", map[string]string{}, nil), http.StatusBadRequest)
	requireStatusCode(t, postJSON(r, "/api/public/subscribers/confirm", map[string]string{"token": "unbekannt"}, nil), http.StatusBadRequest)
}

func TestConfirmSubscription_ExpiredTokenReturns410AndRemovesPendingSubscriber(t *testing.T) {
	db, r, page := setupDOIRouter(t)
	past := time.Now().Add(-time.Minute)
	sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "old@example.com", SubscribedComponentIDs: "all",
		Status: models.SubscriberStatusPending, ConfirmTokenHash: sha256Hex("old-token"), ConfirmTokenExpiry: &past}
	db.Create(&sub)

	requireStatusCode(t, postJSON(r, "/api/public/subscribers/confirm", map[string]string{"token": "old-token"}, nil), http.StatusGone)
	var n int64
	db.Model(&models.Subscriber{}).Where("id = ?", sub.ID).Count(&n)
	if n != 0 {
		t.Fatalf("abgelaufenes, unbestätigtes Abo sollte entfernt werden")
	}
}

func TestConfirmSubscription_TokenWithoutExpiryCountsAsExpired(t *testing.T) {
	db, r, page := setupDOIRouter(t)
	db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "noexp@example.com", SubscribedComponentIDs: "all",
		Status: models.SubscriberStatusPending, ConfirmTokenHash: sha256Hex("noexp-token")})

	requireStatusCode(t, postJSON(r, "/api/public/subscribers/confirm", map[string]string{"token": "noexp-token"}, nil), http.StatusGone)
}

func TestConfirmSubscription_SaveErrorReturns500(t *testing.T) {
	db, r, page := setupDOIRouter(t)
	future := time.Now().Add(time.Hour)
	db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "dberr@example.com", SubscribedComponentIDs: "all",
		Status: models.SubscriberStatusPending, ConfirmTokenHash: sha256Hex("dberr-token"), ConfirmTokenExpiry: &future})
	failSubscriberWrites(t, db)

	requireStatusCode(t, postJSON(r, "/api/public/subscribers/confirm", map[string]string{"token": "dberr-token"}, nil), http.StatusInternalServerError)
}

func TestConfirmSubscription_UsesCustomTemplate(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	db.Create(&models.NotificationTemplate{
		StatusPageID: page.ID, Kind: handlers.NotificationKindSubscriptionConfirm, Audience: handlers.NotificationAudienceSubscriber, Language: "de",
		Subject: "Bitte bestätigen: {{.Title}}", BodyHTML: `<a href="{{.ConfirmURL}}">Jetzt</a>`,
	})

	requireStatusCode(t, subscribeEmail(r, "tmpl@example.com", nil), http.StatusOK)
	msg := awaitSMTPMessage(t, srv)
	if !strings.Contains(msg, "Subject: Bitte bestätigen: DOI Page") {
		t.Fatalf("Custom-Subject fehlt: %s", msg)
	}
	extractConfirmToken(t, msg)
}

func TestSubscribe_ConfirmationMailFailureIsAudited(t *testing.T) {
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "1")
	db, r, _ := setupDOIRouter(t)

	// Das Abo bleibt trotzdem bestehen (wie bei Bestellungen), der Fehler landet im Audit-Log.
	requireStatusCode(t, subscribeEmail(r, "smtpfail@example.com", nil), http.StatusOK)
	waitDOIMails(r)
	if auditCountFor(db, "subscriber.confirmation_email_failed") != 1 {
		t.Fatalf("Audit subscriber.confirmation_email_failed fehlt")
	}
}

func TestSubscribe_ConfirmationMailSkippedOnHeaderInjection(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	db.Model(&page).Update("name", "Evil\r\nBcc: x@example.com")

	requireStatusCode(t, subscribeEmail(r, "inject@example.com", nil), http.StatusOK)
	waitDOIMails(r)
	expectNoSMTPMessage(t, srv)
}

func TestAdminCreateSubscriber_ForeignEmailNeedsConfirmation(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	admin := models.User{Email: "admin@example.com", IsGlobalAdmin: true}
	db.Create(&admin)
	hdr := map[string]string{"X-Mock-UID": strconv.Itoa(int(admin.ID)), "X-Mock-Email": "admin@example.com"}
	pid := strconv.Itoa(int(page.ID))

	w := postJSON(r, "/api/pages/"+pid+"/subscribers", map[string]string{"Channel": "email", "Email": "extern@example.com", "SubscribedComponentIDs": "all"}, hdr)
	requireStatusCode(t, w, http.StatusCreated)
	if got := responseField(t, w, "Status"); got != models.SubscriberStatusPending {
		t.Fatalf("Status = %v, erwartet pending", got)
	}
	if msg := awaitSMTPMessage(t, srv); !strings.Contains(msg, "To: extern@example.com") {
		t.Fatalf("Bestätigungsmail an falschen Empfänger: %s", msg)
	}

	w = postJSON(r, "/api/pages/"+pid+"/subscribers", map[string]string{"Channel": "email", "Email": "admin@example.com", "SubscribedComponentIDs": "all"}, hdr)
	requireStatusCode(t, w, http.StatusCreated)
	if got := responseField(t, w, "Status"); got != models.SubscriberStatusActive {
		t.Fatalf("eigene Adresse: Status = %v, erwartet active", got)
	}
	waitDOIMails(r)
	expectNoSMTPMessage(t, srv)
}

func TestAdminSubscriber_InvalidEmailIsRejected(t *testing.T) {
	db, r, page := setupDOIRouter(t)
	pid := strconv.Itoa(int(page.ID))
	requireStatusCode(t, postJSON(r, "/api/pages/"+pid+"/subscribers", map[string]string{"Channel": "email", "Email": "Name <a@example.com>"}, nil), http.StatusBadRequest)

	sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "ok@example.com", SubscribedComponentIDs: "all"}
	db.Create(&sub)
	body, _ := json.Marshal(map[string]string{"Channel": "email", "Email": "kaputt", "SubscribedComponentIDs": "all"})
	req, _ := http.NewRequest(http.MethodPut, "/api/pages/"+pid+"/subscribers/"+strconv.Itoa(int(sub.ID)), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusBadRequest)
}

func TestAdminUpdateSubscriber_EmailChangeRequiresNewConfirmation(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	sub := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "before@example.com", SubscribedComponentIDs: "all"}
	db.Create(&sub)
	path := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/subscribers/" + strconv.Itoa(int(sub.ID))
	put := func(payload map[string]string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, path, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Nur Komponenten geändert: bleibt aktiv, keine Mail.
	requireStatusCode(t, put(map[string]string{"Channel": "email", "Email": "before@example.com", "SubscribedComponentIDs": "1"}), http.StatusOK)
	expectNoSMTPMessage(t, srv)
	db.First(&sub, sub.ID)
	if sub.Status != models.SubscriberStatusActive {
		t.Fatalf("ohne Adresswechsel muss das Abo aktiv bleiben")
	}

	// Neue Adresse: muss erneut bestätigt werden.
	requireStatusCode(t, put(map[string]string{"Channel": "email", "Email": "after@example.com", "SubscribedComponentIDs": "1"}), http.StatusOK)
	if msg := awaitSMTPMessage(t, srv); !strings.Contains(msg, "To: after@example.com") {
		t.Fatalf("Bestätigungsmail an falschen Empfänger: %s", msg)
	}
	db.First(&sub, sub.ID)
	if sub.Status != models.SubscriberStatusPending {
		t.Fatalf("nach Adresswechsel erwartet pending, ist %q", sub.Status)
	}
}

func TestGetMySubscription_ReturnsStatus(t *testing.T) {
	db, r, page := setupDOIRouter(t)
	db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "me@example.com", SubscribedComponentIDs: "all", Status: models.SubscriberStatusPending})

	req, _ := http.NewRequest(http.MethodGet, "/api/me/subscription?slug=doi-page", nil)
	req.Header.Set("X-Mock-Email", "me@example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusOK)
	if got := responseField(t, w, "status"); got != models.SubscriberStatusPending {
		t.Fatalf("status = %v, erwartet pending", got)
	}
}

func TestNotificationTemplate_Preview_SubscriptionConfirm(t *testing.T) {
	env := newNotifAdminHarness(t)

	rec := env.do(t, http.MethodPost, "/api/pages/1/notification-templates/subscription_confirm/subscriber/de/preview", map[string]string{
		"Subject":  "Abo für {{.Title}}",
		"BodyHTML": `<a href="{{.ConfirmURL}}">ok</a>`,
	})
	requireStatusCode(t, rec, http.StatusOK)
	if got := responseField(t, rec, "subject"); got != "Abo für Acme Status" {
		t.Fatalf("subject = %v", got)
	}
	if got, _ := responseField(t, rec, "bodyHTML").(string); !strings.Contains(got, "/subscribe/confirm?token=SAMPLE_TOKEN") {
		t.Fatalf("ConfirmURL fehlt in der Vorschau: %s", got)
	}
}

func TestDispatchNotifications_SkipsPendingSubscribers(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, h, comp, inc := setupDispatchFixtures("Pending Dispatch Page", "pending-dispatch-page")
	db.Create(&models.Subscriber{StatusPageID: comp.StatusPageID, Channel: "email", Email: "pending@example.com", SubscribedComponentIDs: "all", Status: models.SubscriberStatusPending})

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")
	expectNoSMTPMessage(t, srv)
}

func TestDispatchNoticeNotifications_SkipsPendingSubscribers(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	page := models.StatusPage{Name: "Pending Notice Page", Slug: "pending-notice-page"}
	db.Create(&page)
	db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "pending@example.com", SubscribedComponentIDs: "all", Status: models.SubscriberStatusPending})

	h.DispatchNoticeNotifications(models.GeneralNotice{StatusPageID: page.ID, Title: "Hinweis", Message: "Text", Level: "info", IsActive: true, NotifySubscribers: true}, "created")
	expectNoSMTPMessage(t, srv)
}
