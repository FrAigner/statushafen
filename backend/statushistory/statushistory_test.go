package statushistory_test

import (
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/statushistory"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func newComp(t *testing.T, db *gorm.DB, status string) models.Component {
	t.Helper()
	page := models.StatusPage{Name: "P", Slug: "p-" + t.Name()}
	require.NoError(t, db.FirstOrCreate(&page, models.StatusPage{Slug: page.Slug}).Error)
	c := models.Component{StatusPageID: page.ID, Name: "C", Status: status}
	require.NoError(t, db.Create(&c).Error)
	return c
}

func rows(t *testing.T, db *gorm.DB, compID uint) []models.ComponentStatusChange {
	t.Helper()
	var r []models.ComponentStatusChange
	require.NoError(t, db.Where("component_id = ?", compID).Order("id").Find(&r).Error)
	return r
}

func TestSet_SchreibtGenauEinenEintragProAenderung(t *testing.T) {
	db := testutils.SetupTestDB()
	c := newComp(t, db, "operational")

	changed, err := statushistory.Set(db, c.ID, "", "major_outage", models.StatusSourceWebhook, t0)
	require.NoError(t, err)
	assert.True(t, changed)

	r := rows(t, db, c.ID)
	require.Len(t, r, 1)
	assert.Equal(t, "operational", r[0].OldStatus)
	assert.Equal(t, "major_outage", r[0].NewStatus)
	assert.Equal(t, models.StatusSourceWebhook, r[0].Source)
	assert.True(t, t0.Equal(r[0].ChangedAt))

	var cur models.Component
	require.NoError(t, db.First(&cur, c.ID).Error)
	assert.Equal(t, "major_outage", cur.Status)
}

func TestSet_UnveraenderterStatusSchreibtNichts(t *testing.T) {
	db := testutils.SetupTestDB()
	c := newComp(t, db, "major_outage")
	changed, err := statushistory.Set(db, c.ID, "", "major_outage", models.StatusSourceManual, t0)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Empty(t, rows(t, db, c.ID))
}

func TestSet_BedingtNurWennStatusPasst(t *testing.T) {
	db := testutils.SetupTestDB()
	c := newComp(t, db, "under_maintenance") // Admin hat Wartung gesetzt

	changed, err := statushistory.Set(db, c.ID, "operational", "major_outage", models.StatusSourceSelfMonitor, t0)
	require.NoError(t, err)
	assert.False(t, changed, "Bedingung nicht erfüllt: Admin-Status bleibt")
	assert.Empty(t, rows(t, db, c.ID))
	var cur models.Component
	require.NoError(t, db.First(&cur, c.ID).Error)
	assert.Equal(t, "under_maintenance", cur.Status)
}

func TestSet_GruppeUndFehlendeKomponenteBleibenUnberuehrt(t *testing.T) {
	db := testutils.SetupTestDB()
	g := newComp(t, db, "group")
	changed, err := statushistory.Set(db, g.ID, "", "major_outage", models.StatusSourceManual, t0)
	require.NoError(t, err)
	assert.False(t, changed)
	var cur models.Component
	require.NoError(t, db.First(&cur, g.ID).Error)
	assert.Equal(t, "group", cur.Status)

	changed, err = statushistory.Set(db, 99999, "", "major_outage", models.StatusSourceManual, t0)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Zero(t, countAll(t, db))
}

func countAll(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.ComponentStatusChange{}).Count(&n).Error)
	return n
}

// Transaktionssicherheit: scheitert der Historieneintrag, bleibt auch der
// Status unverändert (und umgekehrt gibt es keinen Eintrag ohne Änderung).
func TestSet_HistorieFehlerRolltStatusZurueck(t *testing.T) {
	db := testutils.SetupTestDB()
	c := newComp(t, db, "operational")
	require.NoError(t, db.Migrator().DropTable(&models.ComponentStatusChange{}))

	changed, err := statushistory.Set(db, c.ID, "", "major_outage", models.StatusSourceManual, t0)
	require.Error(t, err)
	assert.False(t, changed)
	var cur models.Component
	require.NoError(t, db.First(&cur, c.ID).Error)
	assert.Equal(t, "operational", cur.Status)
}

