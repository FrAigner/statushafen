package handlers_test

// Security-Review Fund F3: Gleichzeitige Einladungen auf
// einem ECHTEN Postgres (READ COMMITTED). SQLite serialisiert Schreiber und
// zeigt das Problem nicht. Wie database/migration_postgres_test.go nur mit
// MIGRATION_PG_TEST=1 (lädt die Binaries, startet nicht als root).

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func startInvitationPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("MIGRATION_PG_TEST") != "1" {
		t.Skip("nur mit MIGRATION_PG_TEST=1 (echtes Postgres)")
	}
	if os.Getenv("JWT_SECRET") == "" {
		t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production") // wie testutils.SetupTestDB
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint32(l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).Port(port).Database("statushafen").
		Encoding("UTF8").Locale("C").RuntimePath(t.TempDir()).Logger(nil))
	if err := pg.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Stop() })
	dsn := fmt.Sprintf("host=localhost port=%d user=postgres password=postgres dbname=statushafen sslmode=disable", port)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StatusPage{}, &models.Role{}, &models.User{}, &models.UserStatusPageRole{},
		&models.UserSSOIdentity{}, &models.AuditLog{}, &models.PageInvitation{}, &models.Language{}, &models.StatusPageLanguage{}); err != nil {
		t.Fatal(err)
	}
	database.CreateUserStatusPageRoleUniqueIndex(db)
	database.CreatePageInvitationUniqueIndex(db)
	old := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = old })
	return db
}

// warmUpPool öffnet n Verbindungen im Voraus und hält sie offen. Der Aufbau
// einer Postgres-Verbindung dauert Millisekunden; ohne vorgewärmten Pool laufen
// die "gleichzeitigen" Aufrufe in Wirklichkeit nacheinander und der Test
// erkennt das fehlende Sperren nicht (Gegenprobe ohne Sperre bleibt sonst grün).
func warmUpPool(t *testing.T, db *gorm.DB, n int) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxIdleConns(n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db.Exec("SELECT pg_sleep(0.2)")
		}()
	}
	wg.Wait()
}

// postParallel schickt n Einladungen gleichzeitig (Start per Barriere) und
// liefert die Statuscodes.
func postParallel(t *testing.T, f invFixture, emails []string) []int {
	t.Helper()
	codes := make([]int, len(emails))
	warmUpPool(t, f.db, len(emails)+2)
	var ready, wg sync.WaitGroup
	start := make(chan struct{})
	for i, email := range emails {
		ready.Add(1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": email, "role_id": f.viewer.ID})
			codes[i] = w.Code
		}()
	}
	ready.Wait()
	close(start)
	wg.Wait()
	return codes
}

// Gleiche Adresse, viele gleichzeitige Aufrufe: genau eine Zeile, genau eine
// Mail, genau ein Audit-Eintrag, alle antworten neutral 200.
func TestPageInvitationPostgres_ParallelSameAddressOneMail(t *testing.T) {
	db := startInvitationPostgres(t)
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixtureOn(t, db)
	f.account(t, "parallel@x.example")

	emails := make([]string, 24)
	for i := range emails {
		emails[i] = "parallel@x.example"
	}
	for i, code := range postParallel(t, f, emails) {
		if code != http.StatusOK {
			t.Errorf("Aufruf %d: Status %d", i, code)
		}
	}
	awaitSMTPMessage(t, srv)
	expectNoSMTPMessage(t, srv)
	var rows int64
	db.Unscoped().Model(&models.PageInvitation{}).Where("email = ?", "parallel@x.example").Count(&rows)
	if rows != 1 {
		t.Errorf("erwartet eine Zeile, erhalten %d", rows)
	}
	if n := auditCountFor(db, "statuspage.invitation_created"); n != 1 {
		t.Errorf("erwartet ein Audit, erhalten %d", n)
	}
}

