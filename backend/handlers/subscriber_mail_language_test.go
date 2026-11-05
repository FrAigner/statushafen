package handlers_test

import (
	"net/http"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Teil 1: Mails an Abonnenten folgen der Standardsprache der
// Statusseite (vorher fest "de"). Vorlage der Seitenstandardsprache, sonst
// eingebauter Standardtext (de oder en) - nie eine Vorlage einer anderen Sprache.

// setPageDefaultLanguage legt Katalogsprache und Seitenzuordnung als Standard an.
func setPageDefaultLanguage(t *testing.T, db *gorm.DB, pageID uint, code string) {
	t.Helper()
	require.NoError(t, db.Where("code = ?", code).FirstOrCreate(&models.Language{Code: code, Name: code, IsActive: true}).Error)
	require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: pageID, LanguageCode: code, IsDefault: true}).Error)
}

func addEmailSubscriber(t *testing.T, db *gorm.DB, pageID uint, email string) {
	t.Helper()
	require.NoError(t, db.Create(&models.Subscriber{StatusPageID: pageID, Email: email, Channel: "email", SubscribedComponentIDs: "all"}).Error)
}

// --- Vorfälle ---------------------------------------------------------------

func TestSubscriberMailSprache_Vorfall_SeitenstandardEnglisch_Standardtext(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "sl-inc-default", "sl-inc-admin@example.com")
	setPageDefaultLanguage(t, db, page.ID, "en")
	addEmailSubscriber(t, db, page.ID, "sl-inc-sub@example.com")
	inc := seedIncident(t, db, page.ID, "Latency", "Looking.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessageTo(t, srv, "sl-inc-sub@example.com")
	assert.Contains(t, msg, "Status update for", "englischer Standardtext statt deutschem")
	assert.NotContains(t, msg, "Status-Update für")
	assert.Contains(t, msg, "Unsubscribe")
}

func TestSubscriberMailSprache_Vorfall_OhneSprachpflegeBleibtDeutsch(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "sl-inc-nolang", "sl-inc-admin2@example.com")
	addEmailSubscriber(t, db, page.ID, "sl-inc-sub2@example.com")
	inc := seedIncident(t, db, page.ID, "Latenz", "Wir prüfen.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessageTo(t, srv, "sl-inc-sub2@example.com")
	assert.Contains(t, msg, "Status-Update für")
}

func TestSubscriberMailSprache_Vorfall_VorlageDerSeitenstandardspracheNichtDeutsch(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "sl-inc-tpl", "sl-inc-admin3@example.com")
	setPageDefaultLanguage(t, db, page.ID, "en")
	addEmailSubscriber(t, db, page.ID, "sl-inc-sub3@example.com")
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceSubscriber, "de", "DE-BETREFF {{.Title}}", "<p>DE-TEXT</p>")
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceSubscriber, "en", "EN-SUBJECT {{.Title}} {{.Language}} {{.StatusLabel}}", "<p>EN-BODY</p>")
	inc := seedIncident(t, db, page.ID, "Latency", "Looking.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessageTo(t, srv, "sl-inc-sub3@example.com")
	assert.Contains(t, msg, "Subject: EN-SUBJECT Latency en Investigating")
	assert.Contains(t, msg, "EN-BODY")
	assert.NotContains(t, msg, "DE-TEXT")
}

func TestSubscriberMailSprache_Vorfall_NurDeutscheVorlageBeiEnglischerSeiteFaelltAufStandardtextZurueck(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "sl-inc-fallback", "sl-inc-admin4@example.com")
	setPageDefaultLanguage(t, db, page.ID, "en")
	addEmailSubscriber(t, db, page.ID, "sl-inc-sub4@example.com")
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceSubscriber, "de", "DE-BETREFF", "<p>DE-TEXT</p>")
	inc := seedIncident(t, db, page.ID, "Latency", "Looking.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessageTo(t, srv, "sl-inc-sub4@example.com")
	assert.NotContains(t, msg, "DE-TEXT", "Vorlage einer fremden Sprache wird nicht benutzt")
	assert.Contains(t, msg, "Subject: [ALERT]")
}

// --- Ankündigungen ----------------------------------------------------------

func noticeFor(pageID uint) models.GeneralNotice {
	return models.GeneralNotice{StatusPageID: pageID, Title: "Hinweis", Message: "Text", Level: "critical", IsActive: true, NotifySubscribers: true}
}

func TestSubscriberMailSprache_Ankuendigung_SeitenstandardEnglisch_Standardtext(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "sl-notice-default")
	setPageDefaultLanguage(t, h.DB, page.ID, "en")

	h.DispatchNoticeNotifications(noticeFor(page.ID), "created")

	msg := waitForEmail(t, srv)
	assert.Contains(t, msg, "Critical")
	assert.Contains(t, msg, "View status page")
	assert.Contains(t, msg, "Unsubscribe")
	assert.NotContains(t, msg, "Kritisch")
	assert.NotContains(t, msg, "Statusseite ansehen")
}

