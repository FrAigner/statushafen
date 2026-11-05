package handlers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Zwei Seiten weisen dieselbe Domain gleichzeitig nach: Die Vorabprüfung
// (customDomainLiveOnOtherPage) sieht beide Male keine live Seite, erst der
// partielle Unique-Index idx_status_pages_custom_domain_live entscheidet. Der
// Verlierer bekommt 409 domain_taken statt 500.

func verifyPath(p models.StatusPage) string {
	return "/api/admin/pages/" + strconv.Itoa(int(p.ID)) + "/custom-domain/verify"
}

// Deterministische Variante auf SQLite: Kurz vor dem eigenen UPDATE macht eine
// andere Anfrage die konkurrierende Seite live.
func TestVerifyCustomDomain_ParallelVerifizierteDomainGibt409(t *testing.T) {
	const domain = "status.race.example"
	stubLookup(t, func(context.Context, string) ([]string, error) { return []string{"statushafen-verify=tok-b"}, nil })
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	rival := models.StatusPage{Name: "A", Slug: "a", CustomDomain: domain, CustomDomainStatus: "pending", CustomDomainVerificationToken: "tok-a"}
	loser := models.StatusPage{Name: "B", Slug: "b", CustomDomain: domain, CustomDomainStatus: "pending", CustomDomainVerificationToken: "tok-b"}
	require.NoError(t, db.Create(&rival).Error)
	require.NoError(t, db.Create(&loser).Error)

	fired := false
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("cd_race", func(tx *gorm.DB) {
		if fired {
			return
		}
		fired = true
		require.NoError(t, db.Session(&gorm.Session{NewDB: true}).
			Exec("UPDATE status_pages SET custom_domain_status = 'verified' WHERE id = ?", rival.ID).Error)
	}))

	w := doJSONRequest(r, "POST", verifyPath(loser), nil)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"domain_taken"`)
	assertCustomDomainState(t, db, loser.ID, domain, "pending")
	assertCustomDomainState(t, db, rival.ID, domain, "verified")
	assert.NotContains(t, w.Body.String(), "tok-", "keine Tokens in der Antwort")
}

// Gegenprobe: ein anderer Datenbankfehler bleibt ein 500.
func TestVerifyCustomDomain_AndererDBFehlerBleibt500(t *testing.T) {
	stubLookup(t, func(context.Context, string) ([]string, error) { return []string{"statushafen-verify=tok"}, nil })
	r, db, page, path := customDomainCheckFixture(t, "pending")
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("cd_other", func(tx *gorm.DB) {
		_ = tx.AddError(errors.New("db down"))
	}))
	w := doJSONRequest(r, "POST", path, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"server_error"`)
	assertCustomDomainState(t, db, page.ID, "s.a.example", "pending")
}

// Echtes Postgres: gleichzeitige Nachweise, genau einer gewinnt, kein 500.
// Nur mit MIGRATION_PG_TEST=1 (lädt Postgres, startet nicht als root).
func TestVerifyCustomDomainPostgres_ParallelGenauEinGewinner(t *testing.T) {
	if os.Getenv("MIGRATION_PG_TEST") != "1" {
		t.Skip("nur mit MIGRATION_PG_TEST=1 (echtes Postgres)")
	}
	if os.Getenv("JWT_SECRET") == "" {
		t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := uint32(l.Addr().(*net.TCPAddr).Port)
	require.NoError(t, l.Close())
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).Port(port).Database("statushafen").
		Encoding("UTF8").Locale("C").RuntimePath(t.TempDir()).Logger(nil))
	require.NoError(t, pg.Start())
	t.Cleanup(func() { _ = pg.Stop() })
	dsn := fmt.Sprintf("host=localhost port=%d user=postgres password=postgres dbname=statushafen sslmode=disable", port)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.Role{}, &models.User{}, &models.UserStatusPageRole{},
		&models.UserSSOIdentity{}, &models.AuditLog{}, &models.SystemSetting{}))
	database.CreateSoftDeleteUniqueIndexes(db)
	old := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = old })
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)

	stubLookup(t, func(context.Context, string) ([]string, error) {
		return []string{"statushafen-verify=tok-a", "statushafen-verify=tok-b"}, nil
	})
	r := setupCustomDomainRouter(NewHandler(db))
	for round := 0; round < 25; round++ {
		domain := fmt.Sprintf("status-%d.race.example", round)
		pages := []models.StatusPage{
			{Name: "A", Slug: fmt.Sprintf("a-%d", round), CustomDomain: domain, CustomDomainStatus: "pending", CustomDomainVerificationToken: "tok-a"},
			{Name: "B", Slug: fmt.Sprintf("b-%d", round), CustomDomain: domain, CustomDomainStatus: "pending", CustomDomainVerificationToken: "tok-b"},
		}
		require.NoError(t, db.Create(&pages).Error)
		codes := make([]int, len(pages))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range pages {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				codes[i] = doJSONRequest(r, "POST", verifyPath(pages[i]), nil).Code
			}()
		}
		close(start)
		wg.Wait()
		ok := 0
		for _, c := range codes {
			require.Contains(t, []int{http.StatusOK, http.StatusConflict}, c, "Runde %d, Codes: %v", round, codes)
			if c == http.StatusOK {
				ok++
			}
		}
		require.Equal(t, 1, ok, "Runde %d, Codes: %v", round, codes)
		var live int64
		db.Model(&models.StatusPage{}).Where("custom_domain = ? AND custom_domain_status = 'verified'", domain).Count(&live)
		require.EqualValues(t, 1, live)
	}
}
