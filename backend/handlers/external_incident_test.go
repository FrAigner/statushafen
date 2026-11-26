package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Part B2: mirrored incidents and maintenances,
// subscriber notification and read API.

const fixtureBase = "https://status.example.com"

type incidentSetup struct {
	*extFixture
	srcID uint
	vm    models.ExternalComponent
}

// newIncidentSetup: source with linked external component "vm" <-> f.comp,
// initial import (empty) already done.
func newIncidentSetup(t *testing.T) *incidentSetup {
	t.Helper()
	f := newExtFixture(t)
	f.clock = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	srcID := f.createSource(f.page, f.adminTok, fixtureBase)
	vm := f.extComp(srcID, "vm", "Service 03", "operational")
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.pageAdminTok, f.comp.ID, vm.ID).Code)
	f.provider.sum = &externalstatus.Summary{Components: []externalstatus.Component{{ExternalID: "vm", Name: "Service 03", Status: "operational", RawStatus: "operational"}}}
	f.provider.feeds = map[string][]externalstatus.Incident{}
	s := &incidentSetup{extFixture: f, srcID: srcID, vm: vm}
	require.True(t, s.poll().OK)
	return s
}

func (s *incidentSetup) poll() externalstatus.FetchResult {
	s.t.Helper()
	s.clock = s.clock.Add(5 * time.Minute)
	var src models.ExternalSource
	require.NoError(s.t, s.db.First(&src, s.srcID).Error)
	res := s.h.ExternalStatus.FetchNow(context.Background(), src)
	s.h.WaitIncidentMails()
	return res
}

func extInc(id, status string, bodies ...string) externalstatus.Incident {
	st, closed := externalstatus.NormalizeIncidentStatus(models.ExternalIncidentKindIncident, status)
	t0 := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	upd := t0.Add(time.Duration(len(bodies)) * time.Minute)
	it := externalstatus.Incident{
		ExternalID: id, Kind: models.ExternalIncidentKindIncident, Title: "Region B API errors " + id, Status: st, RawStatus: status,
		Impact: "major", Closed: closed, URL: fixtureBase + "/incidents/" + id, StartedAt: &t0, UpdatedAt: &upd,
		Components: []externalstatus.IncidentComponent{{ExternalID: "vm", Name: "Service 03"}},
	}
	for i, b := range bodies {
		it.Updates = append(it.Updates, externalstatus.IncidentUpdate{ExternalID: id + "-" + string(rune('a'+i)), Status: st, Body: b, DisplayAt: t0.Add(time.Duration(i) * time.Minute)})
	}
	return it
}

func (s *incidentSetup) setFeed(kind string, items ...externalstatus.Incident) {
	s.provider.mu.Lock()
	defer s.provider.mu.Unlock()
	s.provider.feeds[kind] = items
}

func (s *incidentSetup) subscriber(email string, componentIDs string) models.Subscriber {
	s.t.Helper()
	sub := models.Subscriber{StatusPageID: s.page.ID, Email: email, Channel: "email", SubscribedComponentIDs: componentIDs, Status: models.SubscriberStatusActive}
	require.NoError(s.t, s.db.Create(&sub).Error)
	return sub
}

