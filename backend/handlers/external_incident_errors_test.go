package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Fehlerpfade (Datenbankfehler, Webhook, private Seite, Header).

// injectFail lässt GORM-Operationen op ("query", "row", "delete") auf table
// scheitern; skip überspringt die ersten Treffer.
func injectFail(t *testing.T, db *gorm.DB, op, table string, skip int) {
	t.Helper()
	name := "test:fail:" + op + ":" + table
	seen := 0
	hook := func(tx *gorm.DB) {
		if tx.Statement.Table != table {
			return
		}
		seen++
		if seen > skip {
			_ = tx.AddError(errors.New("injizierter Fehler"))
		}
	}
	var err error
	switch op {
	case "row":
		err = db.Callback().Row().Before("gorm:row").Register(name, hook)
	case "delete":
		err = db.Callback().Delete().Before("gorm:delete").Register(name, hook)
	default:
		err = db.Callback().Query().Before("gorm:query").Register(name, hook)
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(name)
		_ = db.Callback().Row().Remove(name)
		_ = db.Callback().Delete().Remove(name)
	})
}

func (s *incidentSetup) firstIncident() models.ExternalIncident {
	s.t.Helper()
	var row models.ExternalIncident
	require.NoError(s.t, s.db.First(&row).Error)
	return row
}

func TestExternalIncidentsAPI_Datenbankfehler(t *testing.T) {
	cases := []struct {
		name, op, table string
		skip            int
		detail          bool
	}{
		{"Liste: Meldungen", "query", "external_incidents", 0, false},
		{"Liste: Kopplungen", "row", "l", 0, false},
		{"Liste: Quellen", "query", "external_sources", 0, false},
		{"Detail: Meldung", "query", "external_incidents", 0, true},
		{"Detail: Quelle", "query", "external_sources", 0, true},
		{"Detail: Kopplungen", "row", "l", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newIncidentSetup(t)
			s.setFeed("incident", extInc("i1", "investigating", "a"))
			s.poll()
			path := s.incidentsPath(s.page, "")
			if tc.detail {
				path += "/" + uid(s.firstIncident().ID)
			}
			injectFail(t, s.db, tc.op, tc.table, tc.skip)
			w := s.do(http.MethodGet, path, s.pageAdminTok, nil)
			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
		})
	}
}

