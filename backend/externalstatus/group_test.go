package externalstatus

import (
	"context"
	"testing"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Kopplung einer ganzen externen Gruppe.

// groupFixture: Gruppe "Database" (g-db) der eigenen Quelle mit drei Bausteinen,
// eine zweite Gruppe daneben und eine gleichnamige Gruppe in der fremden Quelle.
type groupFixture struct {
	*storeFixture
	grp, otherGrp, foreignGrp         models.ExternalComponent
	mariadb, postgres, redis, outside models.ExternalComponent
}

func (f *storeFixture) extComp(t *testing.T, srcID uint, extID, name, status, groupID string, isGroup bool) models.ExternalComponent {
	t.Helper()
	ec := models.ExternalComponent{ExternalSourceID: srcID, ExternalID: extID, Name: name, Status: status, RawStatus: status,
		IsGroup: isGroup, GroupExternalID: groupID, URL: "https://status.example.com", LastSeenAt: f.clock}
	if groupID != "" {
		ec.GroupName = "Database"
	}
	require.NoError(t, f.db.Create(&ec).Error)
	return ec
}

func newGroupFixture(t *testing.T) *groupFixture {
	t.Helper()
	f := &groupFixture{storeFixture: newStoreFixture(t)}
	// Die Gruppe selbst meldet einen eigenen Status; er darf nie zählen.
	f.grp = f.extComp(t, f.src.ID, "g-db", "Database", componentstatus.MajorOutage, "", true)
	f.otherGrp = f.extComp(t, f.src.ID, "g-net", "Service 18", componentstatus.Operational, "", true)
	f.mariadb = f.extComp(t, f.src.ID, "mariadb", "Service 04", componentstatus.Operational, "g-db", false)
	f.postgres = f.extComp(t, f.src.ID, "pg", "PostgreSQL", componentstatus.DegradedPerformance, "g-db", false)
	f.redis = f.extComp(t, f.src.ID, "redis", "Service 12", componentstatus.UnderMaintenance, "g-db", false)
	f.outside = f.extComp(t, f.src.ID, "lb", "Load Balancer", componentstatus.MajorOutage, "g-net", false)
	f.foreignGrp = f.extComp(t, f.otherSrc.ID, "g-db", "Database", componentstatus.Operational, "", true)
	f.extComp(t, f.otherSrc.ID, "fremd-db", "Fremd-DB", componentstatus.MajorOutage, "g-db", false)
	return f
}

func (f *groupFixture) ref(t *testing.T) models.ExternalStatusRef {
	t.Helper()
	refs, err := LinkedRefs(f.db, f.page.ID)
	require.NoError(t, err)
	require.Len(t, refs[f.comp.ID], 1)
	return refs[f.comp.ID][0]
}

func (f *groupFixture) effective(t *testing.T) string {
	t.Helper()
	m, err := EffectiveStatuses(f.db, f.page.ID)
	require.NoError(t, err)
	return m[f.comp.ID]
}

func TestGroupLink_GemischteStatusSchlimmsterGewinnt(t *testing.T) {
	f := newGroupFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.grp.ID)

	r := f.ref(t)
	assert.True(t, r.IsGroup)
	assert.Equal(t, "Database", r.Name)
	assert.Equal(t, "Database", r.GroupName)
	assert.Equal(t, f.src.ID, r.SourceID)
	assert.Equal(t, "Example Cloud", r.SourceName)
	assert.Equal(t, componentstatus.DegradedPerformance, r.Status, "Mitglieder, nicht der Eigenstatus der Gruppe")
	assert.Equal(t, &models.ExternalWorst{Name: "PostgreSQL", Status: componentstatus.DegradedPerformance}, r.Worst)
	assert.Equal(t, componentstatus.DegradedPerformance, f.effective(t))

	// Eigener Status ist schlimmer: er gewinnt weiter.
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("status", componentstatus.PartialOutage).Error)
	assert.Equal(t, componentstatus.PartialOutage, f.effective(t))

	// Ein Mitglied wird schlimmer.
	f.setExtStatus(t, f.mariadb, componentstatus.MajorOutage)
	r = f.ref(t)
	assert.Equal(t, componentstatus.MajorOutage, r.Status)
	assert.Equal(t, "Service 04", r.Worst.Name)
	assert.Equal(t, componentstatus.MajorOutage, f.effective(t))
}

func TestGroupLink_GleichstandNimmtErstesNachName(t *testing.T) {
	f := newGroupFixture(t)
	f.setExtStatus(t, f.redis, componentstatus.DegradedPerformance)
	f.link(t, f.page.ID, f.comp.ID, f.grp.ID)
	assert.Equal(t, "PostgreSQL", f.ref(t).Worst.Name, "PostgreSQL vor Redis")
}

