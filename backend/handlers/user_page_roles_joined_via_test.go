package handlers_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/models"
)

// Saving a user's page roles keeps the origin of rows whose role is unchanged;
// only new or changed rows get the admin origin (assigned).
func TestUpdateUser_PageRolesKeepJoinedViaOfUnchangedRows(t *testing.T) {
	p := setupPageUsers(t)
	u, _ := p.user("keep@x.example", false)
	pageC := models.StatusPage{Name: "C", Slug: "c"}
	p.db.Create(&pageC)
	p.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: p.pageA.ID, RoleID: p.viewer.ID, JoinedVia: models.JoinedViaAutoJoin})
	p.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: p.pageB.ID, RoleID: p.viewer.ID, JoinedVia: models.JoinedViaLegacy})

	w := idorRequest(t, p.r, "PUT", "/api/users/"+strconv.Itoa(int(u.ID)), "", map[string]any{
		"email": u.Email,
		"page_roles": []map[string]any{
			{"status_page_id": p.pageA.ID, "role_id": p.viewer.ID},   // unchanged
			{"status_page_id": p.pageB.ID, "role_id": p.ownerRol.ID}, // changed
			{"status_page_id": pageC.ID, "role_id": p.viewer.ID},     // new
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	want := map[uint]string{
		p.pageA.ID: models.JoinedViaAutoJoin,
		p.pageB.ID: models.JoinedViaAssigned,
		pageC.ID:   models.JoinedViaAssigned,
	}
	var rows []models.UserStatusPageRole
	p.db.Where("user_id = ?", u.ID).Find(&rows)
	if len(rows) != len(want) {
		t.Fatalf("expected %d rows, got %+v", len(want), rows)
	}
	for _, r := range rows {
		if r.JoinedVia != want[r.StatusPageID] {
			t.Errorf("page %d: joined_via %q, want %q", r.StatusPageID, r.JoinedVia, want[r.StatusPageID])
		}
	}
}
