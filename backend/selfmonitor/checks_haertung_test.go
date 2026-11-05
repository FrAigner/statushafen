package selfmonitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testMaxMetrics spiegelt das Limit der Metrikdatei (64 KiB).
const testMaxMetrics = 64 << 10

func backupCheckFuer(dir string) func(context.Context) Result {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	return BackupCheck(dir, "backup-prod", 36*time.Hour, func() time.Time { return now })
}

// Betreiber-Entscheidung: Fehlt das Metrik-Verzeichnis bei gesetztem
// Job (Mount fehlt), ist das ein Fehlschlag mit festem Text ohne Pfad. Eine fehlende
// Datei im vorhandenen Verzeichnis bleibt "nicht prüfbar".
func TestBackupCheck_FehlendesVerzeichnisIstFehlschlag(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "geheim-mount")
	res := backupCheckFuer(dir)(context.Background())
	assert.False(t, res.Skip)
	assert.False(t, res.OK)
	assert.Equal(t, "Backup-Metrik-Verzeichnis fehlt", res.Detail)
	assert.NotContains(t, res.Detail, "geheim-mount")

	require.NoError(t, os.Mkdir(dir, 0o750))
	assert.True(t, backupCheckFuer(dir)(context.Background()).Skip, "Verzeichnis da, Datei fehlt: weiter nicht prüfbar")
}

// Ein Symlink wird nicht verfolgt.
func TestBackupCheck_SymlinkWirdNichtVerfolgt(t *testing.T) {
	dir := t.TempDir()
	ziel := filepath.Join(t.TempDir(), "echt.prom")
	require.NoError(t, os.WriteFile(ziel, []byte("x"), 0o600))
	require.NoError(t, os.Symlink(ziel, filepath.Join(dir, "job_backup-prod.prom")))
	res := backupCheckFuer(dir)(context.Background())
	assert.False(t, res.Skip)
	assert.False(t, res.OK)
	assert.Equal(t, "Backup-Metrik ist keine reguläre Datei", res.Detail)
	assert.NotContains(t, res.Detail, dir)
}

// Ein FIFO blockiert den Monitor nicht (os.ReadFile hängt ohne Schreiber).
func TestBackupCheck_FifoBlockiertNicht(t *testing.T) {
	dir := t.TempDir()
	pfad := filepath.Join(dir, "job_backup-prod.prom")
	require.NoError(t, syscall.Mkfifo(pfad, 0o600))
	t.Cleanup(func() { // hängenden Leser (alter Stand) freigeben
		if f, err := os.OpenFile(pfad, os.O_RDWR, 0); err == nil { //nolint:gosec // Testpfad
			_ = f.Close()
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
		t.Fatal("BackupCheck blockiert an einem FIFO")
	}
}

// Eine riesige Datei wird nicht eingelesen, sondern abgelehnt.
func TestBackupCheck_ZuGrosseDateiWirdAbgelehnt(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	writeProm(t, dir, "backup-prod", now.Unix(), now.Unix(), 0)
	pfad := filepath.Join(dir, "job_backup-prod.prom")
	alt, err := os.ReadFile(pfad) //nolint:gosec // Testpfad
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(pfad, append(alt, []byte(strings.Repeat("# fuellung\n", 10000))...), 0o600))
	res := backupCheckFuer(dir)(context.Background())
	assert.False(t, res.OK, "gültiger Inhalt, aber über dem Limit")
	assert.Equal(t, "Backup-Metrik ist zu groß", res.Detail)
}

// Grenze genau am Limit bleibt lesbar.
func TestBackupCheck_DateiAmLimitIstLesbar(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	writeProm(t, dir, "backup-prod", now.Unix(), now.Unix(), 0)
	pfad := filepath.Join(dir, "job_backup-prod.prom")
	alt, err := os.ReadFile(pfad) //nolint:gosec // Testpfad
	require.NoError(t, err)
	fuell := strings.Repeat("#", testMaxMetrics-len(alt)-1) + "\n"
	require.NoError(t, os.WriteFile(pfad, append(alt, []byte(fuell)...), 0o600))
	st, err := os.Stat(pfad)
	require.NoError(t, err)
	require.EqualValues(t, testMaxMetrics, st.Size())
	assert.True(t, backupCheckFuer(dir)(context.Background()).OK)
}

// nicht lesbare Datei (kein Leserecht) bleibt Fehlschlag mit festem Text.
func TestBackupCheck_NichtLesbarOhnePfad(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignoriert Dateirechte")
	}
	dir := t.TempDir()
	pfad := filepath.Join(dir, "job_backup-prod.prom")
	require.NoError(t, os.WriteFile(pfad, []byte("x"), 0o600))
	require.NoError(t, os.Chmod(pfad, 0o000))
	t.Cleanup(func() { _ = os.Chmod(pfad, 0o600) })
	res := backupCheckFuer(dir)(context.Background())
	assert.Equal(t, "Backup-Metrik nicht lesbar", res.Detail)
	assert.NotContains(t, res.Detail, dir)
}

type kaputterLeser struct{}

func (kaputterLeser) Read([]byte) (int, error) { return 0, syscall.EAGAIN }

// Ein Lesefehler (z. B. EAGAIN eines untergeschobenen FIFO) ist
// "nicht lesbar" mit festem Text.
func TestReadLimited_LesefehlerIstFehlschlag(t *testing.T) {
	_, res, ok := readLimited(kaputterLeser{})
	assert.False(t, ok)
	assert.Equal(t, "Backup-Metrik nicht lesbar", res.Detail)
}

// Ist der Metrik-"Ordner" eine Datei, scheitert die Prüfung mit
// festem Text (ENOTDIR ist nicht "Datei fehlt").
func TestBackupCheck_VerzeichnisIstDatei(t *testing.T) {
	datei := filepath.Join(t.TempDir(), "keinordner")
	require.NoError(t, os.WriteFile(datei, []byte("x"), 0o600))
	res := backupCheckFuer(datei)(context.Background())
	assert.False(t, res.Skip)
	assert.Equal(t, "Backup-Metrik nicht lesbar", res.Detail)
}