func TestGroupLink_NeuesMitgliedZaehltSofort(t *testing.T) {
	f := newGroupFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.grp.ID)
	require.Equal(t, componentstatus.DegradedPerformance, f.effective(t))

	neu := f.extComp(t, f.src.ID, "neu", "Neuer Dienst", componentstatus.PartialOutage, "g-db", false)
	assert.Equal(t, componentstatus.PartialOutage, f.effective(t))
	assert.Equal(t, neu.Name, f.ref(t).Worst.Name)
}

func TestGroupLink_VerschwundenesMitgliedZaehltNicht(t *testing.T) {
	f := newGroupFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.grp.ID)
	require.NoError(t, f.db.Model(&models.ExternalComponent{}).Where("id = ?", f.postgres.ID).Update("vanished", true).Error)
	r := f.ref(t)
	assert.Equal(t, componentstatus.UnderMaintenance, r.Status)
	assert.Equal(t, "Service 12", r.Worst.Name)
	assert.Equal(t, componentstatus.UnderMaintenance, f.effective(t))

	// Gelöschte Mitglieder ebenso, und die Gruppe selbst verschwunden zählt gar nicht.
	require.NoError(t, f.db.Delete(&f.redis).Error)
	assert.Equal(t, componentstatus.Operational, f.ref(t).Status)
	require.NoError(t, f.db.Model(&models.ExternalComponent{}).Where("id = ?", f.mariadb.ID).Update("status", componentstatus.MajorOutage).Error)
	require.NoError(t, f.db.Model(&models.ExternalComponent{}).Where("id = ?", f.grp.ID).Update("vanished", true).Error)
	r = f.ref(t)
	assert.True(t, r.Vanished)
	assert.Equal(t, componentstatus.Operational, f.effective(t), "verschwundene Gruppe zählt nicht")
}

func TestGroupLink_LeereGruppeZaehltNichts(t *testing.T) {
	f := newGroupFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.otherGrp.ID)
	require.NoError(t, f.db.Model(&models.ExternalComponent{}).Where("id = ?", f.outside.ID).Update("vanished", true).Error)

	r := f.ref(t)
	assert.Equal(t, componentstatus.Unknown, r.Status)
	assert.Nil(t, r.Worst)
	assert.Equal(t, componentstatus.Operational, f.effective(t), "leere Gruppe löst keinen Fehlalarm aus")

	// Mitglieder mit unbekanntem Status verschlechtern ebenfalls nichts.
	f.setExtStatus(t, f.outside, componentstatus.Unknown)
	require.NoError(t, f.db.Model(&models.ExternalComponent{}).Where("id = ?", f.outside.ID).Update("vanished", false).Error)
	r = f.ref(t)
	assert.Equal(t, componentstatus.Unknown, r.Status)
	assert.Nil(t, r.Worst)
	assert.Equal(t, componentstatus.Operational, f.effective(t))
}

func TestGroupLink_Mandantentrennung(t *testing.T) {
	f := newGroupFixture(t)
	// Gleiche externe Gruppen-ID in der Quelle einer anderen Statusseite: deren
	// Mitglieder zählen nie mit.
	f.link(t, f.page.ID, f.comp.ID, f.grp.ID)
	assert.Equal(t, componentstatus.DegradedPerformance, f.effective(t))

	// Manipulierte Kopplung auf die Fremd-Gruppe: zählt nicht.
	f.link(t, f.page.ID, f.comp.ID, f.foreignGrp.ID)
	refs, err := LinkedRefs(f.db, f.page.ID)
	require.NoError(t, err)
	assert.Len(t, refs[f.comp.ID], 1)
	assert.Equal(t, componentstatus.DegradedPerformance, f.effective(t))

	// Die andere Seite sieht nur ihre eigenen Mitglieder.
	f.link(t, f.otherPage.ID, f.otherComp.ID, f.foreignGrp.ID)
	other, err := LinkedRefs(f.db, f.otherPage.ID)
	require.NoError(t, err)
	require.Len(t, other[f.otherComp.ID], 1)
	assert.Equal(t, "Fremd-DB", other[f.otherComp.ID][0].Worst.Name)

	// Inaktive Quelle zählt nicht.
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("active", false).Error)
	assert.Equal(t, componentstatus.Operational, f.effective(t))
}

func TestGroupLink_Idempotenz(t *testing.T) {
	f := newGroupFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.grp.ID)
	first := f.ref(t)
	for range 3 {
		assert.Equal(t, first, f.ref(t))
	}
	// Derselbe Abruf zweimal verändert Gruppenstatus und Ergebnis nicht.
	sum := summaryOf(
		Component{ExternalID: "g-db", Name: "Database", IsGroup: true, Status: componentstatus.Operational},
		Component{ExternalID: "mariadb", Name: "Service 04", GroupExternalID: "g-db", GroupName: "Database", Status: componentstatus.PartialOutage},
	)
	for range 2 {
		require.NoError(t, f.db.Transaction(func(tx *gorm.DB) error {
			_, err := upsertComponents(tx, f.src, sum, f.clock)
			return err
		}))
		r := f.ref(t)
		assert.Equal(t, componentstatus.PartialOutage, r.Status)
		assert.Equal(t, "Service 04", r.Worst.Name)
	}
	// PostgreSQL und Redis kamen im Abruf nicht mehr vor und zählen nicht mehr.
	assert.True(t, extRows(t, f.db, f.src.ID)["pg"].Vanished)
}

