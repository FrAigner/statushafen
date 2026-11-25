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

// Re-review 1: recover locks the outage marker first (FOR UPDATE),
// like openIncident; only then does a second replica (Postgres, READ COMMITTED) read
// the incident marker only after the first one's commit. The lock effect itself is
// only testable on Postgres; here the clause is checked on the query.
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

// Re-review (1): if the locked read of the outage marker fails, nothing is recovered.
func TestMonitor_RecoverSperrLesefehlerRollback(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	offeneStoerung(t, f)
	lesefehlerAb(t, f.db, 2) // 1st: ownership check, 2nd: locked read in the transaction
	comp := f.comp
	comp.Status = f.m.Checks[0].FailedStatus
	f.m.recover(f.m.Checks[0], comp, recoveredReason)
	lesefehlerAus(t, f.db)
	assert.Contains(t, buf.String(), "Erholung nicht gespeichert")
	assert.NotContains(t, buf.String(), "GEHEIM123")
	assert.Equal(t, "major_outage", f.status(t))
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// Re-review (2): a soft-deleted outage marker counts (as in readValue) as
// present; an incident is still created.
func TestMonitor_SoftGeloeschteStoerungsMarkeBlockiertVorfallNicht(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.NoError(t, f.db.Where("key = ?", failingKeyPrefix+testSlug).Delete(&models.SystemSetting{}).Error)
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(5)
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "1 = 1"))
}

// Re-review (2): the upsert resets a soft-deleted marker.
func TestSetValue_HebtSoftDeleteAuf(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, setValue(f.db, "selfmonitor.x", "1"))
	require.NoError(t, f.db.Where("key = ?", "selfmonitor.x").Delete(&models.SystemSetting{}).Error)
	require.NoError(t, setValue(f.db, "selfmonitor.x", "2"))
	assert.EqualValues(t, 1, f.count(t, &models.SystemSetting{}, "key = ? AND value = ?", "selfmonitor.x", "2"))
}

// Re-review (3): the REAL openMetrics opens a FIFO without a writer immediately
// (O_NONBLOCK); a blocking open would not return.
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
		t.Cleanup(func() { // release the hanging open (error case)
			if w, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil { //nolint:gosec // Test path
				_ = w.Close()
			}
		})
		t.Fatal("openMetrics blockiert an einem FIFO ohne Schreiber")
	}
}

// Re-review (4): with several soft-deleted components with the same slug, the most recent counts.
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
