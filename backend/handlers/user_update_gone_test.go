package handlers_test

// SQLite counterpart to user_update_postgres_test.go: if the account is
// deleted between the check and the write, PUT
// /api/users/:id (page_roles) and POST /users (Global Admin, create and change
// role) answer with 404 user_not_found and create no role. The deletion is
// injected here at exactly the moment the handler locks the
// user row (FOR UPDATE; SQLite omits the clause, but it is in the statement).

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// deleteUserOnLock soft-deletes the account in the transaction's connection, shortly
// before the lock query runs: the result of the deletion by a
// concurrent call, which Postgres re-evaluates after waiting.
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