func TestSubscriberMailSprache_Ankuendigung_Deutsch_Standardtext(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "sl-notice-de")

	h.DispatchNoticeNotifications(noticeFor(page.ID), "created")

	msg := waitForEmail(t, srv)
	assert.Contains(t, msg, "Kritisch")
	assert.Contains(t, msg, "Statusseite ansehen")
	assert.Contains(t, msg, "Abbestellen")
}

func TestSubscriberMailSprache_Ankuendigung_VorlageDerSeitenstandardsprache(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "sl-notice-tpl")
	setPageDefaultLanguage(t, h.DB, page.ID, "en")
	seedTemplate(t, h.DB, page.ID, handlers.NotificationKindNoticeCreated, handlers.NotificationAudienceSubscriber, "de", "DE {{.Title}}", "<p>DE-NOTICE</p>")
	seedTemplate(t, h.DB, page.ID, handlers.NotificationKindNoticeCreated, handlers.NotificationAudienceSubscriber, "en", "EN {{.Title}} {{.Language}}", "<p>EN-NOTICE</p>")

	h.DispatchNoticeNotifications(noticeFor(page.ID), "created")

	msg := waitForEmail(t, srv)
	assert.Contains(t, msg, "Subject: EN Hinweis en")
	assert.Contains(t, msg, "EN-NOTICE")
	assert.NotContains(t, msg, "DE-NOTICE")
}

// --- Abo-Bestätigung --------------------------------------------------------

func TestSubscriberMailSprache_Bestaetigung_SeitenstandardEnglisch(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.com")
	db, r, page := setupDOIRouter(t)
	setPageDefaultLanguage(t, db, page.ID, "en")

	w := subscribeEmail(r, "en-confirm@example.com", nil)
	requireStatusCode(t, w, http.StatusOK)

	msg := awaitSMTPMessage(t, srv)
	assert.Contains(t, msg, "Subject: Please confirm your subscription for DOI Page")
	assert.Contains(t, msg, "Confirm subscription")
	assert.Contains(t, msg, "Someone entered this email address")
	assert.Contains(t, msg, "https://status.example.com/subscribe/confirm?token=")
	assert.NotContains(t, msg, "Bitte bestätige")
}

func TestSubscriberMailSprache_Bestaetigung_VorlageDerSeitenstandardsprache(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db, r, page := setupDOIRouter(t)
	setPageDefaultLanguage(t, db, page.ID, "en")
	seedTemplate(t, db, page.ID, handlers.NotificationKindSubscriptionConfirm, handlers.NotificationAudienceSubscriber, "de", "DE-CONFIRM", "<p>DE-BODY {{.ConfirmURL}}</p>")
	seedTemplate(t, db, page.ID, handlers.NotificationKindSubscriptionConfirm, handlers.NotificationAudienceSubscriber, "en", "EN-CONFIRM {{.Language}}", "<p>EN-BODY {{.ConfirmURL}}</p>")

	requireStatusCode(t, subscribeEmail(r, "en-tpl@example.com", nil), http.StatusOK)

	msg := awaitSMTPMessage(t, srv)
	assert.Contains(t, msg, "Subject: EN-CONFIRM en")
	assert.Contains(t, msg, "EN-BODY")
	assert.NotContains(t, msg, "DE-BODY")
}

func TestSubscriberMailSprache_Bestaetigung_DeutschBleibtDeutsch(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	_, r, _ := setupDOIRouter(t)
	requireStatusCode(t, subscribeEmail(r, "de-confirm@example.com", nil), http.StatusOK)
	msg := awaitSMTPMessage(t, srv)
	assert.Contains(t, msg, "Subject: Bitte bestätige dein Abo für DOI Page")
	assert.Contains(t, msg, "Abonnement bestätigen")
}

// --- Externe Meldungen ------------------------------------------------------

func TestSubscriberMailSprache_Extern_VorlageDerSeitenstandardspracheNichtDeEn(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	setPageDefaultLanguage(t, s.db, s.page.ID, "fr")
	for _, lang := range []string{"de", "fr"} {
		seedTemplate(t, s.db, s.page.ID, "external_incident_created", handlers.NotificationAudienceSubscriber, lang, "SUJET-"+lang+" {{.Title}}", "<p>CORPS-"+lang+"</p>")
	}

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "Wir schauen"))
	s.poll()
	m := drainMails(t, srv, 1)[0]
	assert.Contains(t, m.subject, "SUJET-fr")
	assert.Contains(t, m.body, "CORPS-fr")
	assert.NotContains(t, m.body, "CORPS-de")
}
