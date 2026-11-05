package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
)

const hardeningChatURL = "https://chat.googleapis.com/v1/spaces/AAAA1234/messages?key=k&token=t"

// Die Antwort auf POST /api/subscribe verrät nicht, ob die Adresse schon aktiv
// abonniert ist: gleiche Antwort wie für eine neue Adresse, keine Mail, kein
// neuer Datensatz, der bestehende bleibt unverändert.
func TestSubscribe_ActiveAddressLooksLikeNewAddress(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	existing := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "active@example.com", SubscribedComponentIDs: "1,2", Status: models.SubscriberStatusActive}
	require.NoError(t, db.Create(&existing).Error)

	fresh := subscribeEmail(r, "neu@example.com", nil)
	requireStatusCode(t, fresh, http.StatusOK)
	awaitSMTPMessage(t, srv)

	dup := subscribeEmail(r, "active@example.com", nil)
	requireStatusCode(t, dup, http.StatusOK)
	assert.JSONEq(t, fresh.Body.String(), dup.Body.String(), "bekannte und neue Adresse müssen gleich beantwortet werden")
	waitDOIMails(r)
	expectNoSMTPMessage(t, srv)

	var n int64
	db.Model(&models.Subscriber{}).Where("email = ?", "active@example.com").Count(&n)
	assert.EqualValues(t, 1, n)
	var after models.Subscriber
	require.NoError(t, db.First(&after, existing.ID).Error)
	assert.Equal(t, models.SubscriberStatusActive, after.Status)
	assert.Equal(t, "1,2", after.SubscribedComponentIDs, "bestehendes Abo bleibt unverändert")
}

// Wer angemeldet mit der eigenen Konto-Adresse abonniert, erfährt weiter, dass
// das Abo schon besteht: Es ist die eigene Adresse, kein Orakel.
func TestSubscribe_OwnActiveAddressStillReportsExisting(t *testing.T) {
	db, r, page := setupDOIRouter(t)
	user := models.User{Email: "self@example.com"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "self@example.com", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive}).Error)

	w := subscribeEmail(r, "self@example.com", map[string]string{"X-Mock-UID": strconv.Itoa(int(user.ID)), "X-Mock-Email": "self@example.com"})
	requireStatusCode(t, w, http.StatusConflict)
	assert.Equal(t, "subscription_exists_email", responseField(t, w, "code"))
}

// Auch ein bereits abonnierter Webhook wird wie ein neuer beantwortet.
func TestSubscribe_ActiveWebhookLooksLikeNewWebhook(t *testing.T) {
	db, r, _ := setupDOIRouter(t)
	post := func() *httptest.ResponseRecorder {
		return postJSON(r, "/api/subscribe", map[string]string{"channel": "google_chat", "webhook_url": hardeningChatURL, "component_ids": "all", "slug": "doi-page"}, nil)
	}
	first := post()
	requireStatusCode(t, first, http.StatusOK)
	second := post()
	requireStatusCode(t, second, http.StatusOK)
	assert.JSONEq(t, first.Body.String(), second.Body.String())
	var n int64
	db.Model(&models.Subscriber{}).Where("channel = ?", "google_chat").Count(&n)
	assert.EqualValues(t, 1, n)
}

// Schreibvarianten desselben Postfachs (Groß-/Kleinschreibung, +tag) zählen als
// ein Abo und lösen keine weiteren Bestätigungsmails aus.
func TestSubscribe_MailboxVariantsSendOnlyOneConfirmation(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, _ := setupDOIRouter(t)

	requireStatusCode(t, subscribeEmail(r, "Opfer@example.com", nil), http.StatusOK)
	awaitSMTPMessage(t, srv)

	for _, v := range []string{"opfer@example.com", "OPFER@EXAMPLE.COM", "opfer+1@example.com", "Opfer+news@example.com", "opfer+2@example.com"} {
		w := subscribeEmail(r, v, nil)
		requireStatusCode(t, w, http.StatusOK)
		assert.Equal(t, models.SubscriberStatusPending, responseField(t, w, "status"), v)
	}
	waitDOIMails(r)
	expectNoSMTPMessage(t, srv)
	var n int64
	db.Model(&models.Subscriber{}).Count(&n)
	assert.EqualValues(t, 1, n, "Varianten legen keine weiteren Abos an")

	// Gegenprobe: ein anderes Postfach bekommt seine Mail.
	requireStatusCode(t, subscribeEmail(r, "anderer@example.com", nil), http.StatusOK)
	awaitSMTPMessage(t, srv)
}