// Tagesgrenze unter Last: Stehen schon 199 Einladungen im Audit, darf von
// mehreren gleichzeitigen Aufrufen für verschiedene Adressen nur einer
// durchkommen (Seitenzeile gesperrt, Audit in derselben Transaktion).
func TestPageInvitationPostgres_ParallelDailyLimit(t *testing.T) {
	db := startInvitationPostgres(t)
	f := newInvFixtureOn(t, db)
	rows := make([]models.AuditLog, 199)
	for i := range rows {
		rows[i] = models.AuditLog{Action: "statuspage.invitation_created", Category: "config", TargetType: "StatusPage", TargetID: fmt.Sprint(f.page.ID)}
	}
	if err := db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	// Die Zeitstempel (Unix-Sekunden) liegen im 24-Stunden-Fenster.
	db.Model(&models.AuditLog{}).Where("action = ?", "statuspage.invitation_created").Update("created_at", time.Now().Unix())

	emails := make([]string, 24)
	for i := range emails {
		emails[i] = fmt.Sprintf("a%d@x.example", i)
	}
	ok, limited := 0, 0
	for _, code := range postParallel(t, f, emails) {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Errorf("unerwarteter Status %d", code)
		}
	}
	if ok != 1 || limited != len(emails)-1 {
		t.Errorf("Tagesgrenze umgangen: %d angenommen, %d abgewiesen", ok, limited)
	}
	if n := auditCountFor(db, "statuspage.invitation_created"); n != 200 {
		t.Errorf("erwartet 200 Audit-Einträge, erhalten %d", n)
	}
}

// Re-Review W1: Annehmen gegen gleichzeitiges Neu-Einladen derselben Adresse.
// Das Annehmen hält die Sperre auf der Einladungszeile und braucht beim INSERT
// in user_status_page_roles FOR KEY SHARE auf die Seitenzeile (Fremdschlüssel).
// Sperrt das Einladen die Seite mit FOR UPDATE und wartet dann auf die
// Einladungszeile, entsteht ein Deadlock (40P01). FOR NO KEY UPDATE kollidiert
// nicht mit KEY SHARE; das Einladen wartet, bis das Annehmen fertig ist.
func TestPageInvitationPostgres_AcceptVsReinviteNoDeadlock(t *testing.T) {
	db := startInvitationPostgres(t)
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	f := newInvFixtureOn(t, db)
	u, _ := f.account(t, "w1@x.example")
	f.invite(t, srv, u.Email, f.viewer)
	backdateInvitations(db, 31*time.Minute)
	var inv models.PageInvitation
	if err := db.Where("email = ?", u.Email).First(&inv).Error; err != nil {
		t.Fatal(err)
	}

	// (1) Annehmen: löscht die Einladungszeile und hält deren Sperre.
	accept := db.Begin()
	if err := accept.Unscoped().Where("id = ? AND deleted_at IS NULL", inv.ID).Delete(&models.PageInvitation{}).Error; err != nil {
		t.Fatal(err)
	}
	// (2) Einladen: sperrt die Seite, will die Zeilen der Adresse löschen und wartet auf (1).
	done := make(chan int, 1)
	go func() {
		w := idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": u.Email, "role_id": f.viewer.ID})
		done <- w.Code
	}()
	time.Sleep(700 * time.Millisecond)
	// (3) Annehmen legt die Mitgliedschaft an (KEY SHARE auf die Seitenzeile).
	if insErr := accept.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: f.page.ID, RoleID: f.viewer.ID}).Error; insErr != nil {
		_ = accept.Rollback()
		t.Errorf("Annehmen: %v", insErr)
	} else if err := accept.Commit().Error; err != nil {
		t.Errorf("Annehmen-Commit: %v", err)
	}
	select {
	case code := <-done:
		// Nach dem Annehmen ist die Adresse Mitglied: neutrale 200 (Rolle bleibt).
		if code != http.StatusOK {
			t.Errorf("Einladen: Status %d", code)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Einladen hängt")
	}
}

