package externalstatus

import (
	"errors"
	"testing"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fixture: zwei Statusseiten mit je einer Quelle, damit jede Abfrage auch
// gegen die Mandantengrenze geprüft wird.
type storeFixture struct {
	db              *gorm.DB
	page, otherPage models.StatusPage
	src, otherSrc   models.ExternalSource
	comp, otherComp models.Component
	group           models.Component
	ext, otherExt   models.ExternalComponent
	clock           time.Time
}

func newStoreFixture(t *testing.T) *storeFixture {
	t.Helper()
	f := &storeFixture{db: testutils.SetupTestDB(), clock: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	f.page = models.StatusPage{Name: "Eigene", Slug: "eigene"}
	f.otherPage = models.StatusPage{Name: "Fremde", Slug: "fremde"}
	require.NoError(t, f.db.Create(&f.page).Error)
	require.NoError(t, f.db.Create(&f.otherPage).Error)
	f.src = models.ExternalSource{StatusPageID: f.page.ID, Name: "Example Cloud", BaseURL: "https://example.com", Provider: ProviderSummaryV2, PollIntervalSeconds: 120, Active: true}
	f.otherSrc = models.ExternalSource{StatusPageID: f.otherPage.ID, Name: "Fremdquelle", BaseURL: "https://example.com", Provider: ProviderSummaryV2, PollIntervalSeconds: 120, Active: true}
	require.NoError(t, f.db.Create(&f.src).Error)
	require.NoError(t, f.db.Create(&f.otherSrc).Error)
	f.comp = models.Component{StatusPageID: f.page.ID, Name: "API", Status: componentstatus.Operational}
	f.otherComp = models.Component{StatusPageID: f.otherPage.ID, Name: "Fremd-API", Status: componentstatus.Operational}
	f.group = models.Component{StatusPageID: f.page.ID, Name: "Gruppe", Status: componentstatus.Group}
	require.NoError(t, f.db.Create(&f.comp).Error)
	require.NoError(t, f.db.Create(&f.otherComp).Error)
	require.NoError(t, f.db.Create(&f.group).Error)
	f.ext = models.ExternalComponent{ExternalSourceID: f.src.ID, ExternalID: "vm", Name: "Service 03", GroupName: "Group A", Status: componentstatus.Operational, URL: "https://status.example.com", LastSeenAt: f.clock}
	f.otherExt = models.ExternalComponent{ExternalSourceID: f.otherSrc.ID, ExternalID: "vm", Name: "Fremd-VM", Status: componentstatus.MajorOutage, LastSeenAt: f.clock}
	require.NoError(t, f.db.Create(&f.ext).Error)
	require.NoError(t, f.db.Create(&f.otherExt).Error)
	return f
}

func (f *storeFixture) link(t *testing.T, pageID, compID, extID uint) models.ExternalComponentLink {
	t.Helper()
	l := models.ExternalComponentLink{StatusPageID: pageID, ComponentID: compID, ExternalComponentID: extID}
	require.NoError(t, f.db.Create(&l).Error)
	return l
}

func (f *storeFixture) setExtStatus(t *testing.T, ext models.ExternalComponent, status string) {
	t.Helper()
	require.NoError(t, f.db.Model(&models.ExternalComponent{}).Where("id = ?", ext.ID).Update("status", status).Error)
}

// failOn lässt die nächsten GORM-Operationen op ("create", "update",
// "query") auf table scheitern, bis die Rückgabefunktion aufgerufen wird.
// skip überspringt die ersten Treffer.
func failOn(t *testing.T, db *gorm.DB, op, table string, skip int) {
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
	case "create":
		err = db.Callback().Create().Before("gorm:create").Register(name, hook)
	case "update":
		err = db.Callback().Update().Before("gorm:update").Register(name, hook)
	case "row":
		err = db.Callback().Row().Before("gorm:row").Register(name, hook)
	case "delete":
		err = db.Callback().Delete().Before("gorm:delete").Register(name, hook)
	default:
		err = db.Callback().Query().Before("gorm:query").Register(name, hook)
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(name)
		_ = db.Callback().Update().Remove(name)
		_ = db.Callback().Query().Remove(name)
		_ = db.Callback().Row().Remove(name)
		_ = db.Callback().Delete().Remove(name)
	})
}

