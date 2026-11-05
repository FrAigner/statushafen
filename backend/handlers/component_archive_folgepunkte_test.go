package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Folgepunkte zum Archivieren von Komponenten.

// foreignTagUser legt einen Nutzer mit Tag DE an und hängt comp den fremden Tag US an.
func foreignTagUser(t *testing.T, e *visEnv, email string, comp models.Component) models.User {
	t.Helper()
	tagUS := models.Tag{Name: "US-" + email, StatusPageID: &e.page.ID}
	tagDE := models.Tag{Name: "DE-" + email, StatusPageID: &e.page.ID}
	require.NoError(t, e.db.Create(&tagUS).Error)
	require.NoError(t, e.db.Create(&tagDE).Error)
	require.NoError(t, e.db.Create(&models.ComponentTag{ComponentID: comp.ID, TagID: tagUS.ID}).Error)
	user := models.User{Email: email}
	require.NoError(t, e.db.Create(&user).Error)
	require.NoError(t, e.db.Create(&models.UserTag{UserID: user.ID, TagID: tagDE.ID}).Error)
	return user
}

// Punkt 2: Die Tag-Prüfung der Kaskade betrifft nur Komponenten, die sich wirklich ändern.
func TestComponentArchiveFolgepunkte_RestorePrueftNurTatsaechlichWiederkehrende(t *testing.T) {
	e := newVisEnv(t)
	group := e.comp("Gruppe", nil, true, "group")
	child := e.comp("Kind", &group.ID, true, "operational")
	early := e.comp("Früher einzeln archiviert", &group.ID, true, "operational")
	user := foreignTagUser(t, e, "restore-783@example.com", early)

	require.NoError(t, e.db.Model(&models.Component{}).Where("id = ?", early.ID).Update("archived_at", time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)).Error)
	e.archiveRaw(group.ID, child.ID)

	w, _ := e.doAs(user.ID, "POST", e.compURL(group, "/restore"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.Nil(t, e.reload(group).ArchivedAt)
	assert.Nil(t, e.reload(child).ArchivedAt)
	assert.NotNil(t, e.reload(early).ArchivedAt, "früher einzeln archiviertes Kind bleibt archiviert")
}

func TestComponentArchiveFolgepunkte_ArchivePrueftNurNochAktive(t *testing.T) {
	e := newVisEnv(t)
	group := e.comp("Gruppe", nil, true, "group")
	child := e.comp("Kind", &group.ID, true, "operational")
	early := e.comp("Schon archiviert", &group.ID, true, "operational")
	user := foreignTagUser(t, e, "archive-783@example.com", early)
	e.archiveRaw(early.ID)
	before := e.reload(early).ArchivedAt

	w, _ := e.doAs(user.ID, "POST", e.compURL(group, "/archive"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.NotNil(t, e.reload(group).ArchivedAt)
	assert.NotNil(t, e.reload(child).ArchivedAt)
	assert.True(t, before.Equal(*e.reload(early).ArchivedAt), "Zeitpunkt des bereits archivierten Kindes bleibt")
}

// Punkt 3a: Lesen, Prüfen und Schreiben laufen in EINER Transaktion.
func TestComponentArchiveFolgepunkte_ArchivierenLaeuftInEinerTransaktion(t *testing.T) {
	inTx := func(d *gorm.DB) bool {
		_, ok := d.Statement.ConnPool.(gorm.TxCommitter)
		return ok
	}
	run := func(t *testing.T, e *visEnv, method string, comp models.Component) (reads, writes, txReads, txWrites int) {
		require.NoError(t, e.db.Callback().Query().Before("gorm:query").Register("test:tx-read", func(d *gorm.DB) {
			if d.Statement.Table == "components" {
				reads++
				if inTx(d) {
					txReads++
				}
			}
		}))
		require.NoError(t, e.db.Callback().Update().Before("gorm:update").Register("test:tx-write", func(d *gorm.DB) {
			if d.Statement.Table == "components" {
				writes++
				if inTx(d) {
					txWrites++
				}
			}
		}))
		w, _ := e.do("POST", e.compURL(comp, method), nil)
		requireStatusCode(t, w, http.StatusOK)
		return
	}
	t.Run("archive", func(t *testing.T) {
		e := newVisEnv(t)
		comp := e.comp("A", nil, true, "operational")
		reads, writes, txReads, txWrites := run(t, e, "/archive", comp)
		assert.Equal(t, 1, writes)
		assert.Equal(t, writes, txWrites, "Schreiben in der Transaktion")
		assert.GreaterOrEqual(t, txReads, 1, "Baum wird in der Transaktion gelesen")
		assert.LessOrEqual(t, reads-txReads, 1, "außerhalb nur das Laden der Wurzel")
	})
	t.Run("restore", func(t *testing.T) {
		e := newVisEnv(t)
		comp := e.comp("A", nil, true, "operational")
		e.archiveRaw(comp.ID)
		reads, writes, txReads, txWrites := run(t, e, "/restore", comp)
		assert.Equal(t, 1, writes)
		assert.Equal(t, writes, txWrites)
		assert.GreaterOrEqual(t, txReads, 1)
		assert.LessOrEqual(t, reads-txReads, 1)
	})
}

// Punkt 3b: wird die Komponente zwischen Laden und Umhängen archiviert, gilt
// der frische Stand: kein Herauslösen auf die oberste Ebene.
func TestComponentArchiveFolgepunkte_UmhaengenNutztFrischenArchivStand(t *testing.T) {
	e := newVisEnv(t)
	group := e.comp("Gruppe", nil, true, "group")
	member := e.comp("Mitglied", &group.ID, true, "operational")
	count := 0
	require.NoError(t, e.db.Callback().Query().After("gorm:query").Register("test:archive-after-load", func(d *gorm.DB) {
		if d.Statement.Table == "components" {
			count++
			if count == 1 { // das Laden der Komponente im Update-Handler
				require.NoError(t, d.Session(&gorm.Session{NewDB: true}).Exec("UPDATE components SET archived_at = ? WHERE id IN (?, ?)", time.Now(), group.ID, member.ID).Error)
			}
		}
	}))

	w, m := e.do("PUT", e.compURL(member, ""), map[string]any{"status": "operational", "parent_id": 0})
	requireStatusCode(t, w, http.StatusConflict)
	assert.Equal(t, "component_archived", m["code"])
	got := e.reload(member)
	require.NotNil(t, got.ParentID)
	assert.Equal(t, group.ID, *got.ParentID)
	assert.NotNil(t, got.ArchivedAt)
}

// Punkt 1/5: Die Baum-Lesung sperrt mit FOR NO KEY UPDATE (kollidiert nicht mit
// FK-FOR-KEY-SHARE parallel angelegter Vorfälle) und in fester Reihenfolge.
// SQLite lässt die Klausel weg, deshalb wird das SQL für Postgres im DryRun geprüft.
func TestComponentArchiveFolgepunkte_BaumLesungSperrtOhneKeyShareKonflikt(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=localhost dbname=dryrun sslmode=disable"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)
	var sql string
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:capture-sql", func(d *gorm.DB) {
		sql = d.Statement.SQL.String()
	}))
	_, err = handlers.LockedPageComponentsForTest(db, 1)
	require.NoError(t, err)
	assert.Contains(t, sql, "FOR NO KEY UPDATE")
	assert.NotContains(t, sql, "FOR UPDATE")
	assert.Contains(t, sql, "ORDER BY")
}

