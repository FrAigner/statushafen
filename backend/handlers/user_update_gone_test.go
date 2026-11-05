package handlers_test

// SQLite-Gegenstück zu user_update_postgres_test.go: Wird das Konto
// zwischen der Prüfung und dem Schreiben gelöscht, antworten PUT
// /api/users/:id (page_roles) und POST /users (Global Admin, anlegen und Rolle
// wechseln) mit 404 user_not_found und legen keine Rolle an. Die Löschung wird
// hier in genau dem Moment eingespielt, in dem der Handler die Benutzerzeile
// sperrt (FOR UPDATE; SQLite lässt die Klausel weg, sie steht aber im Statement).

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// deleteUserOnLock soft-löscht das Konto in der Verbindung der Transaktion, kurz
// bevor die Sperrabfrage läuft: das Ergebnis der Löschung durch einen
// gleichzeitigen Aufruf, den Postgres nach dem Warten neu auswertet.
func deleteUserOnLock(t *testing.T, db *gorm.DB, userID uint) {
	t.Helper()
	err := db.Callback().Query().Before("gorm:query").Register("test:delete_user_on_lock", func(tx *gorm.DB) {
		if _, locking := tx.Statement.Clauses["FOR"]; !locking {
			return
		}
		if _, err := tx.Statement.ConnPool.ExecContext(tx.Statement.Context,
			"UPDATE users SET deleted_at = ? WHERE id = ?", time.Now(), userID); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUserWrite_AccountGoneBeforeLock(t *testing.T) {
	cases := []struct {
		name   string
		member bool
		method string
		path   func(p *pageUsers, u models.User) string
		body   func(p *pageUsers, u models.User) any
	}{
		{"PUT page_roles", false, http.MethodPut,
			func(_ *pageUsers, u models.User) string { return "/api/users/" + strconv.Itoa(int(u.ID)) },
			func(p *pageUsers, u models.User) any {
				return map[string]any{"email": u.Email, "page_roles": []map[string]any{{"status_page_id": p.pageA.ID, "role_id": p.viewer.ID}}}
			}},
		{"POST neues Mitglied", false, http.MethodPost,
			func(p *pageUsers, _ models.User) string { return p.base(p.pageA) },
			func(p *pageUsers, u models.User) any {
				return map[string]any{"user_id": u.ID, "role_id": p.viewer.ID}
			}},
		{"POST Rollenwechsel", true, http.MethodPost,
			func(p *pageUsers, _ models.User) string { return p.base(p.pageA) },
			func(p *pageUsers, u models.User) any {
				return map[string]any{"user_id": u.ID, "role_id": p.ownerRol.ID}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := setupPageUsers(t)
			_, gaTok := p.user("ga@x.example", true)
			u, _ := p.user("target@x.example", false)
			if tc.member {
				p.member(u, p.pageA, p.viewer)
			}
			deleteUserOnLock(t, p.db, u.ID)
			w := idorRequest(t, p.r, tc.method, tc.path(p, u), gaTok, tc.body(p, u))
			if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "user_not_found") {
				t.Fatalf("erwartet 404 user_not_found, war %d %s", w.Code, w.Body.String())
			}
			var roles int64
			p.db.Unscoped().Model(&models.UserStatusPageRole{}).Where("user_id = ? AND role_id <> ?", u.ID, p.viewer.ID).Count(&roles)
			if roles != 0 {
				t.Errorf("%d unerwartete Rollenzeile(n) am gelöschten Konto", roles)
			}
			if !tc.member {
				var all int64
				p.db.Unscoped().Model(&models.UserStatusPageRole{}).Where("user_id = ?", u.ID).Count(&all)
				if all != 0 {
					t.Errorf("%d Rollenzeile(n) am gelöschten Konto", all)
				}
			}
		})
	}
}
