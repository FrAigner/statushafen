package handlers_test

// Rollenvergabe (PUT /api/admin/users/:id mit page_roles und
// POST /api/admin/pages/:page_id/users durch einen Global Admin) gleichzeitig
// mit der Kontolöschung auf einem ECHTEN Postgres (READ COMMITTED). Früher
// wartete der Schreiber auf die Benutzersperre der Löschung, aktualisierte
// danach 0 Zeilen, legte die Rolle aber trotzdem an und antwortete 200: eine
// Rollenzeile am gelöschten Konto. SQLite serialisiert Schreiber und zeigt das
// nicht. Wie page_invitation_postgres_test.go nur mit MIGRATION_PG_TEST=1 und
// nicht als root.

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func updSetup(t *testing.T) (invFixture, *gorm.DB) {
	t.Helper()
	db := startInvitationPostgres(t)
	if err := db.AutoMigrate(&models.IncidentUpdate{}, &models.ActionItem{}, &models.PageOrder{}, &models.Subscriber{},
		&models.Tag{}, &models.UserTag{}, &models.OIDCLoginState{}, &models.SAMLLoginState{}, &models.NotificationRead{}); err != nil {
		t.Fatal(err)
	}
	f := newInvFixtureOn(t, db)
	h := handlers.NewHandler(db)
	auth := f.r.Group("/api", middleware.AuthMiddleware())
	auth.DELETE("/admin/users/:id", h.DeleteUserHandler)
	auth.PUT("/admin/users/:id", h.UpdateUserHandler)
	auth.DELETE("/me", h.DeleteMeHandler)
	warmUpPool(t, db, 8)
	return f, db
}

type updWrite struct {
	name   string
	method string
	path   func(f invFixture, u models.User) string
	body   func(f invFixture, u models.User) any
	// member: Konto ist vorher schon Mitglied der Seite (Rollenwechsel statt Anlegen).
	member bool
}

func updWrites() []updWrite {
	return []updWrite{
		{name: "PUT page_roles", method: http.MethodPut,
			path: func(_ invFixture, u models.User) string { return "/api/admin/users/" + strconv.Itoa(int(u.ID)) },
			body: func(f invFixture, u models.User) any {
				return map[string]any{"email": u.Email, "page_roles": []map[string]any{{"status_page_id": f.page.ID, "role_id": f.viewer.ID}}}
			}},
		{name: "POST Seitenmitglied neu", method: http.MethodPost,
			path: func(f invFixture, _ models.User) string { return f.usersPath() },
			body: func(f invFixture, u models.User) any {
				return map[string]any{"user_id": u.ID, "role_id": f.viewer.ID}
			}},
		{name: "POST Seitenmitglied Rollenwechsel", method: http.MethodPost, member: true,
			path: func(f invFixture, _ models.User) string { return f.usersPath() },
			body: func(f invFixture, u models.User) any {
				return map[string]any{"user_id": u.ID, "role_id": f.strong.ID}
			}},
	}
}

func updRoles(db *gorm.DB, uid uint) int64 {
	var n int64
	db.Unscoped().Model(&models.UserStatusPageRole{}).Where("user_id = ?", uid).Count(&n)
	return n
}

type updResult struct {
	code int
	body string
}

func updAsync(t *testing.T, f invFixture, method, path, tok string, body any) chan updResult {
	ch := make(chan updResult, 1)
	go func() {
		w := idorRequest(t, f.r, method, path, tok, body)
		ch <- updResult{w.Code, w.Body.String()}
	}()
	return ch
}

func updWait(t *testing.T, name string, ch chan updResult) updResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(20 * time.Second):
		t.Fatalf("%s hängt", name)
	}
	return updResult{}
}