// Punkt 2: Die Zahl der Abfragen hängt nicht von der Baumgröße ab (die Sperre
// läuft sonst während hunderter Roundtrips).
func TestComponentArchiveFolgepunkte_AbfragenUnabhaengigVonBaumgroesse(t *testing.T) {
	queries := func(n int) int {
		e := newVisEnv(t)
		group := e.comp("Gruppe", nil, true, "group")
		for i := 0; i < n; i++ {
			e.comp("Kind", &group.ID, true, "operational")
		}
		tag := models.Tag{Name: "T", StatusPageID: &e.page.ID}
		require.NoError(t, e.db.Create(&tag).Error)
		user := models.User{Email: "viele@example.com"}
		require.NoError(t, e.db.Create(&user).Error)
		require.NoError(t, e.db.Create(&models.UserTag{UserID: user.ID, TagID: tag.ID}).Error)
		count := 0
		require.NoError(t, e.db.Callback().Query().Before("gorm:query").Register("test:count", func(*gorm.DB) { count++ }))
		w, _ := e.doAs(user.ID, "POST", e.compURL(group, "/archive"), nil)
		requireStatusCode(t, w, http.StatusOK)
		return count
	}
	assert.Equal(t, queries(2), queries(25))
}

// Punkt 3: Die Wurzel wird in der Transaktion frisch geprüft.
func TestComponentArchiveFolgepunkte_ParallelesDoppeltesArchivierenSchreibtNichtNochmal(t *testing.T) {
	e := newVisEnv(t)
	comp := e.comp("A", nil, true, "operational")
	first := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	count := 0
	require.NoError(t, e.db.Callback().Query().After("gorm:query").Register("test:parallel-archive", func(d *gorm.DB) {
		if d.Statement.Table == "components" {
			count++
			if count == 1 {
				require.NoError(t, d.Session(&gorm.Session{NewDB: true}).Exec("UPDATE components SET archived_at = ? WHERE id = ?", first, comp.ID).Error)
			}
		}
	}))
	w, _ := e.do("POST", e.compURL(comp, "/archive"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.Zero(t, e.auditCount("component.archived"), "kein zweiter Audit-Eintrag")
	require.NotNil(t, e.reload(comp).ArchivedAt)
	assert.True(t, first.Equal(*e.reload(comp).ArchivedAt))
}

func TestComponentArchiveFolgepunkte_RestoreMitGeaendertemZeitpunktBleibtKonsistent(t *testing.T) {
	e := newVisEnv(t)
	group := e.comp("Gruppe", nil, true, "group")
	child := e.comp("Kind", &group.ID, true, "operational")
	e.archiveRaw(group.ID, child.ID)
	second := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	count := 0
	require.NoError(t, e.db.Callback().Query().After("gorm:query").Register("test:rearchive", func(d *gorm.DB) {
		if d.Statement.Table == "components" {
			count++
			if count == 1 { // zwischen Laden und Transaktion: wiederhergestellt und neu archiviert
				require.NoError(t, d.Session(&gorm.Session{NewDB: true}).Exec("UPDATE components SET archived_at = ? WHERE id IN (?, ?)", second, group.ID, child.ID).Error)
			}
		}
	}))
	w, _ := e.do("POST", e.compURL(group, "/restore"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.Nil(t, e.reload(group).ArchivedAt)
	assert.Nil(t, e.reload(child).ArchivedAt, "Mitglied kommt mit zurück")
}

// Punkt 4: Umhängen (Parent-Wechsel) prüft und speichert in einer Transaktion mit Sperre.
func TestComponentArchiveFolgepunkte_UmhaengenLaeuftInEinerTransaktion(t *testing.T) {
	e := newVisEnv(t)
	group := e.comp("Gruppe", nil, true, "group")
	comp := e.comp("A", nil, true, "operational")
	var reads, txReads, txWrites int
	require.NoError(t, e.db.Callback().Query().Before("gorm:query").Register("test:tx-read", func(d *gorm.DB) {
		if d.Statement.Table == "components" {
			reads++
			if _, ok := d.Statement.ConnPool.(gorm.TxCommitter); ok {
				txReads++
			}
		}
	}))
	require.NoError(t, e.db.Callback().Update().Before("gorm:update").Register("test:tx-write", func(d *gorm.DB) {
		if _, ok := d.Statement.ConnPool.(gorm.TxCommitter); ok && d.Statement.Table == "components" {
			txWrites++
		}
	}))
	w, _ := e.do("PUT", e.compURL(comp, ""), map[string]any{"status": "operational", "parent_id": group.ID})
	requireStatusCode(t, w, http.StatusOK)
	assert.GreaterOrEqual(t, txWrites, 1, "Speichern in der Transaktion")
	assert.GreaterOrEqual(t, txReads, 2, "Sperre/Prüfung in der Transaktion")
	assert.LessOrEqual(t, reads-txReads, 1, "außerhalb nur das Laden der Komponente")
}