func TestExternalIncidentMail_ErstimportStillDannMailUndIdempotent(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	s.subscriber("api@example.com", uid(s.comp.ID))
	s.subscriber("andere@example.com", uid(s.otherComp.ID)) // subscribes to another component only
	pending := models.Subscriber{StatusPageID: s.page.ID, Email: "offen@example.com", Channel: "email", SubscribedComponentIDs: "all", Status: models.SubscriberStatusPending}
	require.NoError(t, s.db.Create(&pending).Error)
	foreign := models.Subscriber{StatusPageID: s.other.ID, Email: "fremd@example.com", Channel: "email", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive}
	require.NoError(t, s.db.Create(&foreign).Error)

	// The source already has an open incident before we ever polled it (initial import of this feed).
	require.NoError(t, s.db.Model(&models.ExternalSource{}).Where("id = ?", s.srcID).Updates(map[string]any{"IncidentsSyncedAt": nil, "MaintenancesSyncedAt": nil}).Error)
	s.setFeed(models.ExternalIncidentKindIncident, extInc("alt1", "investigating", "Wir prüfen"))
	require.True(t, s.poll().OK)
	drainMails(t, srv, 0)
	assert.EqualValues(t, 0, s.auditCount("external_incident.notified"), "Erstimport: still")
	assert.EqualValues(t, 1, s.auditCount("external_incident.mirrored"), "aber gespiegelt und protokolliert")

	// New incident after the initial import: mail to the right subscribers.
	s.setFeed(models.ExternalIncidentKindIncident, extInc("alt1", "investigating", "Wir prüfen"), extInc("fx0000000062", "investigating", "Seit 10:00 Uhr gestört"))
	require.True(t, s.poll().OK)
	mails := drainMails(t, srv, 2)
	assert.ElementsMatch(t, []string{"alle@example.com", "api@example.com"}, recipients(mails))
	for _, m := range mails {
		assert.Contains(t, m.subject, "[EXTERN]")
		assert.Contains(t, m.subject, "Region B API errors fx0000000062")
		assert.Contains(t, m.subject, "Quelle: Example Cloud")
		assert.Contains(t, m.body, "Quelle")
		assert.Contains(t, m.body, "Example Cloud")
		assert.Contains(t, m.body, fixtureBase+"/incidents/fx0000000062", "Link auf die Original-Meldung")
		assert.Contains(t, m.body, "Seit 10:00 Uhr gestört")
		assert.Contains(t, m.body, "API", "betroffene eigene Komponente")
		assert.Contains(t, m.body, "/unsubscribe?token=")
	}
	assert.EqualValues(t, 1, s.auditCount("external_incident.notified"))
	assert.EqualValues(t, 2, s.auditCount("external_incident.mirrored"))
	var notified models.AuditLog
	require.NoError(t, s.db.Where("action = ?", "external_incident.notified").First(&notified).Error)
	assert.Contains(t, notified.Details, "sent=2 failed=0")
	assert.Equal(t, "ExternalIncident", notified.TargetType)

	// Polling twice without a change: no second mail.
	require.True(t, s.poll().OK)
	require.True(t, s.poll().OK)
	drainMails(t, srv, 0)
	assert.EqualValues(t, 1, s.auditCount("external_incident.notified"))
}

func TestExternalIncidentMail_VerlaufUpdateUndBehebung(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "a"))
	s.poll()
	assert.Contains(t, drainMails(t, srv, 1)[0].body, "Eine externe Quelle meldet einen Vorfall")

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "identified", "a", "Ursache gefunden"))
	s.poll()
	m := drainMails(t, srv, 1)[0]
	assert.Contains(t, m.body, "Aktualisierung zu einem Vorfall")
	assert.Contains(t, m.body, "Ursache gefunden")
	assert.Contains(t, m.subject, "Identifiziert")

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "resolved", "a", "Ursache gefunden", "Behoben"))
	s.poll()
	m = drainMails(t, srv, 1)[0]
	assert.Contains(t, m.body, "Vorfall einer externen Quelle ist behoben")
	assert.Contains(t, m.subject, "Behoben")
}

func TestExternalIncidentMail_Wartung(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")

	m := extInc("fx0000000062", "scheduled", "Geplante Wartung")
	m.Kind, m.Status, m.RawStatus, m.Impact = models.ExternalIncidentKindMaintenance, "scheduled", "scheduled", "maintenance"
	sched := time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC)
	m.ScheduledAt = &sched
	s.setFeed(models.ExternalIncidentKindMaintenance, m)
	s.poll()
	mail := drainMails(t, srv, 1)[0]
	assert.Contains(t, mail.body, "eine Wartung angekündigt")
	assert.Contains(t, mail.subject, "Geplant")

	var row models.ExternalIncident
	require.NoError(t, s.db.Where("external_id = ?", "fx0000000062").First(&row).Error)
	require.NotNil(t, row.ScheduledFor)
	assert.True(t, row.ScheduledFor.Equal(sched))
}

