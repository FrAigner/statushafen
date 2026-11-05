package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ausfall-Meldung, Gruppen-Kopplung, Abruf-Sperre.

var errBoom = errors.New("HTTP 503 von https://status.example.com")

// outageSetup legt eine Quelle samt gekoppelter Komponente an (externer Status
// major_outage), setzt den letzten Erfolg 50 Minuten vor die Fake-Uhr und lässt
// den Anbieter fehlschlagen.
func outageSetup(t *testing.T) (*extFixture, uint) {
	t.Helper()
	f := newExtFixture(t)
	f.clock = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	srcID := f.createSource(f.page, f.adminTok, "https://status.example.com")
	vm := f.extComp(srcID, "vm", "VM", "major_outage")
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.pageAdminTok, f.comp.ID, vm.ID).Code)
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", srcID).
		Update("last_success_at", f.clock.Add(-50*time.Minute)).Error)
	f.provider.err = errBoom
	return f, srcID
}

func (f *extFixture) fetchAt(srcID uint, at time.Time) externalstatus.FetchResult {
	f.t.Helper()
	f.clock = at
	var src models.ExternalSource
	require.NoError(f.t, f.db.First(&src, srcID).Error)
	res := f.h.ExternalStatus.FetchNow(context.Background(), src)
	f.h.WaitOutageMails()
	return res
}

// mails sammelt die bis jetzt zugestellten Mails (Empfänger → Betreff, Text).
type testMail struct{ to, subject, body string }

func drainMails(t *testing.T, srv *fakeSMTPServer, want int) []testMail {
	t.Helper()
	var out []testMail
	timeout := time.After(3 * time.Second)
	for len(out) < want {
		select {
		case raw := <-srv.dataCh:
			out = append(out, parseMail(t, raw))
		case <-timeout:
			t.Fatalf("erwartet %d Mails, erhalten %d", want, len(out))
		}
	}
	// Weitere unerwünschte Mails treffen kurz danach ein.
	select {
	case raw := <-srv.dataCh:
		t.Fatalf("unerwartete zusätzliche Mail: %s", raw)
	case <-time.After(150 * time.Millisecond):
	}
	return out
}

func parseMail(t *testing.T, raw string) testMail {
	t.Helper()
	m := testMail{}
	head, body, _ := strings.Cut(raw, "\n\n")
	for _, line := range strings.Split(head, "\n") {
		switch {
		case strings.HasPrefix(line, "To: "):
			m.to = strings.TrimPrefix(line, "To: ")
		case strings.HasPrefix(line, "Subject: "):
			subj, err := new(mime.WordDecoder).DecodeHeader(strings.TrimPrefix(line, "Subject: "))
			require.NoError(t, err)
			m.subject = subj
		}
	}
	m.body = body
	return m
}

func recipients(mails []testMail) []string {
	out := make([]string, 0, len(mails))
	for _, m := range mails {
		out = append(out, strings.TrimSpace(m.to))
	}
	return out
}

func (f *extFixture) auditCount(action string) int64 {
	var n int64
	f.db.Model(&models.AuditLog{}).Where("action = ?", action).Count(&n)
	return n
}

func TestOutageMail_EmpfaengerkreisUndSprache(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f, srcID := outageSetup(t)
	require.NoError(t, f.db.Model(&models.User{}).Where("email = ?", "pa@example.com").Update("preferred_language", "en-GB").Error)

	f.fetchAt(srcID, f.clock.Add(10*time.Minute))

	mails := drainMails(t, srv, 2)
	assert.ElementsMatch(t, []string{"admin@example.com", "pa@example.com"}, recipients(mails),
		"Global Admin und Page Admin, nicht der Viewer und nicht der Admin der anderen Seite")
	for _, m := range mails {
		switch strings.TrimSpace(m.to) {
		case "admin@example.com":
			assert.Equal(t, "Externe Quelle „Example Cloud“ nicht erreichbar", m.subject)
			assert.Contains(t, m.body, "Keine Daten seit")
			assert.Contains(t, m.body, "29.10.2026 11:10 UTC")
		case "pa@example.com":
			assert.Equal(t, `External source "Example Cloud" unreachable`, m.subject)
			assert.Contains(t, m.body, "No data since")
			assert.Contains(t, m.body, "2026-10-29 11:10 UTC")
		}
		assert.Contains(t, m.body, "HTTP 503")
		assert.Contains(t, m.body, "Eigene", "Name der Statusseite")
	}
	assert.EqualValues(t, 2, f.auditCount("external_source.outage_email_sent"))
}

