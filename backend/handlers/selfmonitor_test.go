package handlers_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/selfmonitor"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Verdrahtung der Selbstüberwachung (Prüfungen aus der Umgebung,
// Schalter in den Systemeinstellungen).

func checkSlugsFor(t *testing.T, env map[string]string) []string {
	t.Helper()
	h := handlers.NewHandler(testutils.SetupTestDB())
	m := h.NewSelfMonitor(func(k string) string { return env[k] })
	var out []string
	for _, c := range m.Checks {
		out = append(out, c.Slug)
	}
	return out
}

func TestSelfMonitor_PruefungenAusDerUmgebung(t *testing.T) {
	basis := []string{"feature-plattform-api", "feature-plattform-datenbank", "feature-plattform-email"}
	assert.Equal(t, basis, checkSlugsFor(t, nil), "ohne FRONTEND_URL/Backup-Job nur die Pflichtprüfungen")
	assert.Equal(t, append(append([]string{}, basis...), "feature-plattform-frontend"),
		checkSlugsFor(t, map[string]string{"FRONTEND_URL": " https://example.org "}))
	assert.Equal(t, append(append([]string{}, basis...), "feature-plattform-backup"),
		checkSlugsFor(t, map[string]string{"SELFMONITOR_BACKUP_JOB": "backup-prod"}))
}

// Review B: Ein Jobname mit Pfadanteilen registriert keine Backup-Prüfung.
func TestSelfMonitor_BackupJobNameWirdValidiert(t *testing.T) {
	buf := &bytes.Buffer{}
	alt := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(alt) })
	for _, job := range []string{"../../x", "a/b", "Backup", "a b", "x.prom"} {
		got := checkSlugsFor(t, map[string]string{"SELFMONITOR_BACKUP_JOB": job})
		assert.NotContains(t, got, "feature-plattform-backup", job)
	}
	assert.Contains(t, buf.String(), "SELFMONITOR_BACKUP_JOB ungültig")
	assert.NotContains(t, buf.String(), "../../x", "der Wert steht nicht im Log")
	assert.Contains(t, checkSlugsFor(t, map[string]string{"SELFMONITOR_BACKUP_JOB": "backup-prod2"}), "feature-plattform-backup")
}

func TestSelfMonitor_BackupPruefungLiestMetrikOrdner(t *testing.T) {
	h := handlers.NewHandler(testutils.SetupTestDB())
	// Standardordner /job-metrics existiert hier nicht (Mount fehlt): Fehlschlag
	// statt Skip (Betreiber-Entscheidung)
	m := h.NewSelfMonitor(func(k string) string {
		if k == "SELFMONITOR_BACKUP_JOB" {
			return "backup-prod"
		}
		return ""
	})
	last := m.Checks[len(m.Checks)-1]
	assert.Equal(t, "feature-plattform-backup", last.Slug)
	assert.Equal(t, "degraded_performance", last.FailedStatus)
	res := last.Run(context.Background())
	assert.False(t, res.Skip)
	assert.False(t, res.OK)
	assert.Equal(t, "Backup-Metrik-Verzeichnis fehlt", res.Detail)

	dir := t.TempDir()
	m = h.NewSelfMonitor(func(k string) string {
		return map[string]string{"SELFMONITOR_BACKUP_JOB": "backup-prod", "SELFMONITOR_METRICS_DIR": dir}[k]
	})
	backup := m.Checks[len(m.Checks)-1]
	assert.True(t, backup.Run(context.Background()).Skip)

	old := time.Now().Add(-72 * time.Hour).Unix()
	body := fmt.Sprintf("job_last_success_timestamp_seconds{job_name=\"backup-prod\"} %d\njob_last_exit_code{job_name=\"backup-prod\"} 1\n", old)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "job_backup-prod.prom"), []byte(body), 0o600))
	assert.False(t, backup.Run(context.Background()).OK, "Ordner aus SELFMONITOR_METRICS_DIR, 72 h ist veraltet")

	frisch := time.Now().Add(-35 * time.Hour).Unix()
	body = fmt.Sprintf("job_last_success_timestamp_seconds{job_name=\"backup-prod\"} %d\njob_last_exit_code{job_name=\"backup-prod\"} 0\n", frisch)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "job_backup-prod.prom"), []byte(body), 0o600))
	assert.True(t, backup.Run(context.Background()).OK, "35 h ist frisch (Grenze 36 h)")
	stale := time.Now().Add(-37 * time.Hour).Unix()
	body = fmt.Sprintf("job_last_success_timestamp_seconds{job_name=\"backup-prod\"} %d\njob_last_exit_code{job_name=\"backup-prod\"} 0\n", stale)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "job_backup-prod.prom"), []byte(body), 0o600))
	assert.False(t, backup.Run(context.Background()).OK, "37 h ist veraltet")
}