func TestGroupLink_DoppelkopplungGruppeUndBausteinAendertNichts(t *testing.T) {
	f := newGroupFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.grp.ID)
	before := f.effective(t)
	f.link(t, f.page.ID, f.comp.ID, f.postgres.ID)
	assert.Equal(t, before, f.effective(t))
	refs, err := LinkedRefs(f.db, f.page.ID)
	require.NoError(t, err)
	assert.Len(t, refs[f.comp.ID], 2)
}

func TestGroupLink_AusgefalleneQuelleZaehltNicht(t *testing.T) {
	f := newGroupFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.grp.ID)
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", f.src.ID).Update("outage_since", f.clock).Error)
	r := f.ref(t)
	assert.True(t, r.SourceOutage)
	assert.Equal(t, componentstatus.DegradedPerformance, r.Status, "zuletzt bekannter Status bleibt sichtbar")
	assert.Equal(t, componentstatus.Operational, f.effective(t), "zählt aber nicht")
}

func TestGroupLink_DBFehler(t *testing.T) {
	f := newGroupFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.grp.ID)
	failOn(t, f.db, "row", "external_components", 0)
	_, err := LinkedRefs(f.db, f.page.ID)
	require.Error(t, err)
}

func TestGroupWorst(t *testing.T) {
	f := newGroupFixture(t)
	status, worst, err := GroupWorst(f.db, f.src.ID, "g-db")
	require.NoError(t, err)
	assert.Equal(t, componentstatus.DegradedPerformance, status)
	assert.Equal(t, "PostgreSQL", worst.Name)

	status, worst, err = GroupWorst(f.db, f.src.ID, "unbekannt")
	require.NoError(t, err)
	assert.Equal(t, componentstatus.Unknown, status)
	assert.Nil(t, worst)

	failOn(t, f.db, "row", "external_components", 0)
	_, _, err = GroupWorst(f.db, f.src.ID, "g-db")
	require.Error(t, err)
}

func TestGroupStatus_UnbewertbareMitgliederUndRangfolge(t *testing.T) {
	status, worst := groupStatus(nil)
	assert.Equal(t, componentstatus.Unknown, status)
	assert.Nil(t, worst)
	status, worst = groupStatus([]groupMember{
		{Name: "a", Status: componentstatus.Unknown}, {Name: "b", Status: componentstatus.Group},
	})
	assert.Equal(t, componentstatus.Unknown, status)
	assert.Nil(t, worst)
	status, worst = groupStatus([]groupMember{
		{Name: "a", Status: componentstatus.Operational}, {Name: "b", Status: componentstatus.Unknown}, {Name: "c", Status: componentstatus.UnderMaintenance},
	})
	assert.Equal(t, componentstatus.UnderMaintenance, status)
	assert.Equal(t, "c", worst.Name)
	status, worst = groupStatus([]groupMember{{Name: "a", Status: componentstatus.Operational}})
	assert.Equal(t, componentstatus.Operational, status)
	assert.Equal(t, "a", worst.Name)
}

// Ein Abruf, der ein Mitglied verschlechtert, löst ein Ereignis an der
// gekoppelten eigenen Komponente aus.
func TestGroupLink_AbrufMeldetEreignis(t *testing.T) {
	f := newGroupFixture(t)
	f.link(t, f.page.ID, f.comp.ID, f.grp.ID)
	listener := &recordingListener{}
	provider := &fakeProvider{responses: []fakeResponse{okSummary("e1",
		Component{ExternalID: "g-db", Name: "Database", IsGroup: true, Status: componentstatus.Operational},
		Component{ExternalID: "mariadb", Name: "Service 04", GroupExternalID: "g-db", GroupName: "Database", Status: componentstatus.MajorOutage},
	)}}
	svc := NewService(f.db, Options{Providers: map[string]Provider{ProviderSummaryV2: provider}, Listener: listener, Now: func() time.Time { return f.clock }})
	var src models.ExternalSource
	require.NoError(t, f.db.First(&src, f.src.ID).Error)
	require.True(t, svc.FetchNow(context.Background(), src).OK)
	require.Len(t, listener.changes, 1)
	assert.Equal(t, componentstatus.DegradedPerformance, listener.changes[0].Previous)
	assert.Equal(t, componentstatus.MajorOutage, listener.changes[0].Current)
}