func TestOutageMail_NurEinmalErinnerungEntwarnung(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f, srcID := outageSetup(t)
	start := f.clock

	f.fetchAt(srcID, start.Add(10*time.Minute))
	first := drainMails(t, srv, 2)
	assert.Contains(t, first[0].subject, "nicht erreichbar")

	// Jeder weitere fehlgeschlagene Abruf: keine Mail.
	f.fetchAt(srcID, start.Add(time.Hour))
	f.fetchAt(srcID, start.Add(2*time.Hour))
	drainMails(t, srv, 0)

	// Nach 24 Stunden eine Erinnerung, danach nichts mehr.
	f.fetchAt(srcID, start.Add(10*time.Minute+externalstatus.OutageReminderAfter))
	reminder := drainMails(t, srv, 2)
	assert.Contains(t, reminder[0].subject, "Erinnerung")
	assert.Contains(t, reminder[0].subject, "Example Cloud")
	f.fetchAt(srcID, start.Add(72*time.Hour))
	drainMails(t, srv, 0)

	// Wiederherstellung: Entwarnung.
	f.provider.err = nil
	f.provider.sum = &externalstatus.Summary{Components: []externalstatus.Component{{ExternalID: "vm", Name: "VM", Status: "operational", RawStatus: "operational"}}}
	f.fetchAt(srcID, start.Add(73*time.Hour))
	recovered := drainMails(t, srv, 2)
	assert.Equal(t, "Externe Quelle „Example Cloud“ wieder erreichbar", recovered[0].subject)
	assert.EqualValues(t, 6, f.auditCount("external_source.outage_email_sent"))
}

func TestOutageMail_EmailSchalterAus(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f, srcID := outageSetup(t)
	disableEmail(t, f.db)

	f.fetchAt(srcID, f.clock.Add(10*time.Minute))

	drainMails(t, srv, 0)
	assert.EqualValues(t, 0, f.auditCount("external_source.outage_email_sent"))
	assert.EqualValues(t, 2, f.auditCount("email.skipped_disabled"), "übersprungen und protokolliert, je Empfänger")
	var entry models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "email.skipped_disabled").First(&entry).Error)
	assert.Equal(t, "ExternalSource", entry.TargetType)
	assert.Equal(t, "Example Cloud", entry.TargetName)

	// Die Admin-Meldung bleibt.
	w := f.do(http.MethodGet, "/api/pages/"+pid(f.page)+"/external-outages", f.viewerTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 1)
	assert.Equal(t, "Example Cloud", list[0]["source_name"])
}

func TestOutageMail_OhneEmpfaenger(t *testing.T) {
	f, srcID := outageSetup(t)
	require.NoError(t, f.db.Where("1 = 1").Delete(&models.UserStatusPageRole{}).Error)
	require.NoError(t, f.db.Where("is_global_admin = ?", true).Delete(&models.User{}).Error)

	f.fetchAt(srcID, f.clock.Add(10*time.Minute))

	assert.EqualValues(t, 1, f.auditCount("external_source.outage_email_skipped"))
	var entry models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "external_source.outage_email_skipped").First(&entry).Error)
	assert.Contains(t, entry.Details, "no_recipients")
}

func TestOutageMail_VersandfehlerUndUngueltigeAdresse(t *testing.T) {
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "1") // niemand lauscht
	f, srcID := outageSetup(t)
	require.NoError(t, f.db.Create(&models.User{Email: "kaputt", IsGlobalAdmin: true}).Error)

	f.fetchAt(srcID, f.clock.Add(10*time.Minute))

	assert.EqualValues(t, 2, f.auditCount("external_source.outage_email_failed"), "Fehler je gültigem Empfänger protokolliert")
	assert.EqualValues(t, 0, f.auditCount("external_source.outage_email_sent"))
}