func TestSelfMonitor_SchalterWirktAufDenMonitor(t *testing.T) {
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "System", Slug: "system", IsSystem: true}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Slug: "feature-plattform-api", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	m := handlers.NewHandler(db).NewSelfMonitor(func(string) string { return "" })
	m.Threshold = 1
	m.Checks = []selfmonitor.Check{{Slug: comp.Slug, Name: "API", FailedStatus: "major_outage",
		Run: func(context.Context) selfmonitor.Result { return selfmonitor.Result{Detail: "tot"} }}}
	status := func() string {
		var c models.Component
		require.NoError(t, db.First(&c, comp.ID).Error)
		return c.Status
	}

	require.NoError(t, db.Create(&models.SystemSetting{Key: "selfmonitor.enabled", Value: "false"}).Error)
	m.RunOnce(context.Background())
	assert.Equal(t, "operational", status(), "Schalter aus: keine Statusänderung")

	require.NoError(t, db.Model(&models.SystemSetting{}).Where("key = ?", "selfmonitor.enabled").Update("value", "true").Error)
	m.RunOnce(context.Background())
	assert.Equal(t, "major_outage", status())
}

func TestSelfMonitor_StartUndStopp(t *testing.T) {
	h := handlers.NewHandler(testutils.SetupTestDB())
	stop := h.StartSelfMonitorFromEnv(context.Background(), func(string) string { return "" })
	stop()
}

// altfall legt auf der Systemseite einen geschlossenen Monitor-Vorfall an,
// dessen Komponente ausgeblendet ist (Altfall).
func altfall(t *testing.T, db *gorm.DB) models.Incident {
	t.Helper()
	page := models.StatusPage{Name: "System", Slug: "system", IsSystem: true}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Slug: "feature-plattform-api", Status: "operational", Visible: true}
	require.NoError(t, db.Create(&comp).Error)
	require.NoError(t, db.Model(&comp).Update("visible", false).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "Störung: API", Status: "resolved",
		Message: "Die automatische Selbstüberwachung meldet eine Störung bei API (Zeitüberschreitung)."}
	require.NoError(t, db.Create(&inc).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID}).Error)
	require.NoError(t, db.Create(&models.IncidentUpdate{IncidentID: inc.ID, Status: "investigating", AuthorEmail: "system", Message: inc.Message}).Error)
	return inc
}

// Der Start bereinigt Altfälle, auch bei ausgeschalteter Überwachung
// (der Text ist öffentlich, der Schalter ändert daran nichts).
func TestSelfMonitor_StartNeutralisiertAltfaelle(t *testing.T) {
	db := testutils.SetupTestDB()
	inc := altfall(t, db)
	h := handlers.NewHandler(db)
	stop := h.StartSelfMonitorFromEnv(context.Background(), func(string) string { return "" })
	stop()
	var nachher models.Incident
	require.NoError(t, db.First(&nachher, inc.ID).Error)
	assert.Equal(t, "Störung: automatische Überwachung", nachher.Title)
	assert.NotContains(t, nachher.Message, "API")
}