func summaryOf(comps ...Component) *Summary {
	return &Summary{PageURL: "https://status.example.com", Components: comps}
}

func comp(id, name, status string) Component {
	return Component{ExternalID: id, Name: name, RawStatus: status, Status: NormalizeSummaryV2Status(status)}
}

func extRows(t *testing.T, db *gorm.DB, srcID uint) map[string]models.ExternalComponent {
	t.Helper()
	var rows []models.ExternalComponent
	require.NoError(t, db.Where("external_source_id = ?", srcID).Find(&rows).Error)
	out := map[string]models.ExternalComponent{}
	for _, r := range rows {
		out[r.ExternalID] = r
	}
	return out
}

func TestUpsertComponents_VerschwindenUndWiederauftauchen(t *testing.T) {
	f := newStoreFixture(t)
	now := f.clock.Add(time.Minute)
	sum := summaryOf(
		Component{ExternalID: "vm", Name: "Virtual Machine 2", IsGroup: false, GroupExternalID: "g", GroupName: "Group A", Position: 3, RawStatus: "partial_outage", Status: componentstatus.PartialOutage},
		comp("db", "Service 04", "operational"),
		comp("db", "MariaDB doppelt", "major_outage"),
		Component{ExternalID: "g", Name: "Group A", IsGroup: true, RawStatus: "partial_outage", Status: componentstatus.PartialOutage},
	)

	res, err := upsertComponents(f.db, f.src, sum, now)
	require.NoError(t, err)
	assert.Equal(t, UpsertResult{Seen: 3, Created: 2, Vanished: 0}, res, "doppelte ID zählt einmal, vm existiert schon")
	rows := extRows(t, f.db, f.src.ID)
	require.Len(t, rows, 3)
	vm := rows["vm"]
	assert.Equal(t, f.ext.ID, vm.ID, "bestehende Zeile wird aktualisiert, nicht neu angelegt")
	assert.Equal(t, "Virtual Machine 2", vm.Name)
	assert.Equal(t, "g", vm.GroupExternalID)
	assert.Equal(t, "Group A", vm.GroupName)
	assert.Equal(t, 3, vm.Position)
	assert.Equal(t, "partial_outage", vm.RawStatus)
	assert.Equal(t, componentstatus.PartialOutage, vm.Status)
	assert.Equal(t, "https://status.example.com", vm.URL)
	assert.True(t, vm.LastSeenAt.Equal(now))
	assert.Equal(t, "Service 04", rows["db"].Name, "erstes Vorkommen gewinnt")
	assert.True(t, rows["g"].IsGroup)

	// Zweimal hintereinander: idempotent.
	res, err = upsertComponents(f.db, f.src, sum, now)
	require.NoError(t, err)
	assert.Equal(t, UpsertResult{Seen: 3}, res)
	assert.Len(t, extRows(t, f.db, f.src.ID), 3)

	// db fehlt → verschwunden, nicht gelöscht.
	later := now.Add(time.Minute)
	res, err = upsertComponents(f.db, f.src, summaryOf(sum.Components[0], sum.Components[3]), later)
	require.NoError(t, err)
	assert.Equal(t, UpsertResult{Seen: 2, Vanished: 1}, res)
	db := extRows(t, f.db, f.src.ID)["db"]
	assert.True(t, db.Vanished)
	require.NotNil(t, db.VanishedAt)
	assert.True(t, db.VanishedAt.Equal(later))
	assert.True(t, db.LastSeenAt.Equal(now), "LastSeenAt bleibt der letzte Sichtkontakt")

	// Nochmals ohne db: bereits verschwunden, zählt nicht erneut.
	res, err = upsertComponents(f.db, f.src, summaryOf(sum.Components[0], sum.Components[3]), later)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Vanished)

	// Wiederauftauchen: Markierung zurück, keine neue Zeile.
	res, err = upsertComponents(f.db, f.src, sum, later)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Created)
	db2 := extRows(t, f.db, f.src.ID)["db"]
	assert.Equal(t, db.ID, db2.ID)
	assert.False(t, db2.Vanished)
	assert.Nil(t, db2.VanishedAt)

	// Fremde Quelle bleibt unberührt.
	other := extRows(t, f.db, f.otherSrc.ID)["vm"]
	assert.Equal(t, "Fremd-VM", other.Name)
	assert.False(t, other.Vanished)
}

