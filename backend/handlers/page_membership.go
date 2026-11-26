package handlers

import (
	"errors"
	"net/http"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const codeLastPageOwner = "last_page_owner"

// deleteSSOIdentitiesOnPage removes the page SSO links
// of an account on EXACTLY this page. When the membership ends,
// the link would otherwise remain orphaned: It later blocks
// SSO login and attaching to other pages (hasSSOIdentityOnOtherPage,
// errOIDCOtherPageRole). UserSSOIdentity has no soft delete, so deletion is
// permanent. Identities on other pages remain untouched. Runs in the
// caller's transaction. It returns the number of deleted
// links for the audit entry.
func deleteSSOIdentitiesOnPage(tx *gorm.DB, userID, pageID uint) (int64, error) {
	res := tx.Where("user_id = ? AND status_page_id = ?", userID, pageID).
		Delete(&models.UserSSOIdentity{})
	return res.RowsAffected, res.Error
}

// deleteSSOIdentitiesForRoles removes the page SSO links for the
// given memberships. Thanks to the unique index there is at most
// one active row per (account, page).
func deleteSSOIdentitiesForRoles(tx *gorm.DB, rows []models.UserStatusPageRole) error {
	for _, r := range rows {
		if _, err := deleteSSOIdentitiesOnPage(tx, r.UserID, r.StatusPageID); err != nil {
			return err
		}
	}
	return nil
}

// dedupePageRoleInputs keeps only the last entry per page:
// The partial unique index (user_id, status_page_id) allows exactly one
// role per page; duplicate pages in the request would otherwise fail with 500.
// Entries without a role (RoleID 0) are kept unchanged.
func dedupePageRoleInputs(in []PageRoleInput) []PageRoleInput {
	last := map[uint]int{}
	for i, pr := range in {
		if pr.RoleID != 0 {
			last[pr.StatusPageID] = i
		}
	}
	out := make([]PageRoleInput, 0, len(in))
	for i, pr := range in {
		if pr.RoleID == 0 || last[pr.StatusPageID] == i {
			out = append(out, pr)
		}
	}
	return out
}

// errLastPageOwner: the change would leave the page without a member with pages:update.
var errLastPageOwner = errors.New("last page owner")

// ensureOtherPageOwner returns errLastPageOwner if, besides userID,
// no other active member of the page has pages:update. Accounts that
// are soft-deleted do not count (like soleAdminPages in me_delete.go). Runs
// in the caller's transaction; the page's membership rows are locked under
// Postgres with FOR UPDATE so that two owners do not both
// see "the other one" at the same time (SQLite serializes writers anyway).
func ensureOtherPageOwner(tx *gorm.DB, pageID, userID uint) error {
	var rows []models.UserStatusPageRole
	// SQLite does not know FOR UPDATE, GORM's dialect omits the clause there.
	q := tx.Preload("Role").
		Joins("JOIN users ON users.id = user_status_page_roles.user_id AND users.deleted_at IS NULL").
		Where("user_status_page_roles.status_page_id = ?", pageID).
		Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "user_status_page_roles"}})
	if err := q.Find(&rows).Error; err != nil {
		return err
	}
	upd := permissions.Perm("pages", permissions.Update)
	for _, r := range rows {
		if r.UserID != userID && r.Role.ID != 0 && permissions.Has(r.Role.Permissions, upd) {
			return nil
		}
	}
	return errLastPageOwner
}

// lockOutApplies: Does the change affect the lock-out rule? That holds
// for a non-global admin who removes themselves (newRole == nil) or
// downgrades to a role without pages:update although they currently have pages:update.
// Global admins are never affected (they can repair at any time).
func (h *Handler) lockOutApplies(c *gin.Context, targetID uint, current models.Role, newRole *models.Role) bool {
	if targetID != getUserID(c) || h.isEffectiveGlobalAdmin(c) {
		return false
	}
	upd := permissions.Perm("pages", permissions.Update)
	if !permissions.Has(current.Permissions, upd) {
		return false // was not an owner anyway, nothing is lost
	}
	return newRole == nil || !permissions.Has(newRole.Permissions, upd)
}

func respondLastPageOwner(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{"error": "You are the last user with page management rights on this page", "code": codeLastPageOwner})
}