// Deterministisch verschränkt: Die "Löschung" (dieselben Schritte wie
// lockAccountForDeletion + DeleteUserHandler) hält Benutzerzeile und Rollen,
// der Schreiber läuft in sie hinein und wird erst nach dem Commit frei.
// Erwartung: 404, keine Rollenzeile des gelöschten Kontos.
func TestUserWritePostgres_VsDeleteHeldLock(t *testing.T) {
	for _, w := range updWrites() {
		t.Run(w.name, func(t *testing.T) {
			f, db := updSetup(t)
			u, _ := f.account(t, "held@x.example")
			if w.member {
				if err := db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: f.page.ID, RoleID: f.viewer.ID}).Error; err != nil {
					t.Fatal(err)
				}
			}
			del := db.Begin()
			if err := del.Unscoped().Where("invited_user_id = ?", u.ID).Delete(&models.PageInvitation{}).Error; err != nil {
				t.Fatal(err)
			}
			var x models.User
			if err := del.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, u.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := del.Unscoped().Where("user_id = ?", u.ID).Delete(&models.UserStatusPageRole{}).Error; err != nil {
				t.Fatal(err)
			}
			if err := del.Delete(&models.User{}, u.ID).Error; err != nil {
				t.Fatal(err)
			}
			res := updAsync(t, f, w.method, w.path(f, u), f.gaTok, w.body(f, u))
			time.Sleep(500 * time.Millisecond)
			if err := del.Commit().Error; err != nil {
				t.Fatal(err)
			}
			r := updWait(t, w.name, res)
			if r.code != http.StatusNotFound || !strings.Contains(r.body, "user_not_found") {
				t.Errorf("erwartet 404 user_not_found, war %d %s", r.code, r.body)
			}
			if n := updRoles(db, u.ID); n != 0 {
				t.Errorf("%d Rollenzeile(n) am gelöschten Konto", n)
			}
		})
	}
}

// Echte Handler gegeneinander, viele Läufe mit versetztem Start: danach nie
// eine Rollenzeile des gelöschten Kontos, nie 5xx (Deadlock 40P01) und kein
// Hänger.
func TestUserWritePostgres_VsDeleteRounds(t *testing.T) {
	dels := []struct {
		name string
		path func(u models.User) string
		tok  func(f invFixture, utok string) string
		body func(u models.User) any
	}{
		{"DeleteUserHandler",
			func(u models.User) string { return "/api/admin/users/" + strconv.Itoa(int(u.ID)) },
			func(f invFixture, _ string) string { return f.gaTok },
			func(models.User) any { return nil }},
		{"DeleteMeHandler",
			func(models.User) string { return "/api/me" },
			func(_ invFixture, utok string) string { return utok },
			func(u models.User) any { return map[string]any{"confirm_email": u.Email} }},
	}
	for _, w := range updWrites() {
		for _, d := range dels {
			t.Run(w.name+"/"+d.name, func(t *testing.T) {
				f, db := updSetup(t)
				const rounds = 40
				for i := 0; i < rounds; i++ {
					u, utok := f.account(t, fmt.Sprintf("r%d@x.example", i))
					if w.member {
						if err := db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: f.page.ID, RoleID: f.viewer.ID}).Error; err != nil {
							t.Fatal(err)
						}
					}
					var wg sync.WaitGroup
					start := make(chan struct{})
					var wCode, dCode int
					var dBody string
					wg.Add(2)
					go func() {
						defer wg.Done()
						<-start
						wCode = idorRequest(t, f.r, w.method, w.path(f, u), f.gaTok, w.body(f, u)).Code
					}()
					go func() {
						defer wg.Done()
						<-start
						time.Sleep(time.Duration(i%8) * 700 * time.Microsecond)
						r := idorRequest(t, f.r, http.MethodDelete, d.path(u), d.tok(f, utok), d.body(u))
						dCode, dBody = r.Code, r.Body.String()
					}()
					close(start)
					done := make(chan struct{})
					go func() { wg.Wait(); close(done) }()
					select {
					case <-done:
					case <-time.After(30 * time.Second):
						t.Fatalf("Runde %d: hängt", i)
					}
					if dCode != http.StatusOK {
						t.Fatalf("Runde %d: Löschen antwortet %d: %s", i, dCode, dBody)
					}
					if wCode >= 500 {
						t.Fatalf("Runde %d: Schreiber antwortet %d (Deadlock/Abbruch)", i, wCode)
					}
					if n := updRoles(db, u.ID); n != 0 {
						t.Fatalf("Runde %d: %d Rollenzeile(n) des gelöschten Kontos (Schreiber: %d)", i, n, wCode)
					}
				}
			})
		}
	}
}