// Regression test: update texts from the third-party site are multi-line. The CR/LF protection
// of mail delivery discards mails with a line break in the body - which would have
// silently broken the notification on every multi-line update (so almost always).
func TestExternalIncidentMail_MehrzeiligerTextUndEntschaerfung(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")

	it := extInc("i1", "investigating", "Zeile 1\nZeile 2\r\n<script>alert(1)</script>")
	it.Title = "Böser Titel\r\nBcc: x@example.com"
	s.setFeed(models.ExternalIncidentKindIncident, it)
	require.NoError(t, s.db.Model(&models.ExternalSource{}).Where("id = ?", s.srcID).Update("name", "Quelle <b>fett</b>\nBcc: y@example.com").Error)
	s.poll()

	m := drainMails(t, srv, 1)[0]
	assert.NotContains(t, m.subject, "\n")
	assert.Contains(t, m.subject, "Böser Titel  Bcc: x@example.com", "CR/LF im Betreff durch Leerzeichen ersetzt")
	assert.Contains(t, m.body, "Zeile 1<br>Zeile 2<br>")
	assert.NotContains(t, m.body, "<script>")
	assert.Contains(t, m.body, "&lt;script&gt;")
	assert.NotContains(t, m.body, "<b>fett</b>")
	assert.Contains(t, m.body, "Quelle &lt;b&gt;fett&lt;/b&gt;")
}

func TestExternalIncidentMail_EnglischeSeite(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	require.NoError(t, s.db.Create(&models.Language{Code: "en", Name: "English", IsActive: true}).Error)
	require.NoError(t, s.db.Create(&models.StatusPageLanguage{StatusPageID: s.page.ID, LanguageCode: "en", IsDefault: true}).Error)

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "We are looking into it"))
	s.poll()
	m := drainMails(t, srv, 1)[0]
	assert.Contains(t, m.subject, "[EXTERNAL]")
	assert.Contains(t, m.subject, "source: Example Cloud")
	assert.Contains(t, m.body, "An external source reports an incident")
	assert.Contains(t, m.body, "Source")
	assert.Contains(t, m.body, "View at the source")
}

func TestExternalIncidentMail_EigeneVorlageMitQuellenPlatzhaltern(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	require.NoError(t, s.db.Create(&models.NotificationTemplate{
		StatusPageID: s.page.ID, Kind: "external_incident_created", Audience: "subscriber", Language: "de",
		Subject:  "Fremdstörung {{.Title}} bei {{.SourceName}}",
		BodyHTML: `<p>{{.StatusLabel}}: {{.Message}}</p><a href="{{.SourceURL}}">{{.SourceName}}</a> {{.ComponentNames}} {{.Action}}`,
	}).Error)

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "Zeile 1\nZeile 2"))
	s.poll()
	m := drainMails(t, srv, 1)[0]
	assert.Equal(t, "Fremdstörung Region B API errors i1 bei Example Cloud", m.subject)
	assert.Contains(t, m.body, "Untersuchung: Zeile 1 Zeile 2")
	assert.Contains(t, m.body, `href="`+fixtureBase+`/incidents/i1"`)
	assert.Contains(t, m.body, "API created")
}

func TestExternalIncidentMail_EmailSchalterAus(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	disableEmail(t, s.db)

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "a"))
	s.poll()
	drainMails(t, srv, 0)
	assert.EqualValues(t, 1, s.auditCount("email.skipped_disabled"))
	assert.EqualValues(t, 0, s.auditCount("external_incident.notified"))
	assert.EqualValues(t, 0, s.auditCount("external_incident.failed"))
	assert.EqualValues(t, 1, s.auditCount("external_incident.mirrored"))
}