func TestPublicDashboard_ExternalIncidentsFehlerLaesstDashboardLeben(t *testing.T) {
	s := newIncidentSetup(t)
	s.setFeed("incident", extInc("i1", "investigating", "a"))
	s.poll()
	injectFail(t, s.db, "query", "external_incidents", 0)
	w := s.do(http.MethodGet, "/api/public/pages/eigene/dashboard", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"external_incidents":[]`)
}

func TestDeleteExternalSource_FehlerRolltZurueck(t *testing.T) {
	for _, table := range []string{"external_incident_updates", "external_incident_components", "external_incidents"} {
		t.Run(table, func(t *testing.T) {
			s := newIncidentSetup(t)
			s.setFeed("incident", extInc("i1", "investigating", "a"))
			s.poll()
			injectFail(t, s.db, "delete", table, 0)
			w := s.do(http.MethodDelete, s.sourcesPath(s.page)+"/"+uid(s.srcID), s.pageAdminTok, nil)
			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())

			var n int64
			s.db.Model(&models.ExternalSource{}).Where("id = ?", s.srcID).Count(&n)
			assert.EqualValues(t, 1, n, "die Quelle bleibt bestehen")
			s.db.Model(&models.ExternalIncidentUpdate{}).Count(&n)
			assert.EqualValues(t, 1, n, "und nichts ist halb gelöscht")
			s.db.Model(&models.ExternalComponentLink{}).Count(&n)
			assert.EqualValues(t, 1, n, "auch die Kopplung nicht")
		})
	}
}

func TestExternalIncidentMail_StatusseiteNichtLadbar(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	s.setFeed("incident", extInc("i1", "investigating", "a"))
	injectFail(t, s.db, "query", "status_pages", 0)
	s.poll()
	drainMails(t, srv, 0)
	assert.EqualValues(t, 1, s.auditCount("external_incident.mirrored"))
	assert.EqualValues(t, 0, s.auditCount("external_incident.notified"))
}

func TestExternalIncidentWebhook_ErfolgUndStatusfehler(t *testing.T) {
	handlers.SetWebhookLoopbackAllowedForTests(t.Cleanup)
	var bodies []string
	status := http.StatusOK
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		bodies = append(bodies, string(buf[:n]))
		w.WriteHeader(status)
	}))
	defer hook.Close()

	s := newIncidentSetup(t)
	require.NoError(t, s.db.Create(&models.Subscriber{StatusPageID: s.page.ID, Channel: "google_chat", WebhookURL: hook.URL,
		SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive}).Error)

	s.setFeed("incident", extInc("i1", "investigating", "Zeile 1\nZeile 2"))
	s.poll()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `Quelle:** Example Cloud`)
	assert.Contains(t, bodies[0], fixtureBase+"/incidents/i1")
	assert.Contains(t, bodies[0], "API")
	assert.EqualValues(t, 1, s.auditCount("external_incident.notified"))
	assert.EqualValues(t, 0, s.auditCount("external_incident.failed"))

	// 5xx würde wiederholt (mit echter Wartezeit); 403 ist permanent, ein Versuch.
	status = http.StatusForbidden
	s.setFeed("incident", extInc("i1", "identified", "Zeile 1\nZeile 2", "Neu"))
	s.poll()
	require.Len(t, bodies, 2)
	assert.EqualValues(t, 1, s.auditCount("external_incident.failed"))
	var failed models.AuditLog
	require.NoError(t, s.db.Where("action = ?", "external_incident.failed").First(&failed).Error)
	assert.Contains(t, failed.Details, "webhook status 403")
	assert.Contains(t, failed.Details, "channel=google_chat")
}

func TestExternalIncidentMail_PrivateSeiteMitBerechtigtemErsteller(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.org")
	s := newIncidentSetup(t)
	require.NoError(t, s.db.Model(&models.StatusPage{}).Where("id = ?", s.page.ID).Update("is_private", true).Error)
	var creator models.User
	require.NoError(t, s.db.Where("email = ?", "viewer@example.com").First(&creator).Error)
	sub := models.Subscriber{StatusPageID: s.page.ID, Email: "privat@example.com", Channel: "email", SubscribedComponentIDs: "all",
		Status: models.SubscriberStatusActive, CreatedByUserID: &creator.ID}
	require.NoError(t, s.db.Create(&sub).Error)

	s.setFeed("incident", extInc("i1", "investigating", "a"))
	s.poll()
	m := drainMails(t, srv, 1)[0]
	assert.Contains(t, m.body, "https://status.example.org/status/eigene?token=", "Zugriffs-Token im Link der privaten Seite")
}

func TestExternalIncidentMail_HeaderMitZeilenumbruchWirdVerworfen(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	// FRONTEND_URL fließt in den List-Unsubscribe-Header; CR/LF darf dort nie ankommen.
	t.Setenv("FRONTEND_URL", "https://status.example.org\nBcc: x@example.com")
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	s.setFeed("incident", extInc("i1", "investigating", "a"))
	s.poll()
	drainMails(t, srv, 0)
	assert.EqualValues(t, 1, s.auditCount("external_incident.failed"))
}

// Nur der List-Unsubscribe-Header trägt FRONTEND_URL (Vorlage ohne Link): auch dort wird CR/LF verworfen.
func TestExternalIncidentMail_ListUnsubscribeHeaderMitZeilenumbruch(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.org\nBcc: x@example.com")
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	require.NoError(t, s.db.Create(&models.NotificationTemplate{
		StatusPageID: s.page.ID, Kind: "external_incident_created", Audience: "subscriber", Language: "de",
		Subject: "Betreff", BodyHTML: "ohne Links",
	}).Error)
	s.setFeed("incident", extInc("i1", "investigating", "a"))
	s.poll()
	drainMails(t, srv, 0)
	assert.EqualValues(t, 1, s.auditCount("external_incident.failed"))
	var failed models.AuditLog
	require.NoError(t, s.db.Where("action = ?", "external_incident.failed").First(&failed).Error)
	assert.Contains(t, failed.Details, "extra header")
}

func TestExternalIncidentMail_DefektesTemplateFaelltAufStandardZurueck(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	require.NoError(t, s.db.Create(&models.NotificationTemplate{
		StatusPageID: s.page.ID, Kind: "external_incident_created", Audience: "subscriber", Language: "de",
		Subject: "{{.Gibtsnicht}}", BodyHTML: "x",
	}).Error)
	s.setFeed("incident", extInc("i1", "investigating", "a"))
	s.poll()
	m := drainMails(t, srv, 1)[0]
	assert.Contains(t, m.subject, "[EXTERN]", "eine defekte Vorlage verhindert die Mail nicht")
}

func TestExternalIncidentMail_UnbekanntesStatusUndWartungsLabel(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")

	it := extInc("m1", "scheduled", "Beginnt gleich")
	it.Kind, it.Status, it.Impact = "maintenance", "in_progress", "maintenance"
	s.setFeed("maintenance", it)
	s.poll()
	m := drainMails(t, srv, 1)[0]
	assert.Contains(t, m.subject, "In Arbeit")

	it.Status, it.Closed = "completed", true
	it.Updates = append(it.Updates, extInc("m1", "resolved", "x", "Fertig").Updates[1])
	s.setFeed("maintenance", it)
	time.Sleep(10 * time.Millisecond)
	s.poll()
	m = drainMails(t, srv, 1)[0]
	assert.Contains(t, m.subject, "Abgeschlossen")
	assert.Contains(t, m.body, "Wartung")
}