func TestUpsertComponents_LinkFallbackAufBaseURL(t *testing.T) {
	f := newStoreFixture(t)
	_, err := upsertComponents(f.db, f.src, &Summary{Components: []Component{comp("neu", "Neu", "operational")}}, f.clock)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", extRows(t, f.db, f.src.ID)["neu"].URL)
}

func TestUpsertComponents_DBFehler(t *testing.T) {
	cases := []struct {
		name, op string
		sum      *Summary
	}{
		{"Lesen", "query", summaryOf()},
		{"Anlegen", "create", summaryOf(comp("neu", "Neu", "operational"))},
		{"Aktualisieren", "update", summaryOf(comp("vm", "VM", "operational"))},
		{"Verschwinden", "update", summaryOf()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newStoreFixture(t)
			failOn(t, f.db, tc.op, "external_components", 0)
			_, err := upsertComponents(f.db, f.src, tc.sum, f.clock)
			require.Error(t, err)
		})
	}
}

func TestLinkedRefs_Mandantentrennung(t *testing.T) {
	f := newStoreFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.ext.ID)
	// Manipulierte Kopplung: Link auf Seite 1, externe Komponente aus der
	// Quelle von Seite 2 - darf nie zählen.
	f.link(t, f.page.ID, f.comp.ID, f.otherExt.ID)
	f.link(t, f.otherPage.ID, f.otherComp.ID, f.otherExt.ID)

	refs, err := LinkedRefs(f.db, f.page.ID)
	require.NoError(t, err)
	require.Len(t, refs[f.comp.ID], 1)
	r := refs[f.comp.ID][0]
	assert.Equal(t, models.ExternalStatusRef{
		ExternalComponentID: f.ext.ID, SourceID: f.src.ID, SourceName: "Example Cloud", Name: "Service 03",
		GroupName: "Group A", Status: componentstatus.Operational, URL: "https://status.example.com",
	}, r)
	assert.NotContains(t, refs, f.otherComp.ID)

	other, err := LinkedRefs(f.db, f.otherPage.ID)
	require.NoError(t, err)
	assert.Len(t, other[f.otherComp.ID], 1)
	assert.Equal(t, "Fremd-VM", other[f.otherComp.ID][0].Name)

	// Verschwunden: enthalten, mit Markierung.
	require.NoError(t, f.db.Model(&models.ExternalComponent{}).Where("id = ?", f.ext.ID).Update("vanished", true).Error)
	refs, err = LinkedRefs(f.db, f.page.ID)
	require.NoError(t, err)
	assert.True(t, refs[f.comp.ID][0].Vanished)

	// Inaktive Quelle zählt nicht.
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("active", false).Error)
	refs, err = LinkedRefs(f.db, f.page.ID)
	require.NoError(t, err)
	assert.Empty(t, refs)

	// Gelöschte Kopplung zählt nicht.
	require.NoError(t, f.db.Where("component_id = ?", f.otherComp.ID).Delete(&models.ExternalComponentLink{}).Error)
	other, err = LinkedRefs(f.db, f.otherPage.ID)
	require.NoError(t, err)
	assert.Empty(t, other)
}

