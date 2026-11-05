package externalstatus

import (
	"context"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Aufbewahrung beendeter gespiegelter Meldungen.

// MirroredIncidentsPruned macht den Recorder zum RetentionReporter.
func (r *incRecorder) MirroredIncidentsPruned(_ context.Context, count int, retention time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruned = append(r.pruned, count)
	r.retention = retention
}

func (f *incFixture) mirror(t *testing.T, extID string, closed bool, resolved, updated *time.Time) models.ExternalIncident {
	t.Helper()
	row := models.ExternalIncident{StatusPageID: f.page.ID, ExternalSourceID: f.src.ID, ExternalID: extID, Kind: models.ExternalIncidentKindIncident,
		Title: extID, Status: "resolved", Closed: closed, ResolvedAt: resolved, ExternalUpdatedAt: updated, LastSeenAt: f.now}
	require.NoError(t, f.db.Create(&row).Error)
	require.NoError(t, f.db.Create(&models.ExternalIncidentUpdate{ExternalIncidentID: row.ID, ExternalID: extID + "-u", Body: "x", DisplayAt: f.now}).Error)
	require.NoError(t, f.db.Create(&models.ExternalIncidentComponent{ExternalIncidentID: row.ID, ComponentExternalID: "vm", Name: "VM"}).Error)
	return row
}

func daysAgo(f *incFixture, d int) *time.Time {
	t := f.now.Add(-time.Duration(d) * 24 * time.Hour)
	return &t
}

func (f *incFixture) countAll(t *testing.T, model any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Unscoped().Model(model).Count(&n).Error)
	return n
}

func TestPruneMirrored_EntferntNurAlteBeendete(t *testing.T) {
	f := newIncFixture(t)
	old := f.mirror(t, "alt", true, daysAgo(f, 91), nil)
	f.mirror(t, "grenze-neu", true, daysAgo(f, 89), nil)
	f.mirror(t, "offen-uralt", false, daysAgo(f, 400), daysAgo(f, 400))
	// Ohne resolved_at zählt das letzte Update der Fremdseite.
	f.mirror(t, "nur-update-alt", true, nil, daysAgo(f, 120))
	f.mirror(t, "nur-update-neu", true, nil, daysAgo(f, 5))

	n, err := f.svc.PruneMirrored(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	var ids []string
	require.NoError(t, f.db.Unscoped().Model(&models.ExternalIncident{}).Order("id").Pluck("external_id", &ids).Error)
	assert.Equal(t, []string{"grenze-neu", "offen-uralt", "nur-update-neu"}, ids, "offene bleiben, junge bleiben")
	assert.EqualValues(t, 3, f.countAll(t, &models.ExternalIncidentUpdate{}), "Updates der entfernten Meldungen sind weg (hart)")
	assert.EqualValues(t, 3, f.countAll(t, &models.ExternalIncidentComponent{}))
	var left int64
	require.NoError(t, f.db.Unscoped().Model(&models.ExternalIncidentUpdate{}).Where("external_incident_id = ?", old.ID).Count(&left).Error)
	assert.Zero(t, left)

	n, err = f.svc.PruneMirrored(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n, "idempotent")
}

func TestPruneMirrored_LetzterAbrufAlsFallbackUndBatches(t *testing.T) {
	f := newIncFixture(t)
	// Weder resolved_at noch Update-Zeit: last_seen_at entscheidet.
	row := f.mirror(t, "ohne-zeiten", true, nil, nil)
	require.NoError(t, f.db.Model(&row).Update("last_seen_at", *daysAgo(f, 200)).Error)
	for i := 0; i < pruneBatchSize+50; i++ {
		rows := models.ExternalIncident{StatusPageID: f.page.ID, ExternalSourceID: f.src.ID, ExternalID: "b" + time.Duration(i).String(),
			Title: "b", Closed: true, ResolvedAt: daysAgo(f, 300), LastSeenAt: f.now}
		require.NoError(t, f.db.Create(&rows).Error)
	}
	n, err := f.svc.PruneMirrored(context.Background())
	require.NoError(t, err)
	assert.Equal(t, pruneBatchSize+51, n, "mehr als ein Batch")
	assert.Zero(t, f.countAll(t, &models.ExternalIncident{}))
}

func TestPruneMirrored_Fehler(t *testing.T) {
	for _, tc := range []struct{ name, op, table string }{
		{"Auswahl", "query", "external_incidents"},
		{"Updates löschen", "delete", "external_incident_updates"},
		{"Komponenten löschen", "delete", "external_incident_components"},
		{"Meldungen löschen", "delete", "external_incidents"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newIncFixture(t)
			f.mirror(t, "alt", true, daysAgo(f, 200), nil)
			failOn(t, f.db, tc.op, tc.table, 0)
			_, err := f.svc.PruneMirrored(context.Background())
			require.Error(t, err)
		})
	}
}

