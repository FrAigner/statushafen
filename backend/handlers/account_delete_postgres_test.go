package handlers_test

// Konto-Löschung (DeleteUserHandler, DeleteMeHandler) gegen
// gleichzeitiges Annehmen einer Einladung auf einem ECHTEN Postgres (READ
// COMMITTED). SQLite serialisiert Schreiber und zeigt die Race nicht. Wie
// page_invitation_postgres_test.go nur mit MIGRATION_PG_TEST=1 und nicht als root.

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
)

// Das Löschen entfernte früher die Rollen, BEVOR die Annahme committete: die
// Annahme legte danach ihre Rollenzeile an und das (weich gelöschte) Konto
// behielt sie. Erwartung jetzt: Löschen und Annehmen serialisieren über die
// Benutzerzeile; nach beiden Läufen gibt es keine Rolle des gelöschten Kontos,
// keinen Deadlock (Postgres bricht einen Beteiligten sonst mit 40P01 ab => 500).
func TestAccountDeletePostgres_VsAcceptInvitation(t *testing.T) {
	variants := []struct {
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
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			db := startInvitationPostgres(t)
			if err := db.AutoMigrate(&models.IncidentUpdate{}, &models.ActionItem{}, &models.PageOrder{}, &models.Subscriber{},
				&models.UserTag{}, &models.OIDCLoginState{}, &models.SAMLLoginState{}, &models.NotificationRead{}); err != nil {
				t.Fatal(err)
			}
			f := newInvFixtureOn(t, db)
			h := handlers.NewHandler(db)
			auth := f.r.Group("/api", middleware.AuthMiddleware())
			auth.DELETE("/admin/users/:id", h.DeleteUserHandler)
			auth.DELETE("/me", h.DeleteMeHandler)
			warmUpPool(t, db, 8)
			const rounds = 40
			for i := 0; i < rounds; i++ {
				u, utok := f.account(t, fmt.Sprintf("del%d@x.example", i))
				raw := fmt.Sprintf("tok-%s-%d", v.name, i)
				if err := db.Create(&models.PageInvitation{StatusPageID: f.page.ID, Email: u.Email, RoleID: f.viewer.ID,
					TokenHash: hashToken(raw), ExpiresAt: time.Now().Add(time.Hour), InvitedByID: f.owner.ID, InvitedUserID: u.ID}).Error; err != nil {
					t.Fatal(err)
				}
				var wg sync.WaitGroup
				start := make(chan struct{})
				var accCode, delCode int
				var delBody string
				wg.Add(2)
				go func() {
					defer wg.Done()
					<-start
					accCode = idorRequest(t, f.r, http.MethodPost, "/api/invitations/accept", utok, map[string]any{"token": raw}).Code
				}()
				go func() {
					defer wg.Done()
					<-start
					// Versatz: das Löschen soll in verschiedene Phasen der Annahme fallen.
					time.Sleep(time.Duration(i%8) * 700 * time.Microsecond)
					w := idorRequest(t, f.r, http.MethodDelete, v.path(u), v.tok(f, utok), v.body(u))
					delCode, delBody = w.Code, w.Body.String()
				}()
				close(start)
				done := make(chan struct{})
				go func() { wg.Wait(); close(done) }()
				select {
				case <-done:
				case <-time.After(30 * time.Second):
					t.Fatalf("Runde %d: hängt", i)
				}
				if delCode != http.StatusOK {
					t.Fatalf("Runde %d: Löschen antwortet %d: %s", i, delCode, delBody)
				}
				if accCode >= 500 {
					t.Fatalf("Runde %d: Annehmen antwortet %d (Deadlock/Abbruch)", i, accCode)
				}
				var roles int64
				db.Unscoped().Model(&models.UserStatusPageRole{}).Where("user_id = ?", u.ID).Count(&roles)
				if roles != 0 {
					t.Fatalf("Runde %d: %d Rollenzeile(n) des gelöschten Kontos (Annehmen: %d)", i, roles, accCode)
				}
				var left int64
				db.Unscoped().Model(&models.PageInvitation{}).Where("invited_user_id = ?", u.ID).Count(&left)
				if left != 0 {
					t.Fatalf("Runde %d: %d Einladung(en) des gelöschten Kontos übrig", i, left)
				}
			}
		})
	}
}