// Die Seitenadresse wirkt nur als Reply-To; mit Zeilenumbruch wird sie
// verworfen, die Mail geht ohne Reply-To und ohne eingeschleuste Header raus.
func TestOutageMail_ReplyToMitZeilenumbruchWirdVerworfen(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f, srcID := outageSetup(t)
	require.NoError(t, f.db.Model(&models.StatusPage{}).Where("id = ?", f.page.ID).
		Update("notification_from_address", "Evil <a@example.com>\nBcc: x@example.com").Error)
	f.fetchAt(srcID, f.clock.Add(10*time.Minute))
	msg := awaitSMTPMessage(t, srv)
	assert.Empty(t, headerLine(msg, "Bcc"))
	assert.Empty(t, headerLine(msg, "Reply-To"))
	assert.EqualValues(t, 0, f.auditCount("external_source.outage_email_failed"))
}

// Der Link der Ausfall-Mail führte auf /admin/components;
// seit es die Seite "Externe Quellen" gibt, muss er dorthin zeigen.
func TestOutageMail_LinkFuehrtAufQuellenseite(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.org")
	f, srcID := outageSetup(t)

	f.fetchAt(srcID, f.clock.Add(10*time.Minute))

	for _, m := range drainMails(t, srv, 2) {
		body := strings.NewReplacer("=\r\n", "", "=\n", "", "=3D", "=").Replace(m.body)
		assert.Contains(t, body, `href="https://status.example.org/admin/external-sources"`)
		assert.NotContains(t, body, "/admin/components")
		assert.Regexp(t, "Externe Quellen öffnen|Open external sources", body)
	}
}

func TestOutageMail_FeldInhaltWirdEntschaerft(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f, srcID := outageSetup(t)
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", srcID).
		Update("name", "<b>Evil</b>\r\nBcc: x@example.com").Error)
	f.provider.err = errors.New("<script>alert(1)</script>\nzweite Zeile")

	f.fetchAt(srcID, f.clock.Add(10*time.Minute))

	mails := drainMails(t, srv, 2)
	for _, m := range mails {
		assert.NotContains(t, m.subject, "\n")
		assert.Contains(t, m.subject, "<b>Evil</b>  Bcc: x@example.com", "CR/LF im Betreff durch Leerzeichen ersetzt")
		assert.NotContains(t, m.body, "<script>")
		assert.Contains(t, m.body, "&lt;script&gt;")
		assert.NotContains(t, m.body, "<b>Evil</b>")
	}
}

func TestExternalOutages_Banner(t *testing.T) {
	f, srcID := outageSetup(t)
	path := "/api/pages/" + pid(f.page) + "/external-outages"

	w := f.do(http.MethodGet, path, f.viewerTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, "[]", w.Body.String(), "ohne Ausfall eine leere Liste")

	f.fetchAt(srcID, f.clock.Add(10*time.Minute))
	// Zweite, jüngere Störung und eine inaktive Quelle.
	second := f.createSourceNamed(f.page, "Zweite", "https://zweite.example")
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", second).
		Updates(map[string]any{"outage_since": f.clock.Add(-30 * time.Minute), "last_error": "timeout"}).Error)
	inactive := f.createSourceNamed(f.page, "Inaktiv", "https://inaktiv.example")
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", inactive).
		Updates(map[string]any{"outage_since": f.clock.Add(-time.Hour), "active": false}).Error)

	w = f.do(http.MethodGet, path, f.viewerTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 2, "inaktive Quellen werden nicht gemeldet")
	assert.Equal(t, "Example Cloud", list[0]["source_name"], "älteste zuerst")
	assert.Equal(t, "Zweite", list[1]["source_name"])
	assert.Equal(t, "timeout", list[1]["last_error"])
	assert.Contains(t, list[0]["last_error"], "HTTP 503")
	assert.Equal(t, "https://status.example.com", list[0]["base_url"])
	assert.NotNil(t, list[0]["since"])
	assert.NotNil(t, list[0]["notified_at"])
	assert.NotNil(t, list[0]["last_success_at"])

	// Mandantentrennung und Berechtigung.
	assert.Equal(t, http.StatusForbidden, f.do(http.MethodGet, path, f.otherTok, nil).Code)
	other := f.do(http.MethodGet, "/api/pages/"+pid(f.other)+"/external-outages", f.otherTok, nil)
	assert.JSONEq(t, "[]", other.Body.String())
	assert.Equal(t, http.StatusUnauthorized, f.do(http.MethodGet, path, "", nil).Code)
}