func TestExternalIncidentMail_VersandfehlerWirdProtokolliert(t *testing.T) {
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "1") // nobody is listening
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	s.subscriber("kaputt", "all") // invalid address

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "a"))
	s.poll()
	assert.EqualValues(t, 2, s.auditCount("external_incident.failed"), "je Empfänger")
	var failed models.AuditLog
	require.NoError(t, s.db.Where("action = ? AND details LIKE ?", "external_incident.failed", "%invalid recipient%").First(&failed).Error)
	assert.Contains(t, failed.Details, "channel=email")
	var notified models.AuditLog
	require.NoError(t, s.db.Where("action = ?", "external_incident.notified").First(&notified).Error)
	assert.Contains(t, notified.Details, "sent=0 failed=2")
}

// The site address only acts as Reply-To. If it contains a line break,
// it is discarded (no header, no injection); the mail itself goes out
// with the operator sender.
func TestExternalIncidentMail_ReplyToMitZeilenumbruchWirdVerworfen(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	require.NoError(t, s.db.Model(&models.StatusPage{}).Where("id = ?", s.page.ID).
		Update("notification_from_address", "Evil <a@example.com>\nBcc: x@example.com").Error)
	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "a"))
	s.poll()
	msg := awaitSMTPMessage(t, srv)
	assert.Empty(t, headerLine(msg, "Bcc"))
	assert.Empty(t, headerLine(msg, "Reply-To"))
	assert.EqualValues(t, 0, s.auditCount("external_incident.failed"))
}

func TestExternalIncidentWebhook_FehlerPfade(t *testing.T) {
	s := newIncidentSetup(t)
	// Webhook on loopback: the webhook client's SSRF protection rejects it.
	require.NoError(t, s.db.Create(&models.Subscriber{StatusPageID: s.page.ID, Channel: "google_chat", WebhookURL: "http://127.0.0.1:9/hook",
		SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive}).Error)
	// Google Chat subscription without webhook URL.
	require.NoError(t, s.db.Exec("INSERT INTO subscribers (status_page_id, channel, subscribed_component_ids, status, created_at, updated_at) VALUES (?, 'google_chat', 'all', 'active', datetime('now'), datetime('now'))", s.page.ID).Error)

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "a"))
	s.poll()
	assert.EqualValues(t, 2, s.auditCount("external_incident.failed"))
}

func TestExternalIncidentMail_PrivateSeiteNurBerechtigteAbonnenten(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	require.NoError(t, s.db.Model(&models.StatusPage{}).Where("id = ?", s.page.ID).Update("is_private", true).Error)
	s.subscriber("ohne-konto@example.com", "all") // no creator with read permission

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "a"))
	s.poll()
	drainMails(t, srv, 0)
	assert.EqualValues(t, 0, s.auditCount("external_incident.notified"))
}

// ---- API ------------------------------------------------------------------

func (s *incidentSetup) incidentsPath(p models.StatusPage, query string) string {
	path := "/api/pages/" + pid(p) + "/external-incidents"
	if query != "" {
		path += "?" + query
	}
	return path
}

