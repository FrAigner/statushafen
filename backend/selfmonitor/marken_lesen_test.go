package selfmonitor

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Review A: a FIFO planted between Lstat and Open
// with a silent writer must not hang the monitor (Go registers O_NONBLOCK files
// with the netpoller, Read then waits instead of failing with EAGAIN).
func TestBackupCheck_FifoNachLstatUntergeschobenBlockiertNicht(t *testing.T) {
	dir := t.TempDir()
	pfad := filepath.Join(dir, "job_backup-prod.prom")
	require.NoError(t, os.WriteFile(pfad, []byte("x"), 0o600)) // Lstat sees a regular file
	fifo := filepath.Join(t.TempDir(), "fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))

	alt := openMetrics
	t.Cleanup(func() { openMetrics = alt })
	var schreiber *os.File
	openMetrics = func(string) (*os.File, error) {
		f, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // Test path
		if err != nil {
			return nil, err
		}
		schreiber, err = os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // Test path: silent writer
		return f, err
	}
	t.Cleanup(func() {
		if schreiber != nil {
			_ = schreiber.Close()
		}
	})

	done := make(chan Result, 1)
	go func() { done <- backupCheckFuer(dir)(context.Background()) }()
	select {
	case res := <-done:
		assert.False(t, res.OK)
		assert.False(t, res.Skip)
		assert.Equal(t, "Backup-Metrik ist keine reguläre Datei", res.Detail)
	case <-time.After(3 * time.Second):
		t.Fatal("BackupCheck hängt an einem untergeschobenen FIFO")
	}
}

// Review B: job names without path parts.
func TestValidJobName(t *testing.T) {
	for _, ok := range []string{"backup-prod", "backup2"} {
		assert.True(t, ValidJobName(ok), ok)
	}
	for _, bad := range []string{"", "../../x", "a/b", "Backup", "a.b", "a\nb"} {
		assert.False(t, ValidJobName(bad), bad)
	}
}

// Review D: a soft-deleted marker counts as present (ON CONFLICT
// also hits it), otherwise every tick would run into the silent rollback.
func TestReadValue_SiehtAuchSoftGeloeschteMarken(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, setValue(f.db, incidentKeyPrefix+testSlug, "7"))
	require.NoError(t, f.db.Where("key = ?", incidentKeyPrefix+testSlug).Delete(&models.SystemSetting{}).Error)
	v, has, err := readValue(f.db, incidentKeyPrefix+testSlug)
	require.NoError(t, err)
	assert.True(t, has)
	assert.Equal(t, "7", v)
}

// Review D: if creation fails because of an existing marker, this appears
// as a warning in the log (fixed text, no secret).
func TestMonitor_VorfallMarkeExistiertWirdGeloggt(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.NoError(t, setValue(f.db, incidentKeyPrefix+testSlug, "999"))
	f.m.openIncident(f.page.ID, f.m.Checks[0], f.comp, f.now, "x https://db.example/x?token=GEHEIM123")
	assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "1 = 1"))
	assert.Contains(t, buf.String(), "Vorfall-Marke existiert bereits")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}

// Review C: if the outage marker was removed in the meantime (recovery
// by another replica), openIncident no longer creates an incident.
func TestMonitor_VorfallAnlegenOhneStoerungsMarkeRollback(t *testing.T) {
	f := newFixture(t)
	f.m.openIncident(f.page.ID, f.m.Checks[0], f.comp, f.now, "x")
	assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "1 = 1"))
	assert.EqualValues(t, 0, f.count(t, &models.IncidentUpdate{}, "1 = 1"))
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", incidentKeyPrefix+"%"))
}

// Review C: read error of the outage marker in the transaction = no incident, log.
func TestMonitor_VorfallAnlegenStoerungsMarkeLesefehler(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.fail()
	f.run(3)
	lesefehlerAb(t, f.db, 1)
	f.m.openIncident(f.page.ID, f.m.Checks[0], f.comp, f.now, "x")
	lesefehlerAus(t, f.db)
	assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "1 = 1"))
	assert.Contains(t, buf.String(), "Vorfall nicht angelegt")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}

// Review E: the audit of the cleanup names the real component and
// the closed incident.
func TestMonitor_AufraeumenAuditNenntKomponenteUndVorfall(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.run(1)
	var a models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "selfmonitor.component_released").First(&a).Error)
	assert.Equal(t, "1", a.TargetID)
	assert.Equal(t, f.comp.Name, a.TargetName)
	assert.Equal(t, "Komponente gelöscht; Vorfall 1 geschlossen", a.Details)
}

// Review E: without an incident, or if the admin already closed it, there is
// no "incident" in the audit; if the component is hard-deleted, the slug serves.
func TestMonitor_AufraeumenAuditOhneGeschlossenenVorfall(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.run(1)
	var a models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "selfmonitor.component_released").First(&a).Error)
	assert.Equal(t, "Komponente gelöscht", a.Details)

	g := newFixture(t)
	offeneStoerung(t, g)
	require.NoError(t, g.db.Model(&models.Incident{}).Where("1 = 1").Update("status", "resolved").Error)
	require.NoError(t, g.db.Unscoped().Delete(&g.comp).Error)
	g.run(1)
	var b models.AuditLog
	require.NoError(t, g.db.Where("action = ?", "selfmonitor.component_released").First(&b).Error)
	assert.Equal(t, "Komponente nicht mehr auf der Systemseite", b.Details, "Admin schloss den Vorfall schon, hart gelöscht: Identität unbekannt")
	assert.Equal(t, "0", b.TargetID)
	assert.Equal(t, testSlug, b.TargetName, "hart gelöscht: Slug statt Name")
}