func TestLinkedRefs_GeloeschteQuelleUndKomponente(t *testing.T) {
	f := newStoreFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.ext.ID)
	require.NoError(t, f.db.Delete(&f.ext).Error)
	refs, err := LinkedRefs(f.db, f.page.ID)
	require.NoError(t, err)
	assert.Empty(t, refs, "gelöschte externe Komponente")

	g := newStoreFixture(t)
	g.link(t, g.page.ID, g.comp.ID, g.ext.ID)
	require.NoError(t, g.db.Delete(&g.src).Error)
	refs, err = LinkedRefs(g.db, g.page.ID)
	require.NoError(t, err)
	assert.Empty(t, refs, "gelöschte Quelle")
}

// Effective: alle Kombinationen eigener × externer Status.
func TestEffective_AlleKombinationen(t *testing.T) {
	all := []string{componentstatus.Operational, componentstatus.UnderMaintenance, componentstatus.DegradedPerformance,
		componentstatus.PartialOutage, componentstatus.MajorOutage, componentstatus.Unknown}
	for _, own := range all {
		for _, ext := range all {
			got := Effective(own, []models.ExternalStatusRef{{Status: ext}})
			want := own
			if componentstatus.Severity(ext) > componentstatus.Severity(own) {
				want = ext
			}
			if componentstatus.Severity(want) == 0 {
				want = componentstatus.Operational
			}
			assert.Equal(t, want, got, "eigen=%s extern=%s", own, ext)
		}
	}
	assert.Equal(t, componentstatus.Operational, Effective(componentstatus.Operational, nil))
	assert.Equal(t, componentstatus.PartialOutage, Effective(componentstatus.Operational, []models.ExternalStatusRef{
		{Status: componentstatus.UnderMaintenance}, {Status: componentstatus.PartialOutage}, {Status: componentstatus.DegradedPerformance},
	}), "mehrere Kopplungen: die schlimmste zählt")
	assert.Equal(t, componentstatus.Operational, Effective(componentstatus.Operational, []models.ExternalStatusRef{
		{Status: componentstatus.MajorOutage, Vanished: true},
	}), "verschwundene zählen nicht")
}

func TestEffectiveStatuses(t *testing.T) {
	f := newStoreFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.ext.ID)
	second := models.Component{StatusPageID: f.page.ID, Name: "Web", Status: componentstatus.Operational}
	require.NoError(t, f.db.Create(&second).Error)
	inc := models.Incident{StatusPageID: f.page.ID, Title: "t", Message: "m", Status: "investigating"}
	require.NoError(t, f.db.Create(&inc).Error)
	require.NoError(t, f.db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: second.ID}).Error)

	got, err := EffectiveStatuses(f.db, f.page.ID)
	require.NoError(t, err)
	assert.Equal(t, map[uint]string{f.comp.ID: componentstatus.Operational, second.ID: componentstatus.DegradedPerformance}, got,
		"Gruppen fehlen, offener Vorfall überlagert")

	// der Schweregrad des Vorfalls bestimmt den Status.
	require.NoError(t, f.db.Model(&inc).Update("severity", componentstatus.PartialOutage).Error)
	got, err = EffectiveStatuses(f.db, f.page.ID)
	require.NoError(t, err)
	assert.Equal(t, componentstatus.PartialOutage, got[second.ID])

	f.setExtStatus(t, f.ext, componentstatus.MajorOutage)
	got, err = EffectiveStatuses(f.db, f.page.ID)
	require.NoError(t, err)
	assert.Equal(t, componentstatus.MajorOutage, got[f.comp.ID])

	var stored models.Component
	require.NoError(t, f.db.First(&stored, f.comp.ID).Error)
	assert.Equal(t, componentstatus.Operational, stored.Status, "gespeicherter eigener Status bleibt unverändert")
}

func TestEffectiveStatuses_DBFehler(t *testing.T) {
	f := newStoreFixture(t)
	failOn(t, f.db, "query", "components", 0)
	_, err := EffectiveStatuses(f.db, f.page.ID)
	require.Error(t, err)

	g := newStoreFixture(t)
	require.NoError(t, g.db.Migrator().DropTable(&models.ExternalComponentLink{}))
	_, err = EffectiveStatuses(g.db, g.page.ID)
	require.Error(t, err, "LinkedRefs-Fehler wird weitergereicht")
}
