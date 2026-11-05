package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Das Verzeichnis GET /api/public/pages ist anonym lesbar. Es liefert höchstens
// 200 Seiten je Aufruf (limit/offset zum Blättern) und kürzt Name und
// Beschreibung, damit eine einzelne riesige Beschreibung die Antwort nicht auf
// Megabyte aufbläht.

type directoryPage struct {
	ID          uint
	Name        string
	Slug        string
	Description string
}

func directoryEnv(t *testing.T) (*gorm.DB, http.Handler) {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages", h.GetPublicPagesHandler)
	return db, r
}

func getDirectory(t *testing.T, r http.Handler, query string) (int, []directoryPage, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/public/pages"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var pages []directoryPage
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &pages))
	}
	return w.Code, pages, w.Body.String()
}

func seedDirectory(t *testing.T, db *gorm.DB, n int) {
	t.Helper()
	pages := make([]models.StatusPage, n)
	for i := range pages {
		pages[i] = models.StatusPage{Name: fmt.Sprintf("Seite %03d", i), Slug: fmt.Sprintf("seite-%03d", i), IsPublicIndexed: true}
	}
	require.NoError(t, db.CreateInBatches(&pages, 100).Error)
}

func TestPublicPagesDirectory_ReturnsAtMost200PagesByDefault(t *testing.T) {
	db, r := directoryEnv(t)
	seedDirectory(t, db, 230)

	code, pages, _ := getDirectory(t, r, "")
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, pages, 200)

	_, rest, _ := getDirectory(t, r, "?offset=200")
	assert.Len(t, rest, 30, "der Rest ist über offset erreichbar")
	assert.Equal(t, "seite-200", rest[0].Slug, "die Reihenfolge ist stabil (nach ID)")
}

func TestPublicPagesDirectory_LimitIsClampedTo200AndHonoured(t *testing.T) {
	db, r := directoryEnv(t)
	seedDirectory(t, db, 230)

	_, five, _ := getDirectory(t, r, "?limit=5")
	assert.Len(t, five, 5)
	_, clamped, _ := getDirectory(t, r, "?limit=1000")
	assert.Len(t, clamped, 200, "über 200 wird gekappt")
}

func TestPublicPagesDirectory_RejectsInvalidWindow(t *testing.T) {
	_, r := directoryEnv(t)
	for _, q := range []string{"?limit=0", "?limit=-1", "?limit=abc", "?offset=-1", "?offset=100001", "?offset=x"} {
		code, _, body := getDirectory(t, r, q)
		assert.Equal(t, http.StatusBadRequest, code, q)
		assert.Contains(t, body, `"code":"invalid_request"`, q)
	}
}

func TestPublicPagesDirectory_TruncatesNameAndDescription(t *testing.T) {
	db, r := directoryEnv(t)
	// „ä“ belegt zwei Byte: Eine Kürzung nach Byte würde ein Zeichen zerschneiden.
	longName := strings.Repeat("ä", 400)
	longDescription := strings.Repeat("ö", 600_000)
	require.NoError(t, db.Create(&models.StatusPage{Name: longName, Slug: "riesig", Description: longDescription, IsPublicIndexed: true}).Error)

	code, pages, body := getDirectory(t, r, "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, pages, 1)
	assert.LessOrEqual(t, utf8.RuneCountInString(pages[0].Name), 120)
	assert.LessOrEqual(t, utf8.RuneCountInString(pages[0].Description), 500)
	assert.True(t, strings.HasSuffix(pages[0].Name, "…"), "Kürzung ist erkennbar")
	assert.True(t, strings.HasSuffix(pages[0].Description, "…"))
	assert.True(t, utf8.ValidString(pages[0].Name) && utf8.ValidString(pages[0].Description))
	assert.Less(t, len(body), 4096, "die Antwort bleibt klein")
}

// Gegenprobe: Normale Werte bleiben unverändert, auch genau an der Grenze.
func TestPublicPagesDirectory_KeepsNormalAndBoundaryValues(t *testing.T) {
	db, r := directoryEnv(t)
	atLimitName := strings.Repeat("n", 120)
	atLimitDescription := strings.Repeat("ü", 500)
	require.NoError(t, db.Create(&models.StatusPage{Name: "Kurz", Slug: "kurz", Description: "Ein Satz.", IsPublicIndexed: true}).Error)
	require.NoError(t, db.Create(&models.StatusPage{Name: atLimitName, Slug: "grenze", Description: atLimitDescription, IsPublicIndexed: true}).Error)

	_, pages, _ := getDirectory(t, r, "")
	require.Len(t, pages, 2)
	assert.Equal(t, "Kurz", pages[0].Name)
	assert.Equal(t, "Ein Satz.", pages[0].Description)
	assert.Equal(t, atLimitName, pages[1].Name)
	assert.Equal(t, atLimitDescription, pages[1].Description)
}

// capturedQueries sammelt den SQL-Text der Lese-Abfragen, solange der Test läuft.
func capturedQueries(t *testing.T, db *gorm.DB) func() []string {
	t.Helper()
	var mu sync.Mutex
	var sqls []string
	name := "test:capture_sql:" + t.Name()
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		mu.Lock()
		defer mu.Unlock()
		sqls = append(sqls, tx.Statement.SQL.String())
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sqls...)
	}
}

// Die Begrenzung geschieht in der Datenbank, nicht erst danach: feste
// Reihenfolge nach ID (sonst blättert offset in Postgres unvorhersehbar),
// LIMIT/OFFSET und die Kürzung von Name und Beschreibung schon in der Abfrage,
// damit eine riesige Beschreibung nicht erst geladen wird.
func TestPublicPagesDirectory_QueryOrdersLimitsAndTruncatesInTheDatabase(t *testing.T) {
	db, r := directoryEnv(t)
	seedDirectory(t, db, 3)
	queries := capturedQueries(t, db)

	code, _, _ := getDirectory(t, r, "?limit=2&offset=1")
	require.Equal(t, http.StatusOK, code)

	var directory string
	for _, q := range queries() {
		if strings.Contains(q, "is_public_indexed") {
			directory = q
		}
	}
	require.NotEmpty(t, directory, "Abfrage des Verzeichnisses nicht gefunden")
	assert.Contains(t, directory, "ORDER BY id asc")
	assert.Contains(t, directory, "LIMIT")
	assert.Contains(t, directory, "OFFSET")
	assert.Contains(t, directory, "substr(name, 1,")
	assert.Contains(t, directory, "substr(description, 1,")
	assert.Contains(t, directory, "is_private", "private Seiten sind nie im Verzeichnis")
}