// Scheitert der Backfill, startet die Überwachung trotzdem und der
// Fehler wird geloggt (fail closed: nichts geschrieben, nächster Start wiederholt).
func TestSelfMonitor_StartBackfillFehlerBlockiertNichts(t *testing.T) {
	db := testutils.SetupTestDB()
	inc := altfall(t, db)
	require.NoError(t, db.Exec("CREATE TRIGGER sperre829h BEFORE UPDATE OF title ON incidents BEGIN SELECT RAISE(ABORT, 'kaputt'); END").Error)
	buf := &bytes.Buffer{}
	alt := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(alt) })
	h := handlers.NewHandler(db)
	stop := h.StartSelfMonitorFromEnv(context.Background(), func(string) string { return "" })
	stop()
	assert.Contains(t, buf.String(), "Vorfalltexte nicht bereinigt")
	var nachher models.Incident
	require.NoError(t, db.First(&nachher, inc.ID).Error)
	assert.Equal(t, "Störung: API", nachher.Title)
}

func TestSettings_SelbstueberwachungSchalter(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupSettingsAdminRouter(t, db, h, true)

	w := doStatusPageRequest(r, "GET", "/api/settings", nil)
	requireStatusCode(t, w, http.StatusOK)
	defaults := decodeSettings(t, w.Body.Bytes())
	assert.False(t, defaults["selfmonitor_enabled"], "fehlender Key = aus (Betreiber-Entscheidung)")
	assert.True(t, defaults["email_enabled"] && defaults["orders_enabled"], "die übrigen Schalter bleiben standardmäßig an")

	w = doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"selfmonitor_enabled": true})
	requireStatusCode(t, w, http.StatusOK)
	resp := decodeSettings(t, w.Body.Bytes())
	assert.True(t, resp["selfmonitor_enabled"])
	assert.True(t, resp["email_enabled"] && resp["orders_enabled"], "andere Schalter unverändert")

	var audit models.AuditLog
	require.NoError(t, db.Where("action = ? AND target_id = ?", "system_setting.updated", "selfmonitor.enabled").First(&audit).Error)
	assert.Equal(t, "true", audit.Details)

	w = doStatusPageRequest(r, "PUT", "/api/settings", map[string]interface{}{"unbekannt": true})
	requireStatusCode(t, w, http.StatusBadRequest)
	assert.Contains(t, w.Body.String(), "selfmonitor_enabled")
}

// Jede Prüfung steuert eine Komponente aus dem Funktionsverzeichnis; ein
// umbenannter Eintrag dort würde die Überwachung sonst still ins Leere laufen lassen.
func TestSelfMonitor_AlleSlugsStehenImFunktionsverzeichnis(t *testing.T) {
	known := map[string]bool{}
	for _, g := range database.SystemCapabilities {
		for _, it := range g.Items {
			known[database.CapabilitySlug(it.Doc)] = true
		}
	}
	all := checkSlugsFor(t, map[string]string{"FRONTEND_URL": "https://example.org", "SELFMONITOR_BACKUP_JOB": "backup-prod"})
	assert.Len(t, all, 5)
	for _, slug := range all {
		assert.True(t, known[slug], "%s fehlt in database.SystemCapabilities", slug)
	}
}

// Ist der Versand per Systemeinstellung abgeschaltet, ist der Mailserver "nicht
// prüfbar" (kein Ausfall, kein Verbindungsaufbau).
func TestSelfMonitor_MailPruefungRespektiertDenVersandSchalter(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, db.Create(&models.SystemSetting{Key: "email.enabled", Value: "false"}).Error)
	m := handlers.NewHandler(db).NewSelfMonitor(func(string) string { return "" })
	mail := m.Checks[2]
	assert.Equal(t, "feature-plattform-email", mail.Slug)
	assert.Equal(t, "partial_outage", mail.FailedStatus)
	assert.True(t, mail.Run(context.Background()).Off)
}