func (f *extFixture) createSourceNamed(p models.StatusPage, name, base string) uint {
	f.t.Helper()
	w := f.do(http.MethodPost, f.sourcesPath(p), f.adminTok, map[string]any{"name": name, "base_url": base})
	require.Equal(f.t, http.StatusCreated, w.Code, w.Body.String())
	var resp struct{ ID uint }
	require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.ID
}

func TestExternalSources_OutageFelder(t *testing.T) {
	f, srcID := outageSetup(t)
	srcPath := f.sourcesPath(f.page) + "/" + uid(srcID)

	get := func() map[string]any {
		w := f.do(http.MethodGet, srcPath, f.viewerTok, nil)
		require.Equal(t, http.StatusOK, w.Code)
		var m map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
		return m
	}
	m := get()
	assert.EqualValues(t, 60, m["outage_after_minutes"], "Standard 60")
	assert.Equal(t, map[string]any{"active": false, "since": nil, "last_error": ""}, m["outage"])

	f.fetchAt(srcID, f.clock.Add(10*time.Minute))
	m = get()
	outage := m["outage"].(map[string]any)
	assert.Equal(t, true, outage["active"])
	since, err := time.Parse(time.RFC3339, outage["since"].(string))
	require.NoError(t, err)
	assert.True(t, since.Equal(time.Date(2026, 10, 29, 11, 10, 0, 0, time.UTC)), "Beginn = letzter Erfolg")
	assert.Contains(t, outage["last_error"], "HTTP 503")
	assert.NotContains(t, m, "outage_notified_at")

	// Frist ändern: 10 bis 1440 Minuten.
	for _, bad := range []int{9, 1441, -1} {
		w := f.do(http.MethodPut, srcPath, f.pageAdminTok, map[string]any{"outage_after_minutes": bad})
		assert.Equal(t, http.StatusBadRequest, w.Code, "%d", bad)
	}
	for _, ok := range []int{10, 1440} {
		w := f.do(http.MethodPut, srcPath, f.pageAdminTok, map[string]any{"outage_after_minutes": ok})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.EqualValues(t, ok, get()["outage_after_minutes"])
	}
}

// Veralteter Status im Komponentenbaum: ausgefallen → "unknown" für die Seite.
func TestOutage_KomponentenbaumZeigtAusfallUndFrischenStatus(t *testing.T) {
	f, srcID := outageSetup(t)
	tree := func() *treeNode {
		w := f.do(http.MethodGet, "/api/pages/"+pid(f.page)+"/components", "", nil)
		require.Equal(t, http.StatusOK, w.Code)
		var nodes []treeNode
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &nodes))
		return findNode(nodes, "API")
	}
	assert.Equal(t, "major_outage", tree().EffectiveStatus)

	f.fetchAt(srcID, f.clock.Add(10*time.Minute))
	n := tree()
	assert.Equal(t, "operational", n.EffectiveStatus, "veralteter Status zählt nicht")
	require.Len(t, n.ExternalSources, 1)
	assert.True(t, n.ExternalSources[0].SourceOutage)
	assert.Equal(t, "major_outage", n.ExternalSources[0].Status, "zuletzt bekannter Status bleibt sichtbar")

	f.provider.err = nil
	f.provider.sum = &externalstatus.Summary{Components: []externalstatus.Component{{ExternalID: "vm", Name: "VM", Status: "partial_outage", RawStatus: "partial_outage"}}}
	f.fetchAt(srcID, f.clock.Add(time.Hour))
	n = tree()
	assert.Equal(t, "partial_outage", n.EffectiveStatus)
	assert.False(t, n.ExternalSources[0].SourceOutage)
}