// Review-Fund 1: Zwei Einladungen (Seite A und B) an dasselbe Konto,
// gleichzeitig angenommen. Ohne die Benutzersperre VOR der Ein-Seiten-Prüfung
// sehen beide Transaktionen "keine Rolle" (FOR UPDATE auf Rollenzeilen eines
// Kontos ohne Rolle sperrt nichts) und das Konto wird Mitglied beider Seiten.
func TestPageInvitationPostgres_ParallelAcceptTwoPages(t *testing.T) {
	db := startInvitationPostgres(t)
	f := newInvFixtureOn(t, db)
	var ga models.User
	db.Where("email = ?", "ga@example.com").First(&ga)
	warmUpPool(t, db, 6)
	both := 0
	codes := map[int]int{}
	for i := 0; i < 30; i++ {
		u := models.User{Email: fmt.Sprintf("two%d@x.example", i)}
		db.Create(&u)
		tok, _ := middleware.GenerateToken(u.ID, u.Email, "", "", false)
		exp := time.Now().Add(time.Hour)
		for j, p := range []models.StatusPage{f.page, f.other} {
			db.Create(&models.PageInvitation{StatusPageID: p.ID, Email: u.Email, RoleID: f.viewer.ID, TokenHash: hashToken(fmt.Sprintf("t%d-%d", i, j)), ExpiresAt: exp, InvitedByID: ga.ID, InvitedUserID: u.ID})
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		res := make([]int, 2)
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				res[j] = idorRequest(t, f.r, "POST", "/api/invitations/accept", tok, map[string]any{"token": fmt.Sprintf("t%d-%d", i, j)}).Code
			}()
		}
		close(start)
		wg.Wait()
		codes[res[0]]++
		codes[res[1]]++
		var pages int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", u.ID).Distinct("status_page_id").Count(&pages)
		if pages > 1 {
			both++
		}
	}
	t.Logf("Statuscodes: %v", codes)
	if both > 0 {
		t.Errorf("Ein-Seiten-Grenze verletzt: %d von 30 Konten auf zwei Seiten", both)
	}
}

// Review-Fund 2: Lock-Reihenfolge des Annehmens (Einladung löschen,
// Rollen und Benutzer FOR UPDATE in BEIDEN Reihenfolgen) gegen gleichzeitiges
// Konto-Löschen (Ablauf von DeleteUserHandler) und Neu-Einladen: kein Deadlock.
func TestPageInvitationPostgres_AcceptLockOrderVsDeleteAndReinvite(t *testing.T) {
	for _, userFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("userFirst=%v", userFirst), func(t *testing.T) {
			db := startInvitationPostgres(t)
			srv := startFakeSMTPServerWithEnv(t)
			defer srv.close()
			f := newInvFixtureOn(t, db)
			u, _ := f.account(t, "lo@x.example")
			f.invite(t, srv, u.Email, f.viewer)
			backdateInvitations(db, 31*time.Minute)
			var inv models.PageInvitation
			if err := db.Where("email = ?", u.Email).First(&inv).Error; err != nil {
				t.Fatal(err)
			}
			acc := db.Begin()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(acc.Unscoped().Where("id = ? AND deleted_at IS NULL", inv.ID).Delete(&models.PageInvitation{}).Error)
			lockUser := func() {
				var x models.User
				must(acc.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, u.ID).Error)
			}
			lockRoles := func() {
				var r []models.UserStatusPageRole
				must(acc.Where("user_id = ?", u.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&r).Error)
			}
			if userFirst {
				lockUser()
				lockRoles()
			} else {
				lockRoles()
				lockUser()
			}
			delDone := make(chan error, 1)
			go func() {
				tx := db.Begin()
				// Reihenfolge wie lockAccountForDeletion (Einladungen,
				// Benutzer FOR UPDATE), danach erst die Rollen.
				err := tx.Unscoped().Where("email = ? OR invited_user_id = ? OR invited_by_id = ?", u.Email, u.ID, u.ID).Delete(&models.PageInvitation{}).Error
				if err == nil {
					var x models.User
					err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, u.ID).Error
				}
				if err == nil {
					err = tx.Unscoped().Where("user_id = ?", u.ID).Delete(&models.UserStatusPageRole{}).Error
				}
				if err == nil {
					err = tx.Delete(&models.User{}, u.ID).Error
				}
				if err == nil {
					err = tx.Commit().Error
				} else {
					tx.Rollback()
				}
				delDone <- err
			}()
			reDone := make(chan int, 1)
			go func() {
				reDone <- idorRequest(t, f.r, "POST", f.usersPath(), f.ownerTok, map[string]any{"email": u.Email, "role_id": f.viewer.ID}).Code
			}()
			time.Sleep(800 * time.Millisecond)
			if err := acc.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: f.page.ID, RoleID: f.viewer.ID}).Error; err != nil {
				acc.Rollback()
				t.Errorf("Annehmen: %v", err)
			} else if err := acc.Commit().Error; err != nil {
				t.Errorf("Commit: %v", err)
			}
			select {
			case err := <-delDone:
				if err != nil {
					t.Errorf("Löschen: %v", err)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("Löschen hängt")
			}
			select {
			case c := <-reDone:
				if c != http.StatusOK {
					t.Errorf("Neu-Einladen: Status %d", c)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("Einladen hängt")
			}
		})
	}
}