// Betreiber-Entscheidung: ohne ausdrücklich gesetztes "an" läuft keine
// Prüfung und nichts wird angefasst (kein Status, kein Vorfall, kein Audit, kein Merker).
func TestSelfMonitor_OhneEintragLaeuftNichtsUndNichtsWirdAngefasst(t *testing.T) {
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "System", Slug: "system", IsSystem: true}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Slug: "feature-plattform-api", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	m := handlers.NewHandler(db).NewSelfMonitor(func(string) string { return "" })
	m.Threshold = 1
	m.IncidentAfter = 0
	runs := 0
	m.Checks = []selfmonitor.Check{{Slug: comp.Slug, Name: "API", FailedStatus: "major_outage",
		Run: func(context.Context) selfmonitor.Result { runs++; return selfmonitor.Result{Detail: "tot"} }}}

	for i := 0; i < 5; i++ {
		m.RunOnce(context.Background())
	}

	assert.Zero(t, runs, "keine Prüfung läuft")
	var c models.Component
	require.NoError(t, db.First(&c, comp.ID).Error)
	assert.Equal(t, "operational", c.Status)
	for _, n := range []any{&models.Incident{}, &models.IncidentUpdate{}, &models.AuditLog{}, &models.SystemSetting{}} {
		var count int64
		require.NoError(t, db.Model(n).Count(&count).Error)
		assert.Zero(t, count, "%T bleibt leer", n)
	}
}

// Fehler (1.11.2026): Die Frontend-Prüfung rief die öffentliche
// Adresse ab; Cloudflare/Caddy antworteten dem Go-Client mit 401/403 und die
// Komponente "Frontend" blieb dauerhaft auf Major Outage. Jetzt geht die
// Prüfung auf den eigenen Port (127.0.0.1:PORT) und ignoriert FRONTEND_URL.
func TestSelfMonitor_FrontendPruefungGehtIntern_NichtZurOeffentlichenAdresse(t *testing.T) {
	publicHits := 0
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		publicHits++
		w.WriteHeader(http.StatusForbidden) // Cloudflare/Caddy: Zugangssperre
	}))
	defer public.Close()
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/assets/index-abc.js" {
			_, _ = w.Write([]byte("console.log(1)"))
			return
		}
		_, _ = w.Write([]byte(`<html><body><div id="app"></div><script src="/assets/index-abc.js"></script></body></html>`))
	}))
	defer internal.Close()
	u, err := url.Parse(internal.URL)
	require.NoError(t, err)

	h := handlers.NewHandler(testutils.SetupTestDB())
	m := h.NewSelfMonitor(func(k string) string {
		return map[string]string{"FRONTEND_URL": public.URL, "PORT": u.Port()}[k]
	})
	var fe selfmonitor.Check
	for _, c := range m.Checks {
		if c.Slug == "feature-plattform-frontend" {
			fe = c
		}
	}
	require.Equal(t, "feature-plattform-frontend", fe.Slug)
	assert.Equal(t, "major_outage", fe.FailedStatus)

	// 127.0.0.1 und der Testserver teilen sich den Loopback; Port wie bei /healthz.
	res := fe.Run(context.Background())
	assert.True(t, res.OK, "intern erreichbar: %q", res.Detail)
	assert.Zero(t, publicHits, "die öffentliche FRONTEND_URL wird nicht abgerufen")
}

// Der Schalter unterscheidet "nicht gesetzt" (Standard aus) von
// "ausdrücklich an" und einem Lesefehler (Tabelle weg): der ist ein Fehler, kein "aus".
func TestSelfMonitor_SchalterLesefehlerIstKeinAus(t *testing.T) {
	db := testutils.SetupTestDB()
	m := handlers.NewHandler(db).NewSelfMonitor(func(string) string { return "" })
	on, err := m.Enabled()
	require.NoError(t, err)
	assert.False(t, on, "nicht gesetzt: Standard aus")
	require.NoError(t, db.Create(&models.SystemSetting{Key: "selfmonitor.enabled", Value: "true"}).Error)
	on, err = m.Enabled()
	require.NoError(t, err)
	assert.True(t, on)
	require.NoError(t, db.Migrator().DropTable(&models.SystemSetting{}))
	_, err = m.Enabled()
	assert.Error(t, err)
}