// Das Limit gilt je Postfach über alle Seiten: Höchstens drei Bestätigungsmails
// in 24 Stunden, die Antwort bleibt gleich.
func TestSubscribe_ConfirmationMailsPerMailboxAreLimitedAcrossPages(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, _ := setupDOIRouter(t)
	slugs := []string{"doi-page"}
	for i := 2; i <= 5; i++ {
		p := models.StatusPage{Name: "Seite " + strconv.Itoa(i), Slug: "doi-page-" + strconv.Itoa(i)}
		require.NoError(t, db.Create(&p).Error)
		slugs = append(slugs, p.Slug)
	}
	subscribe := func(slug, email string) *httptest.ResponseRecorder {
		return postJSON(r, "/api/subscribe", map[string]string{"channel": "email", "email": email, "component_ids": "all", "slug": slug}, nil)
	}

	var bodies []string
	variants := []string{"opfer@example.com", "Opfer+a@example.com", "OPFER+b@example.com", "opfer+c@example.com", "opfer@example.com"}
	for i, slug := range slugs {
		w := subscribe(slug, variants[i])
		requireStatusCode(t, w, http.StatusOK)
		bodies = append(bodies, w.Body.String())
	}
	for i := 0; i < 3; i++ {
		awaitSMTPMessage(t, srv)
	}
	waitDOIMails(r)
	expectNoSMTPMessage(t, srv)
	for _, b := range bodies[1:] {
		assert.JSONEq(t, bodies[0], b, "Antwort darf das Limit nicht verraten")
	}
	var n int64
	db.Model(&models.Subscriber{}).Count(&n)
	assert.EqualValues(t, 3, n, "über dem Limit entsteht kein weiterer Datensatz")

	// Gegenprobe: ein anderes Postfach ist vom Limit nicht betroffen.
	requireStatusCode(t, subscribe(slugs[4], "anderer@example.com"), http.StatusOK)
	awaitSMTPMessage(t, srv)
}

// Sonderzeichen im lokalen Teil wirken nicht als LIKE-Platzhalter: "a_b@" und
// "a%@" sind nicht dasselbe Postfach wie "axb@".
func TestSubscribe_MailboxMatchTreatsWildcardsLiterally(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	require.NoError(t, db.Create(&models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "axb@example.com", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive}).Error)

	for _, e := range []string{"a_b@example.com", "a%@example.com"} {
		requireStatusCode(t, subscribeEmail(r, e, nil), http.StatusOK)
		awaitSMTPMessage(t, srv)
	}
	var n int64
	db.Model(&models.Subscriber{}).Count(&n)
	assert.EqualValues(t, 3, n)
}

// Ein abgelaufener Link wird nur erneuert, solange das Postfach sein Limit
// nicht ausgeschöpft hat.
func TestSubscribe_ExpiredPendingIsNotResentOverLimit(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	expired := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "viel@example.com", SubscribedComponentIDs: "all",
		Status: models.SubscriberStatusPending, ConfirmTokenHash: "alt", ConfirmTokenExpiry: &past}
	require.NoError(t, db.Create(&expired).Error)
	for i := 0; i < 3; i++ {
		p := models.StatusPage{Name: "Weitere " + strconv.Itoa(i), Slug: "weitere-" + strconv.Itoa(i)}
		require.NoError(t, db.Create(&p).Error)
		require.NoError(t, db.Create(&models.Subscriber{StatusPageID: p.ID, Channel: "email", Email: "viel+" + strconv.Itoa(i) + "@example.com", SubscribedComponentIDs: "all",
			Status: models.SubscriberStatusPending, ConfirmTokenHash: "h" + strconv.Itoa(i), ConfirmTokenExpiry: &future}).Error)
	}

	requireStatusCode(t, subscribeEmail(r, "viel@example.com", nil), http.StatusOK)
	waitDOIMails(r)
	expectNoSMTPMessage(t, srv)
	var after models.Subscriber
	require.NoError(t, db.First(&after, expired.ID).Error)
	assert.Equal(t, "alt", after.ConfirmTokenHash, "Token bleibt unverändert, es wurde nichts verschickt")

	// Gegenprobe: ohne die drei anderen gültigen Links wird erneuert.
	require.NoError(t, db.Where("confirm_token_hash IN ?", []string{"h0", "h1", "h2"}).Delete(&models.Subscriber{}).Error)
	requireStatusCode(t, subscribeEmail(r, "viel@example.com", nil), http.StatusOK)
	awaitSMTPMessage(t, srv)
}