func (s *incidentSetup) list(tok, query string) []map[string]any {
	s.t.Helper()
	w := s.do(http.MethodGet, s.incidentsPath(s.page, query), tok, nil)
	require.Equal(s.t, http.StatusOK, w.Code, w.Body.String())
	var out []map[string]any
	require.NoError(s.t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func TestExternalIncidentsAPI_FormatUndFilter(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)

	open := extInc("open1", "monitoring", "Erstes", "Zweites")
	done := extInc("done1", "resolved", "Behoben")
	unrelated := extInc("fremd", "investigating", "x")
	unrelated.Components = []externalstatus.IncidentComponent{{ExternalID: "ungekoppelt", Name: "Anderes"}}
	s.setFeed(models.ExternalIncidentKindIncident, open, done, unrelated)
	maint := extInc("m1", "scheduled", "Geplant")
	maint.Kind, maint.Status, maint.Impact = models.ExternalIncidentKindMaintenance, "scheduled", "maintenance"
	s.setFeed(models.ExternalIncidentKindMaintenance, maint)
	s.poll()

	items := s.list(s.viewerTok, "")
	require.Len(t, items, 3, "ohne gekoppelte Komponente nicht gelistet")
	assert.Equal(t, "open1", items[0]["external_id"], "offene zuerst")
	first := items[0]
	assert.Equal(t, "Example Cloud", first["source_name"])
	assert.Equal(t, fixtureBase, first["source_url"])
	assert.Equal(t, "incident", first["kind"])
	assert.Equal(t, "monitoring", first["status"])
	assert.Equal(t, "major", first["impact"])
	assert.Equal(t, false, first["closed"])
	assert.Equal(t, fixtureBase+"/incidents/open1", first["url"])
	assert.NotNil(t, first["started_at"])
	assert.NotNil(t, first["updated_at"])
	assert.Nil(t, first["resolved_at"])
	affected := first["affected_components"].([]any)
	require.Len(t, affected, 1)
	assert.Equal(t, "API", affected[0].(map[string]any)["name"])
	assert.EqualValues(t, s.comp.ID, affected[0].(map[string]any)["id"])
	ext := first["external_components"].([]any)
	require.Len(t, ext, 1)
	assert.Equal(t, "vm", ext[0].(map[string]any)["external_id"])
	updates := first["updates"].([]any)
	require.Len(t, updates, 2)
	assert.Equal(t, "Zweites", updates[0].(map[string]any)["body"], "neuestes Update zuerst")

	assert.Len(t, s.list(s.viewerTok, "state=open"), 2)
	closed := s.list(s.viewerTok, "state=closed")
	require.Len(t, closed, 1)
	assert.Equal(t, "done1", closed[0]["external_id"])
	maints := s.list(s.viewerTok, "kind=maintenance")
	require.Len(t, maints, 1)
	assert.Equal(t, "maintenance", maints[0]["kind"])
	assert.Len(t, s.list(s.viewerTok, "limit=1"), 1)
	assert.Len(t, s.list(s.viewerTok, "source_id="+uid(s.srcID)), 3)
	assert.Len(t, s.list(s.viewerTok, "source_id=999"), 0)

	// Finished ones outside the window drop out, open ones stay.
	old := s.clock.AddDate(0, 0, -60)
	require.NoError(t, s.db.Model(&models.ExternalIncident{}).Where("external_id = ?", "done1").
		Updates(map[string]any{"resolved_at": old, "external_updated_at": old}).Error)
	assert.Len(t, s.list(s.viewerTok, ""), 2)
	assert.Len(t, s.list(s.viewerTok, "days=90"), 3)

	// Einzelabruf.
	var row models.ExternalIncident
	require.NoError(t, s.db.Where("external_id = ?", "open1").First(&row).Error)
	w := s.do(http.MethodGet, s.incidentsPath(s.page, "")+"/"+uid(row.ID), s.viewerTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var one map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &one))
	assert.Equal(t, "open1", one["external_id"])
}

func TestExternalIncidentsAPI_Fehlerfaelle(t *testing.T) {
	s := newIncidentSetup(t)
	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "a"))
	s.poll()
	var row models.ExternalIncident
	require.NoError(t, s.db.First(&row).Error)

	for _, q := range []string{"kind=foo", "state=foo", "source_id=abc", "source_id=0", "days=0", "days=366", "days=x", "limit=0", "limit=201", "limit=x"} {
		w := s.do(http.MethodGet, s.incidentsPath(s.page, q), s.viewerTok, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code, q)
	}
	assert.Equal(t, http.StatusBadRequest, s.do(http.MethodGet, "/api/pages/abc/external-incidents", s.adminTok, nil).Code)
	assert.Equal(t, http.StatusBadRequest, s.do(http.MethodGet, "/api/pages/abc/external-incidents/1", s.adminTok, nil).Code)
	assert.Equal(t, http.StatusBadRequest, s.do(http.MethodGet, s.incidentsPath(s.page, "")+"/abc", s.viewerTok, nil).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodGet, s.incidentsPath(s.page, "")+"/99999", s.viewerTok, nil).Code)
}