func TestSet_LeseFehlerWirdGemeldet(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, db.Migrator().DropTable(&models.Component{}))
	_, err := statushistory.Set(db, 1, "", "major_outage", models.StatusSourceManual, t0)
	require.Error(t, err)
}

func TestSet_UpdateFehlerWirdGemeldet(t *testing.T) {
	db := testutils.SetupTestDB()
	c := newComp(t, db, "operational")
	// Update scheitert, Lesen nicht: Trigger blockiert Schreiben auf components.
	require.NoError(t, db.Exec(`CREATE TRIGGER block_upd BEFORE UPDATE ON components BEGIN SELECT RAISE(ABORT, 'blockiert'); END`).Error)
	changed, err := statushistory.Set(db, c.ID, "", "major_outage", models.StatusSourceManual, t0)
	require.Error(t, err)
	assert.False(t, changed)
	assert.Zero(t, countAll(t, db))
}

func TestRecord_UeberspringtGleichenStatusUndGruppen(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, statushistory.Record(db, 1, "operational", "operational", "x", t0))
	require.NoError(t, statushistory.Record(db, 1, "", "group", "x", t0))
	require.NoError(t, statushistory.Record(db, 1, "group", "operational", "x", t0))
	assert.Zero(t, countAll(t, db))

	require.NoError(t, statushistory.Record(db, 1, "", "operational", models.StatusSourceCreated, t0.In(time.FixedZone("x", 3600))))
	var r models.ComponentStatusChange
	require.NoError(t, db.First(&r).Error)
	assert.Equal(t, time.UTC, r.ChangedAt.Location(), "Zeit wird als UTC gespeichert")
}

func TestBackfill_NurBestandOhneHistorie_Idempotent(t *testing.T) {
	db := testutils.SetupTestDB()
	down := newComp(t, db, "major_outage")
	ok := newComp(t, db, "operational")
	grp := newComp(t, db, "group")
	hasHist := newComp(t, db, "degraded_performance")
	require.NoError(t, statushistory.Record(db, hasHist.ID, "operational", "degraded_performance", models.StatusSourceWebhook, t0.Add(-time.Hour)))
	gone := newComp(t, db, "major_outage")
	require.NoError(t, db.Delete(&gone).Error)

	require.NoError(t, statushistory.Backfill(db, t0))
	r := rows(t, db, down.ID)
	require.Len(t, r, 1)
	assert.Equal(t, models.StatusSourceBackfill, r[0].Source)
	assert.Equal(t, "", r[0].OldStatus)
	assert.Equal(t, "major_outage", r[0].NewStatus)
	assert.True(t, t0.Equal(r[0].ChangedAt), "Zeitpunkt des Nachtrags, nicht der Anlage")
	assert.Len(t, rows(t, db, ok.ID), 1)
	assert.Empty(t, rows(t, db, grp.ID))
	assert.Len(t, rows(t, db, hasHist.ID), 1, "vorhandene Historie bleibt unberührt")
	assert.Empty(t, rows(t, db, gone.ID))

	before := countAll(t, db)
	require.NoError(t, statushistory.Backfill(db, t0.Add(time.Hour)))
	assert.Equal(t, before, countAll(t, db), "zweiter Lauf ändert nichts")
}

func TestBackfill_FehlerWirdGemeldet(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, db.Migrator().DropTable(&models.ComponentStatusChange{}))
	require.Error(t, statushistory.Backfill(db, t0))
}