func TestPruneMirrored_AbgebrochenerKontext(t *testing.T) {
	f := newIncFixture(t)
	f.mirror(t, "alt", true, daysAgo(f, 200), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := f.svc.PruneMirrored(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestRetentionFromEnv(t *testing.T) {
	get := func(v string) func(string) string {
		return func(k string) string {
			require.Equal(t, RetentionDaysEnv, k)
			return v
		}
	}
	day := 24 * time.Hour
	assert.Equal(t, 90*day, RetentionFromEnv(get("")))
	assert.Equal(t, 30*day, RetentionFromEnv(get(" 30 ")))
	assert.Equal(t, 90*day, RetentionFromEnv(get("0")))
	assert.Equal(t, 90*day, RetentionFromEnv(get("-5")))
	assert.Equal(t, 90*day, RetentionFromEnv(get("abc")))
	assert.Equal(t, 90*day, DefaultRetentionDays*day)
}

func TestNewService_RetentionStandardUndOverride(t *testing.T) {
	f := newIncFixture(t)
	assert.Equal(t, 90*24*time.Hour, f.svc.retention)
	assert.Equal(t, 7*24*time.Hour, NewService(f.db, Options{Retention: 7 * 24 * time.Hour}).retention)
}

func TestRunOnce_RaeumtStuendlichAufUndMeldetAnzahl(t *testing.T) {
	f := newIncFixture(t)
	f.mirror(t, "alt", true, daysAgo(f, 200), nil)
	f.prov.set(models.ExternalIncidentKindIncident)
	ctx := context.Background()

	f.svc.RunOnce(ctx)
	assert.Zero(t, f.countAll(t, &models.ExternalIncident{}))
	assert.Equal(t, []int{1}, f.rec.pruned)
	assert.Equal(t, 90*24*time.Hour, f.rec.retention)

	// Innerhalb der Stunde: nicht erneut (auch nicht bei neuem Altbestand).
	f.mirror(t, "alt2", true, daysAgo(f, 200), nil)
	f.now = f.now.Add(10 * time.Minute)
	f.svc.RunOnce(ctx)
	assert.EqualValues(t, 1, f.countAll(t, &models.ExternalIncident{}))

	f.now = f.now.Add(pruneEvery)
	f.svc.RunOnce(ctx)
	assert.Zero(t, f.countAll(t, &models.ExternalIncident{}))
	assert.Equal(t, []int{1, 1}, f.rec.pruned)
}

func TestRunOnce_NichtLeaderRaeumtNichtAuf(t *testing.T) {
	f := newIncFixture(t)
	f.mirror(t, "alt", true, daysAgo(f, 200), nil)
	f.locker.ok = false
	f.svc.RunOnce(context.Background())
	assert.EqualValues(t, 1, f.countAll(t, &models.ExternalIncident{}), "nur der Halter der Advisory-Sperre räumt auf")
}

func TestMaybePrune_FehlerUndOhneReporter(t *testing.T) {
	f := newIncFixture(t)
	f.mirror(t, "alt", true, daysAgo(f, 200), nil)
	failOn(t, f.db, "query", "external_incidents", 0)
	f.svc.maybePrune(context.Background()) // Fehler wird geloggt, kein Panic, keine Meldung
	assert.Empty(t, f.rec.pruned)

	// Notifier ohne RetentionReporter: kein Panic.
	f2 := newIncFixture(t)
	f2.mirror(t, "alt", true, daysAgo(f2, 200), nil)
	f2.svc.SetIncidentNotifier(nil)
	f2.svc.maybePrune(context.Background())
	assert.Zero(t, f2.countAll(t, &models.ExternalIncident{}))
}

// Ohne Filter beim Spiegeln würde der Feed (letzte 50 Einträge) eine längst
// aufgeräumte Meldung bei jedem Abruf neu anlegen.
func TestSyncFeed_UeberfaelligeBeendeteWerdenNichtNeuAngelegt(t *testing.T) {
	f := newIncFixture(t)
	f.prov.set(models.ExternalIncidentKindIncident)
	require.True(t, f.poll().OK) // Erstimport (leer)

	old := inc("alt", "resolved", "Behoben")
	r := f.now.Add(-100 * 24 * time.Hour)
	old.ResolvedAt = &r
	young := inc("jung", "resolved", "Behoben")
	y := f.now.Add(-2 * 24 * time.Hour)
	young.ResolvedAt = &y
	openOld := inc("offen", "investigating", "Läuft")
	o := f.now.Add(-300 * 24 * time.Hour)
	openOld.ResolvedAt, openOld.UpdatedAt = nil, &o
	f.prov.set(models.ExternalIncidentKindIncident, old, young, openOld)
	require.True(t, f.poll().OK)

	var ids []string
	require.NoError(t, f.db.Model(&models.ExternalIncident{}).Order("id").Pluck("external_id", &ids).Error)
	assert.Equal(t, []string{"jung", "offen"}, ids)
}

func TestEndedAt(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	res, upd := now.Add(-time.Hour), now.Add(-2*time.Hour)
	assert.Equal(t, res, endedAt(Incident{ResolvedAt: &res, UpdatedAt: &upd}, now))
	assert.Equal(t, upd, endedAt(Incident{UpdatedAt: &upd}, now))
	assert.Equal(t, now, endedAt(Incident{}, now), "ohne Zeiten nie überfällig")
}