func TestExternalIncidentsAPI_RechteUndMandanten(t *testing.T) {
	s := newIncidentSetup(t)
	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "a"))
	s.poll()
	var row models.ExternalIncident
	require.NoError(t, s.db.First(&row).Error)
	detail := s.incidentsPath(s.page, "") + "/" + uid(row.ID)

	// Without login 401, without a role on the site 403.
	assert.Equal(t, http.StatusUnauthorized, s.do(http.MethodGet, s.incidentsPath(s.page, ""), "", nil).Code)
	assert.Equal(t, http.StatusForbidden, s.do(http.MethodGet, s.incidentsPath(s.page, ""), s.otherTok, nil).Code)
	assert.Equal(t, http.StatusForbidden, s.do(http.MethodGet, detail, s.otherTok, nil).Code)
	// Role without external_sources:read.
	noRead := models.Role{Name: "Nur Vorfälle", Permissions: "incidents:read"}
	require.NoError(t, s.db.Create(&noRead).Error)
	tok := s.user("incidents-only@example.com", false, map[uint]uint{s.page.ID: noRead.ID})
	assert.Equal(t, http.StatusForbidden, s.do(http.MethodGet, s.incidentsPath(s.page, ""), tok, nil).Code)
	assert.Equal(t, http.StatusForbidden, s.do(http.MethodGet, detail, tok, nil).Code)

	// Foreign tenant: The other site's list shows nothing of ours, the
	// single report of our site cannot be found via their path (404),
	// not even for a Global Admin.
	assert.JSONEq(t, "[]", s.do(http.MethodGet, s.incidentsPath(s.other, ""), s.otherTok, nil).Body.String())
	otherDetail := s.incidentsPath(s.other, "") + "/" + uid(row.ID)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodGet, otherDetail, s.otherTok, nil).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodGet, otherDetail, s.adminTok, nil).Code)
	assert.Equal(t, http.StatusOK, s.do(http.MethodGet, detail, s.pageAdminTok, nil).Code)
}

func TestExternalIncidentsAPI_EntkoppelnBlendetAus(t *testing.T) {
	s := newIncidentSetup(t)
	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "a"))
	s.poll()
	require.Len(t, s.list(s.viewerTok, ""), 1)
	var row models.ExternalIncident
	require.NoError(t, s.db.First(&row).Error)

	var link models.ExternalComponentLink
	require.NoError(t, s.db.First(&link).Error)
	w := s.do(http.MethodDelete, s.linksPath(s.page)+"/"+uid(link.ID), s.pageAdminTok, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Empty(t, s.list(s.viewerTok, ""))
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodGet, s.incidentsPath(s.page, "")+"/"+uid(row.ID), s.viewerTok, nil).Code, "ohne Kopplung nicht abrufbar")
}

func TestExternalIncidentsAPI_QuelleDeaktiviertOderGeloescht(t *testing.T) {
	s := newIncidentSetup(t)
	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "a"))
	s.poll()
	require.NoError(t, s.db.Model(&models.ExternalSource{}).Where("id = ?", s.srcID).Update("active", false).Error)
	assert.Empty(t, s.list(s.viewerTok, ""), "inaktive Quellen werden nicht gezeigt")
	require.NoError(t, s.db.Model(&models.ExternalSource{}).Where("id = ?", s.srcID).Update("active", true).Error)
	require.Len(t, s.list(s.viewerTok, ""), 1)

	w := s.do(http.MethodDelete, s.sourcesPath(s.page)+"/"+uid(s.srcID), s.pageAdminTok, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var n int64
	s.db.Model(&models.ExternalIncident{}).Count(&n)
	assert.Zero(t, n, "gespiegelte Meldungen verschwinden mit der Quelle")
	s.db.Model(&models.ExternalIncidentUpdate{}).Count(&n)
	assert.Zero(t, n)
	s.db.Model(&models.ExternalIncidentComponent{}).Count(&n)
	assert.Zero(t, n)
	assert.Empty(t, s.list(s.viewerTok, ""))
}