func TestPrune_BehaeltJeKomponenteDenNeuestenAltenEintrag(t *testing.T) {
	db := testutils.SetupTestDB()
	a := newComp(t, db, "operational")
	b := newComp(t, db, "operational")
	old := func(id uint, d time.Duration, to string) {
		require.NoError(t, db.Create(&models.ComponentStatusChange{ComponentID: id, NewStatus: to, ChangedAt: t0.Add(-d)}).Error)
	}
	rec := statushistory.Retention
	old(a.ID, rec+3*24*time.Hour, "major_outage")          // wird gelöscht
	old(a.ID, rec+2*24*time.Hour, "operational")           // Ausgangsstatus: bleibt
	old(a.ID, 24*time.Hour, "partial_outage")              // jung: bleibt
	old(b.ID, rec+10*24*time.Hour, "degraded_performance") // einziger, alt: bleibt

	n, err := statushistory.Prune(db, t0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	ra := rows(t, db, a.ID)
	require.Len(t, ra, 2)
	assert.Equal(t, "operational", ra[0].NewStatus)
	assert.Equal(t, "partial_outage", ra[1].NewStatus)
	assert.Len(t, rows(t, db, b.ID), 1)

	n, err = statushistory.Prune(db, t0)
	require.NoError(t, err)
	assert.Zero(t, n, "zweiter Lauf löscht nichts")
}

func TestPrune_FehlerWirdGemeldet(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, db.Migrator().DropTable(&models.ComponentStatusChange{}))
	_, err := statushistory.Prune(db, t0)
	require.Error(t, err)
}

