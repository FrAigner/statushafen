package externalstatus

import (
	"context"
	"errors"
	"testing"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Datenbankfehler in jedem Schritt der Spiegelung. Ein Fehler darf
// weder ein Ereignis auslösen noch halb gespeicherte Meldungen hinterlassen.

// prepareChangeState legt den Stand an, auf dem der nächste Feed alle
// Schreibpfade berührt: bestehende Meldung (Update-Text, Komponenten ändern
// sich) und eine gekoppelte externe Gruppe (Mitglieder-Abfrage).
func prepareChangeState(t *testing.T) (*incFixture, Incident) {
	t.Helper()
	f := newIncFixture(t)
	grp := models.ExternalComponent{ExternalSourceID: f.src.ID, ExternalID: "g1", Name: "Gruppe", IsGroup: true, Status: componentstatus.Operational, LastSeenAt: f.now}
	require.NoError(t, f.db.Create(&grp).Error)
	ownB := models.Component{StatusPageID: f.page.ID, Name: "Backend", Status: componentstatus.Operational}
	require.NoError(t, f.db.Create(&ownB).Error)
	f.link(t, f.page.ID, ownB.ID, grp.ID)

	first := inc("i1", "investigating", "alt")
	first.Components = []IncidentComponent{{ExternalID: "vm", Name: "VM"}, {ExternalID: "weg", Name: "Weg"}}
	f.prov.set(models.ExternalIncidentKindIncident, first)
	require.True(t, f.poll().OK)
	f.rec.take()

	next := inc("i1", "monitoring", "neu", "zweites")
	next.Updates[0].ExternalID = "i1-u0"
	next.Components = []IncidentComponent{{ExternalID: "vm", Name: "VM umbenannt"}, {ExternalID: "neu", Name: "Neu"}}
	return f, next
}

func TestApplyFeed_DatenbankfehlerJederSchritt(t *testing.T) {
	cases := []struct {
		name, op, table string
		skip            int
	}{
		{"bestehende Meldungen lesen", "query", "external_incidents", 0},
		{"Kopplungen lesen", "row", "l", 0},
		{"Gruppenmitglieder lesen", "row", "external_components", 0},
		{"Stand beanspruchen", "update", "external_incidents", 0},
		{"Meldung speichern", "update", "external_incidents", 1},
		{"Updates lesen", "query", "external_incident_updates", 0},
		{"Update ändern", "update", "external_incident_updates", 0},
		{"Update anlegen", "create", "external_incident_updates", 0},
		{"Komponenten lesen", "query", "external_incident_components", 0},
		{"Komponente umbenennen", "update", "external_incident_components", 0},
		{"Komponente entfernen", "delete", "external_incident_components", 0},
		{"Komponente anlegen", "create", "external_incident_components", 0},
		{"Ereignis laden", "query", "external_incidents", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, next := prepareChangeState(t)
			failOn(t, f.db, tc.op, tc.table, tc.skip)

			var evs []IncidentEvent
			err := f.db.Transaction(func(tx *gorm.DB) error {
				var e error
				evs, e = applyFeed(tx, f.source2(), []Incident{next}, false, f.now)
				return e
			})
			require.Error(t, err, tc.name)
			assert.Empty(t, evs)
		})
	}
}

func TestSyncIncidents_FehlerPfade(t *testing.T) {
	t.Run("Quelle nicht lesbar", func(t *testing.T) {
		f := newIncFixture(t)
		failOn(t, f.db, "query", "external_sources", 0)
		f.svc.syncIncidents(context.Background(), f.src.ID, f.now)
		assert.Empty(t, f.prov.reqs, "ohne Quelle kein Abruf")
	})
	t.Run("Fehlertext nicht speicherbar", func(t *testing.T) {
		f := newIncFixture(t)
		f.prov.errs[models.ExternalIncidentKindIncident] = errors.New("kaputt")
		failOn(t, f.db, "update", "external_sources", 0)
		f.svc.syncIncidents(context.Background(), f.src.ID, f.now)
		assert.Empty(t, f.source2().IncidentsError, "Speichern scheiterte, aber nichts stürzt ab")
	})
	t.Run("ohne Empfänger", func(t *testing.T) {
		f := newIncFixture(t)
		f.svc.SetIncidentNotifier(nil)
		f.prov.set(models.ExternalIncidentKindIncident)
		f.poll()
		f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "a"))
		require.True(t, f.poll().OK)
		assert.Len(t, f.rows(t), 1, "wird trotzdem gespiegelt")
		assert.Empty(t, f.rec.take())
	})
	t.Run("Meldung nicht speicherbar: Abruf gelingt, Fehler vermerkt, nichts halb gespeichert", func(t *testing.T) {
		f := newIncFixture(t)
		f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "a"))
		failOn(t, f.db, "create", "external_incident_updates", 0)
		res := f.poll()
		require.True(t, res.OK)
		assert.Contains(t, f.source2().IncidentsError, "incident: injizierter Fehler")
		assert.Empty(t, f.rows(t), "Transaktion zurückgerollt")
		assert.Nil(t, f.source2().IncidentsSyncedAt, "und der Erstimport zählt noch nicht als erledigt")
	})
	t.Run("Validatoren nicht speicherbar: Rollback", func(t *testing.T) {
		f := newIncFixture(t)
		f.prov.set(models.ExternalIncidentKindIncident, inc("i1", "investigating", "a"))
		failOn(t, f.db, "update", "external_sources", 1) // 0 = Abrufstatus des Komponenten-Abrufs
		f.poll()
		assert.Empty(t, f.rows(t))
	})
}

func TestApplyFeed_LeererFeedUndZeitvergleich(t *testing.T) {
	f := newIncFixture(t)
	evs, err := applyFeed(f.db, f.source2(), nil, false, f.now)
	require.NoError(t, err)
	assert.Empty(t, evs)
	assert.False(t, sameTime(nil, nil))
}

func TestOwnComponentMap_Datenbankfehler(t *testing.T) {
	f, _ := prepareChangeState(t)
	failOn(t, f.db, "row", "l", 0)
	_, err := OwnComponentMap(f.db, f.page.ID, 0)
	assert.Error(t, err)
}