func TestPublicDashboard_ExternalIncidents(t *testing.T) {
	s := newIncidentSetup(t)
	s.setFeed(models.ExternalIncidentKindIncident, extInc("open1", "investigating", "a"), extInc("done1", "resolved", "b"))
	s.poll()
	// done1 was already finished when it first appeared and is visible anyway.

	dash := func() []any {
		w := s.do(http.MethodGet, "/api/public/pages/eigene/dashboard", "", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body["external_incidents"].([]any)
	}
	items := dash()
	require.Len(t, items, 2)
	assert.Equal(t, "open1", items[0].(map[string]any)["external_id"])
	assert.Equal(t, "Example Cloud", items[0].(map[string]any)["source_name"])

	old := s.clock.AddDate(0, 0, -20)
	require.NoError(t, s.db.Model(&models.ExternalIncident{}).Where("external_id = ?", "done1").
		Updates(map[string]any{"resolved_at": old, "external_updated_at": old}).Error)
	assert.Len(t, dash(), 1, "beendete nach 14 Tagen nicht mehr")

	// The other site sees none of this.
	w := s.do(http.MethodGet, "/api/public/pages/fremde/dashboard", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Empty(t, body["external_incidents"])
}

func TestExternalIncidents_FeedFehlerLaesstKomponentenAbrufGelingen(t *testing.T) {
	s := newIncidentSetup(t)
	s.provider.feedErr = errors.New("HTTP 502 von " + fixtureBase)
	res := s.poll()
	require.True(t, res.OK, res.Error)
	w := s.do(http.MethodGet, s.sourcesPath(s.page)+"/"+uid(s.srcID), s.viewerTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var src map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &src))
	assert.Contains(t, src["incidents_error"], "HTTP 502")
	assert.NotNil(t, src["incidents_synced_at"], "Erstimport des früheren Abrufs bleibt erhalten")
}

// ---- Vorlagen ---------------------------------------------------------------

func TestNotificationTemplates_ExternalKinds(t *testing.T) {
	f := newExtFixture(t)
	r := f.r
	f.h.DB = f.db
	r.PUT("/api/pages/:page_id/notification-templates/:kind/:audience/:language", f.h.PutNotificationTemplateHandler)
	r.POST("/api/pages/:page_id/notification-templates/:kind/:audience/:language/preview", f.h.PreviewNotificationTemplateHandler)

	for _, kind := range []string{"external_incident_created", "external_incident_updated", "external_incident_resolved",
		"external_maintenance_created", "external_maintenance_updated", "external_maintenance_resolved"} {
		base := "/api/pages/" + pid(f.page) + "/notification-templates/" + kind + "/subscriber/de"
		body := map[string]string{"Subject": "{{.SourceName}}: {{.Title}}", "BodyHTML": `<a href="{{.SourceURL}}">{{.SourceName}}</a> {{.Action}}`}
		w := f.do(http.MethodPost, base+"/preview", f.adminTok, body)
		require.Equal(t, http.StatusOK, w.Code, kind+": "+w.Body.String())
		var prev map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &prev))
		assert.True(t, strings.HasPrefix(prev["subject"], "Example Cloud: "), kind)
		assert.Contains(t, prev["bodyHTML"], "https://status.example.com/incidents/", kind)
		assert.Equal(t, http.StatusOK, f.do(http.MethodPut, base, f.adminTok, body).Code, kind)
	}
	w := f.do(http.MethodPut, "/api/pages/"+pid(f.page)+"/notification-templates/external_bogus/subscriber/de", f.adminTok,
		map[string]string{"Subject": "x", "BodyHTML": "y"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