// Prune behält den neuesten Eintrag nach Zeit, nicht nach ID.
func TestPrune_BehaeltNeuestenNachZeitNichtNachID(t *testing.T) {
	db := testutils.SetupTestDB()
	a := newComp(t, db, "operational")
	alt := t0.Add(-statushistory.Retention - 10*24*time.Hour)
	neuer := alt.Add(5 * 24 * time.Hour)
	require.NoError(t, db.Create(&models.ComponentStatusChange{ComponentID: a.ID, NewStatus: "partial_outage", ChangedAt: neuer}).Error)
	require.NoError(t, db.Create(&models.ComponentStatusChange{ComponentID: a.ID, NewStatus: "operational", ChangedAt: alt}).Error)

	n, err := statushistory.Prune(db, t0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	r := rows(t, db, a.ID)
	require.Len(t, r, 1)
	assert.Equal(t, "partial_outage", r[0].NewStatus)
}

// Webhook-Flattern darf die Tabelle nicht wachsen lassen. Der
// Status gilt sofort, nur die Historie wird verdichtet.
func TestSet_WebhookFlatternWirdVerdichtet(t *testing.T) {
	db := testutils.SetupTestDB()
	c := newComp(t, db, "operational")
	status := []string{"major_outage", "operational"}
	const wechsel = 3000
	for i := 0; i < wechsel; i++ {
		to := status[i%2]
		changed, err := statushistory.Set(db, c.ID, "", to, models.StatusSourceWebhook, t0.Add(time.Duration(i)*time.Millisecond*10))
		require.NoError(t, err)
		assert.True(t, changed, "der Statuswechsel selbst bleibt sofort wirksam (Wechsel %d)", i)
		var cur models.Component
		require.NoError(t, db.First(&cur, c.ID).Error)
		require.Equal(t, to, cur.Status)
	}
	// 3000 Wechsel in 30 s: ohne Verdichtung 3000 Zeilen.
	assert.LessOrEqual(t, len(rows(t, db, c.ID)), 2)
	// Endstand: letzter Wechsel (Index 2999) ist "operational".
	var cur models.Component
	require.NoError(t, db.First(&cur, c.ID).Error)
	assert.Equal(t, "operational", cur.Status)
}

func TestRecord_ZusammenfassenGrenzen(t *testing.T) {
	setup := func(t *testing.T) (*gorm.DB, models.Component) {
		db := testutils.SetupTestDB()
		return db, newComp(t, db, "operational")
	}
	t.Run("Anker behaelt Anfangszeit und schlimmsten Status, Nachlauf den Endstand", func(t *testing.T) {
		db, c := setup(t)
		require.NoError(t, statushistory.Record(db, c.ID, "operational", "major_outage", models.StatusSourceWebhook, t0))
		require.NoError(t, statushistory.Record(db, c.ID, "major_outage", "partial_outage", models.StatusSourceWebhook, t0.Add(statushistory.MergeGap-time.Second)))
		r := rows(t, db, c.ID)
		// Anker trägt den schlimmsten Status des Fensters, der Nachlauf den Endstand.
		require.Len(t, r, 2)
		assert.Equal(t, "operational", r[0].OldStatus)
		assert.Equal(t, "major_outage", r[0].NewStatus)
		assert.True(t, t0.Equal(r[0].ChangedAt))
		assert.Equal(t, "partial_outage", r[1].NewStatus)
	})
	t.Run("Hin und zurueck behaelt den Ausfall (B1)", func(t *testing.T) {
		db, c := setup(t)
		require.NoError(t, statushistory.Record(db, c.ID, "operational", "major_outage", models.StatusSourceWebhook, t0))
		require.NoError(t, statushistory.Record(db, c.ID, "major_outage", "operational", models.StatusSourceWebhook, t0.Add(time.Second)))
		r := rows(t, db, c.ID)
		// Nie löschen: Anker (schlimmster Status des Fensters) + Nachlauf (Endstand).
		require.Len(t, r, 2)
		assert.Equal(t, "operational", r[0].OldStatus)
		assert.Equal(t, "major_outage", r[0].NewStatus)
		assert.Equal(t, models.StatusSourceWebhook, r[0].Source)
		assert.Equal(t, "major_outage", r[1].OldStatus)
		assert.Equal(t, "operational", r[1].NewStatus)
		assert.Equal(t, models.StatusSourceWebhookNachlauf, r[1].Source)
		assert.True(t, t0.Add(time.Second).Equal(r[1].ChangedAt))
	})
	t.Run("Rueckkehr zum schlimmsten Status loescht nur den Nachlauf", func(t *testing.T) {
		db, c := setup(t)
		require.NoError(t, statushistory.Record(db, c.ID, "operational", "major_outage", models.StatusSourceWebhook, t0))
		require.NoError(t, statushistory.Record(db, c.ID, "major_outage", "operational", models.StatusSourceWebhook, t0.Add(time.Second)))
		require.NoError(t, statushistory.Record(db, c.ID, "operational", "major_outage", models.StatusSourceWebhook, t0.Add(2*time.Second)))
		r := rows(t, db, c.ID)
		require.Len(t, r, 1)
		assert.Equal(t, "major_outage", r[0].NewStatus)
	})
	t.Run("schlimmerer Zwischenstand bleibt im Anker", func(t *testing.T) {
		db, c := setup(t)
		require.NoError(t, statushistory.Record(db, c.ID, "operational", "partial_outage", models.StatusSourceWebhook, t0))
		require.NoError(t, statushistory.Record(db, c.ID, "partial_outage", "major_outage", models.StatusSourceWebhook, t0.Add(time.Second)))
		require.NoError(t, statushistory.Record(db, c.ID, "major_outage", "degraded_performance", models.StatusSourceWebhook, t0.Add(2*time.Second)))
		r := rows(t, db, c.ID)
		require.Len(t, r, 2)
		assert.Equal(t, "major_outage", r[0].NewStatus)
		assert.Equal(t, "major_outage", r[1].OldStatus)
		assert.Equal(t, "degraded_performance", r[1].NewStatus)
	})
	t.Run("genau am Mindestabstand neue Zeile", func(t *testing.T) {
		db, c := setup(t)
		require.NoError(t, statushistory.Record(db, c.ID, "operational", "major_outage", models.StatusSourceWebhook, t0))
		require.NoError(t, statushistory.Record(db, c.ID, "major_outage", "operational", models.StatusSourceWebhook, t0.Add(statushistory.MergeGap)))
		assert.Len(t, rows(t, db, c.ID), 2)
	})
	t.Run("andere Quelle wird nie verdichtet", func(t *testing.T) {
		db, c := setup(t)
		require.NoError(t, statushistory.Record(db, c.ID, "operational", "major_outage", models.StatusSourceManual, t0))
		require.NoError(t, statushistory.Record(db, c.ID, "major_outage", "partial_outage", models.StatusSourceWebhook, t0.Add(time.Second)))
		require.NoError(t, statushistory.Record(db, c.ID, "partial_outage", "operational", models.StatusSourceManual, t0.Add(2*time.Second)))
		assert.Len(t, rows(t, db, c.ID), 3)
	})
	t.Run("Uhr rueckwaerts: keine Verdichtung", func(t *testing.T) {
		db, c := setup(t)
		require.NoError(t, statushistory.Record(db, c.ID, "operational", "major_outage", models.StatusSourceWebhook, t0))
		require.NoError(t, statushistory.Record(db, c.ID, "major_outage", "operational", models.StatusSourceWebhook, t0.Add(-time.Second)))
		assert.Len(t, rows(t, db, c.ID), 2)
	})
	t.Run("andere Komponente unberuehrt", func(t *testing.T) {
		db, c := setup(t)
		c2 := newComp(t, db, "operational")
		require.NoError(t, statushistory.Record(db, c.ID, "operational", "major_outage", models.StatusSourceWebhook, t0))
		require.NoError(t, statushistory.Record(db, c2.ID, "operational", "major_outage", models.StatusSourceWebhook, t0.Add(time.Second)))
		assert.Len(t, rows(t, db, c.ID), 1)
		assert.Len(t, rows(t, db, c2.ID), 1)
	})
}

func TestRecord_WebhookLesefehlerWirdGemeldet(t *testing.T) {
	db := testutils.SetupTestDB()
	c := newComp(t, db, "operational")
	require.NoError(t, db.Migrator().DropTable(&models.ComponentStatusChange{}))
	assert.Error(t, statushistory.Record(db, c.ID, "operational", "major_outage", models.StatusSourceWebhook, t0))
}

// Ausfallsekunden aus der Historie (Status je Zeile bis zur nächsten, letzte bis ende).
func ausfallSekunden(r []models.ComponentStatusChange, ende time.Time) float64 {
	sum := 0.0
	for i, x := range r {
		bis := ende
		if i+1 < len(r) {
			bis = r[i+1].ChangedAt
		}
		if x.NewStatus == "major_outage" {
			sum += bis.Sub(x.ChangedAt).Seconds()
		}
	}
	return sum
}

// Review B1: Wiederholtes Hin- und Zurückwechseln innerhalb von MergeGap darf
// einen echten Ausfall NIE aus der Historie löschen. Früher löschte der Rückwechsel die
// Zeile, der nächste Wechsel legte eine neue an: 0 Zeilen und 0 s Ausfall für 50 min echten
// Ausfall. Jetzt: höchstens zwei Zeilen je Fenster, der Ausfall wird mindestens so lang
// gezählt wie er war (pessimistisch), und die Zeilenzahl bleibt begrenzt.
func TestSet_DauerflatternVerliertKeinenAusfall(t *testing.T) {
	for _, tc := range []struct {
		name         string
		aus, betrieb time.Duration
	}{
		{"25 s Ausfall / 5 s Betrieb", 25 * time.Second, 5 * time.Second},
		{"29 s Ausfall / 1 s Betrieb", 29 * time.Second, 1 * time.Second},
		{"10 s Ausfall / 10 s Betrieb", 10 * time.Second, 10 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			c := newComp(t, db, "operational")
			now := t0
			echt := 0.0
			ende := t0.Add(time.Hour)
			for now.Before(ende) {
				ok, err := statushistory.Set(db, c.ID, "", "major_outage", models.StatusSourceWebhook, now)
				require.NoError(t, err)
				require.True(t, ok)
				now = now.Add(tc.aus)
				echt += tc.aus.Seconds()
				ok, err = statushistory.Set(db, c.ID, "", "operational", models.StatusSourceWebhook, now)
				require.NoError(t, err)
				require.True(t, ok)
				now = now.Add(tc.betrieb)
			}
			r := rows(t, db, c.ID)
			assert.NotEmpty(t, r, "der Ausfall darf nicht komplett verschwinden")
			assert.GreaterOrEqual(t, ausfallSekunden(r, now), echt, "Ausfallzeit darf nie unter dem echten Ausfall liegen")
			// höchstens zwei Zeilen je MergeGap-Fenster (+ Anfang)
			maxZeilen := int(2*now.Sub(t0)/statushistory.MergeGap) + 2
			assert.LessOrEqual(t, len(r), maxZeilen, "Zeilenzahl bleibt begrenzt")
		})
	}
}