// Kopplung einer ganzen externen Gruppe über die API.
func TestExternalLinks_GanzeGruppeKoppeln(t *testing.T) {
	f := newExtFixture(t)
	srcID := f.createSource(f.page, f.adminTok, "https://status.example.com")
	grp := models.ExternalComponent{ExternalSourceID: srcID, ExternalID: "g1", Name: "Group A", IsGroup: true, Status: "operational", LastSeenAt: time.Now()}
	require.NoError(t, f.db.Create(&grp).Error)
	vm := f.extComp(srcID, "vm", "Service 03", "partial_outage")
	f.extComp(srcID, "k8s", "Kubernetes", "operational")

	w := f.linkVia(f.page, f.pageAdminTok, f.comp.ID, grp.ID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var link map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &link))
	assert.Equal(t, true, link["is_group"])
	assert.Equal(t, "partial_outage", link["status"], "Schlimmstes der Mitglieder, nicht der Eigenstatus der Gruppe")
	assert.Equal(t, "Group A", link["group_name"])
	assert.Equal(t, map[string]any{"name": "Service 03", "status": "partial_outage"}, link["worst"])
	assert.Equal(t, []any{}, link["warnings"])
	assert.Len(t, f.listener.changes, 1)
	assert.Equal(t, "partial_outage", f.listener.changes[0].Current)

	tree := func() *treeNode {
		w := f.do(http.MethodGet, "/api/pages/"+pid(f.page)+"/components", "", nil)
		var nodes []treeNode
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &nodes))
		return findNode(nodes, "API")
	}
	n := tree()
	assert.Equal(t, "partial_outage", n.EffectiveStatus)
	require.Len(t, n.ExternalSources, 1)
	ref := n.ExternalSources[0]
	assert.True(t, ref.IsGroup)
	assert.Equal(t, "Group A", ref.GroupName)
	assert.Equal(t, &models.ExternalWorst{Name: "Service 03", Status: "partial_outage"}, ref.Worst)

	// Neues Mitglied mit schlimmerem Status zählt automatisch.
	neu := f.extComp(srcID, "neu", "Neu", "major_outage")
	assert.Equal(t, "major_outage", tree().EffectiveStatus)
	// Verschwunden zählt nicht mehr.
	require.NoError(t, f.db.Model(&models.ExternalComponent{}).Where("id = ?", neu.ID).Update("vanished", true).Error)
	assert.Equal(t, "partial_outage", tree().EffectiveStatus)

	// Dieselbe Gruppe plus einer ihrer Bausteine: erlaubt, Warnung an beiden Kopplungen.
	w = f.linkVia(f.page, f.pageAdminTok, f.comp.ID, vm.ID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var second map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &second))
	assert.Equal(t, []any{"overlaps_group"}, second["warnings"])
	assert.Equal(t, "partial_outage", tree().EffectiveStatus, "Doppelkopplung ändert das Ergebnis nicht")

	w = f.do(http.MethodGet, f.linksPath(f.page), f.viewerTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 2)
	for _, l := range list {
		assert.Equal(t, []any{"overlaps_group"}, l["warnings"], l["external_component_name"])
	}

	// Bestehende Kopplung erneut: 200 mit derselben Warnung.
	w = f.linkVia(f.page, f.pageAdminTok, f.comp.ID, vm.ID)
	assert.Equal(t, http.StatusOK, w.Code)

	// Ein anderer Baustein oder eine andere Komponente: keine Warnung.
	other := f.extComp(srcID, "x", "Andere Gruppe", "operational")
	require.NoError(t, f.db.Model(&models.ExternalComponent{}).Where("id = ?", other.ID).Update("group_external_id", "g2").Error)
	w = f.linkVia(f.page, f.pageAdminTok, f.comp.ID, other.ID)
	var third map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &third))
	assert.Equal(t, []any{}, third["warnings"])
	w = f.linkVia(f.page, f.pageAdminTok, f.child.ID, vm.ID)
	var child map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &child))
	assert.Equal(t, []any{}, child["warnings"], "andere eigene Komponente")
}

