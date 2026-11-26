package handlers_test

// SQLite counterpart to page_role_remove_postgres_test.go: If the
// membership is removed between the check and the write, the
// role change answers with 404 user_not_found and does not bring the row back. Previously
// Save() wrote the row back via upsert (deleted_at = NULL) and answered
// 200. The removal is injected at exactly the moment the handler
// locks the user row (FOR UPDATE; SQLite omits the clause).

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

func removeMembershipOnLock(t *testing.T, db *gorm.DB, userID uint) {
	t.Helper()
	err := db.Callback().Query().Before("gorm:query").Register("test:remove_member_on_lock", func(tx *gorm.DB) {
		if _, locking := tx.Statement.Clauses["FOR"]; !locking {
			return
		}
		if _, err := tx.Statement.ConnPool.ExecContext(tx.Statement.Context,
			"UPDATE user_status_page_roles SET deleted_at = ? WHERE user_id = ? AND deleted_at IS NULL", time.Now(), userID); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPageUserRoleChange_MemberGoneBeforeWrite(t *testing.T) {
	p := setupPageUsers(t)
	_, gaTok := p.user("ga@x.example", true)
	u, _ := p.user("target@x.example", false)
	p.member(u, p.pageA, p.viewer)
	removeMembershipOnLock(t, p.db, u.ID)

	w := idorRequest(t, p.r, http.MethodPost, p.base(p.pageA), gaTok, map[string]any{"user_id": u.ID, "role_id": p.ownerRol.ID})
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "user_not_found") {
		t.Fatalf("erwartet 404 user_not_found, war %d %s", w.Code, w.Body.String())
	}
	// The transaction rolls back and takes the injected removal with it; the
	// row remains unchanged (no new role). The "do not bring back" is proven
	// by the Postgres test (page_role_remove_postgres_test.go).
	var role int64
	p.db.Unscoped().Model(&models.UserStatusPageRole{}).Select("role_id").Where("user_id = ?", u.ID).Scan(&role)
	if role != int64(p.viewer.ID) {
		t.Errorf("Rolle der Zeile ist %d, erwartet unverändert %d", role, p.viewer.ID)
	}
	var audits int64
	p.db.Model(&models.AuditLog{}).Where("action = ?", "statuspage.user_role_assigned").Count(&audits)
	if audits != 0 {
		t.Errorf("%d Audit-Eintrag/Einträge für einen nicht erfolgten Rollenwechsel", audits)
	}
}
