package selfmonitor

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Re-Review 1: recover sperrt die Störungs-Marke zuerst (FOR UPDATE),
// wie openIncident; nur so liest eine zweite Replika (Postgres, READ COMMITTED)
// die Vorfall-Marke erst nach dem Commit der ersten. Die Sperrwirkung selbst ist
// nur auf Postgres prüfbar; hier wird die Klausel an der Abfrage geprüft.
func TestMonitor_RecoverSperrtStoerungsMarkeZuerst(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	var gesperrt []string
	require.NoError(t, f.db.Callback().Query().After("gorm:query").Register("test:sperre_beobachten", func(d *gorm.DB) {
		if d.Statement.Table != "system_settings" {
			return
		}
		if _, ok := d.Statement.Clauses["FOR"]; !ok {
			return
		}
		for _, v := range d.Statement.Vars {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "selfmonitor.") {
				gesperrt = append(gesperrt, s)
			}
		}
	}))
	comp := f.comp
	comp.Status = f.m.Checks[0].FailedStatus
	f.m.recover(f.m.Checks[0], comp, recoveredReason)
	assert.Contains(t, gesperrt, failingKeyPrefix+testSlug, "Störungs-Marke gesperrt gelesen")
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// Re-Review (1): Scheitert das gesperrte Lesen der Störungs-Marke, wird nichts erholt.
func TestMonitor_RecoverSperrLesefehlerRollback(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	offeneStoerung(t, f)
	lesefehlerAb(t, f.db, 2) // 1.: Besitzprüfung, 2.: gesperrtes Lesen in der Transaktion
	comp := f.comp
	comp.Status = f.m.Checks[0].FailedStatus
	f.m.recover(f.m.Checks[0], comp, recoveredReason)
	lesefehlerAus(t, f.db)
	assert.Contains(t, buf.String(), "Erholung nicht gespeichert")
	assert.NotContains(t, buf.String(), "GEHEIM123")
	assert.Equal(t, "major_outage", f.status(t))
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// Re-Review (2): Eine soft-gelöschte Störungs-Marke gilt (wie in readValue) als
// vorhanden; es entsteht trotzdem ein Vorfall.
func TestMonitor_SoftGeloeschteStoerungsMarkeBlockiertVorfallNicht(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.NoError(t, f.db.Where("key = ?", failingKeyPrefix+testSlug).Delete(&models.SystemSetting{}).Error)
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(5)
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "1 = 1"))
}

// Re-Review (2): Der Upsert setzt eine soft-gelöschte Marke zurück.
func TestSetValue_HebtSoftDeleteAuf(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, setValue(f.db, "selfmonitor.x", "1"))
	require.NoError(t, f.db.Where("key = ?", "selfmonitor.x").Delete(&models.SystemSetting{}).Error)
	require.NoError(t, setValue(f.db, "selfmonitor.x", "2"))
	assert.EqualValues(t, 1, f.count(t, &models.SystemSetting{}, "key = ? AND value = ?", "selfmonitor.x", "2"))
}

// Re-Review (3): Das ECHTE openMetrics öffnet einen FIFO ohne Schreiber sofort
// (O_NONBLOCK); ein blockierendes Open käme nicht zurück.
func TestOpenMetrics_FifoOhneSchreiberKehrtSofortZurueck(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	done := make(chan *os.File, 1)
	go func() {
		f, err := openMetrics(fifo)
		if err != nil {
			f = nil
		}
		done <- f
	}()
	select {
	case f := <-done:
		require.NotNil(t, f)
		_ = f.Close()
	case <-time.After(3 * time.Second):
		t.Cleanup(func() { // hängenden Open (Fehlerfall) freigeben
			if w, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil { //nolint:gosec // Testpfad
				_ = w.Close()
			}
		})
		t.Fatal("openMetrics blockiert an einem FIFO ohne Schreiber")
	}
}

// Re-Review (4): Bei mehreren soft-gelöschten Komponenten mit gleichem Slug zählt die jüngste.
func TestMonitor_AufraeumenAuditNimmtJuengsteGeloeschteKomponente(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	neu := models.Component{StatusPageID: f.page.ID, Name: "API neu", Slug: testSlug, Status: "operational"}
	require.NoError(t, f.db.Create(&neu).Error)
	f.fail()
	f.run(3)
	require.NoError(t, f.db.Delete(&neu).Error)
	f.run(1)
	var a models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "selfmonitor.component_released").First(&a).Error)
	assert.Equal(t, "API neu", a.TargetName)
}