func TestExternalLinks_GruppeMandantentrennung(t *testing.T) {
	f := newExtFixture(t)
	srcID := f.createSource(f.page, f.adminTok, "https://status.example.com")
	otherSrc := f.createSource(f.other, f.otherTok, "https://status.example.com")
	grp := models.ExternalComponent{ExternalSourceID: srcID, ExternalID: "g1", Name: "Gruppe", IsGroup: true, Status: "operational", LastSeenAt: time.Now()}
	require.NoError(t, f.db.Create(&grp).Error)
	foreign := models.ExternalComponent{ExternalSourceID: otherSrc, ExternalID: "x", Name: "Fremd", Status: "major_outage", GroupExternalID: "g1", LastSeenAt: time.Now()}
	require.NoError(t, f.db.Create(&foreign).Error)
	f.extComp(srcID, "vm", "VM", "operational")

	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.pageAdminTok, f.comp.ID, grp.ID).Code)
	w := f.do(http.MethodGet, "/api/pages/"+pid(f.page)+"/components", "", nil)
	var nodes []treeNode
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &nodes))
	assert.Equal(t, "operational", findNode(nodes, "API").EffectiveStatus, "Mitglieder fremder Quellen zählen nicht")
	// Die fremde Gruppe ist über die Seite nicht koppelbar.
	assert.Equal(t, http.StatusNotFound, f.linkVia(f.other, f.otherTok, f.otherComp.ID, grp.ID).Code)
}

// "Jetzt abrufen" wartet nicht auf einen laufenden Abruf.
type blockingExtProvider struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingExtProvider) FetchSummary(context.Context, externalstatus.FetchRequest) (*externalstatus.Summary, error) {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return &externalstatus.Summary{}, nil
}

func TestFetchExternalSource_LaeuftSchonEinAbruf(t *testing.T) {
	f := newExtFixture(t)
	srcID := f.createSource(f.page, f.adminTok, "https://status.example.com")
	bp := &blockingExtProvider{entered: make(chan struct{}), release: make(chan struct{})}
	f.h.ExternalStatus = externalstatus.NewService(f.db, externalstatus.Options{
		Providers: map[string]externalstatus.Provider{externalstatus.ProviderSummaryV2: bp},
		Jitter:    func(time.Duration) time.Duration { return 0 },
	})
	path := f.sourcesPath(f.page) + "/" + uid(srcID) + "/fetch"

	done := make(chan int)
	go func() { done <- f.do(http.MethodPost, path, f.pageAdminTok, nil).Code }()
	<-bp.entered

	w := f.do(http.MethodPost, path, f.pageAdminTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Result externalstatus.FetchResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Result.OK)
	assert.Equal(t, "fetch_in_progress", resp.Result.Error)

	close(bp.release)
	assert.Equal(t, http.StatusOK, <-done)
	w = f.do(http.MethodPost, path, f.pageAdminTok, nil)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Result.OK, "danach ist die Quelle wieder frei")
}

func TestNewHandler_VerdrahtetAusfallMeldung(t *testing.T) {
	// NewHandler setzt sich selbst als Empfänger der Ausfall-Ereignisse: Der
	// echte Abruf einer gesperrten Adresse (Loopback) scheitert, die Quelle ist
	// seit zwei Stunden ohne Erfolg - die Mail geht ohne weitere Verdrahtung raus.
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newExtFixture(t)
	h := handlers.NewHandler(f.db)
	last := time.Now().Add(-2 * time.Hour)
	src := models.ExternalSource{StatusPageID: f.page.ID, Name: "Lokal", BaseURL: "https://localhost", Provider: "summaryv2",
		PollIntervalSeconds: 120, OutageAfterMinutes: 60, Active: true, LastSuccessAt: &last}
	require.NoError(t, f.db.Create(&src).Error)

	res := h.ExternalStatus.FetchNow(context.Background(), src)
	h.WaitOutageMails()

	assert.False(t, res.OK)
	mails := drainMails(t, srv, 2)
	assert.Contains(t, mails[0].subject, "„Lokal“")
}

func TestExternalLinks_GruppenstatusNichtLesbar(t *testing.T) {
	f := newExtFixture(t)
	srcID := f.createSource(f.page, f.adminTok, "https://status.example.com")
	grp := models.ExternalComponent{ExternalSourceID: srcID, ExternalID: "g1", Name: "Gruppe", IsGroup: true, Status: "operational", LastSeenAt: time.Now()}
	require.NoError(t, f.db.Create(&grp).Error)
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.pageAdminTok, f.comp.ID, grp.ID).Code)

	extFailOn(t, f.db, "row", "external_components")
	assert.Equal(t, http.StatusInternalServerError, f.do(http.MethodGet, f.linksPath(f.page), f.adminTok, nil).Code)
}
