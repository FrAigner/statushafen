package handlers

import (
	"net/http"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Error codes of user management. The frontend translates them
// via users.errors.<code>; the English "error" message remains for API users.
const (
	codeGlobalAdminRoleNotAssignable = "global_admin_role_not_assignable"
	codeCannotDeleteSelf             = "cannot_delete_self"
	codeCannotDemoteSelf             = "cannot_demote_self"
	codeLastGlobalAdmin              = "last_global_admin"
)

// globalOnlyRoleIDs returns the IDs of the given roles that must not be
// assigned as a page role (models.Role.GlobalOnly).
// O2: A DB error is returned as an error; callers
// then block (fail closed) instead of letting the role through as "assignable".
func (h *Handler) globalOnlyRoleIDs(db *gorm.DB, roleIDs []uint) (map[uint]bool, error) {
	out := map[uint]bool{}
	if len(roleIDs) == 0 {
		return out, nil
	}
	var roles []models.Role
	if err := db.Where("id IN ? AND name = ?", roleIDs, models.GlobalAdminRoleName).Find(&roles).Error; err != nil {
		return nil, err
	}
	for _, r := range roles {
		out[r.ID] = true
	}
	return out, nil
}

// pageRolesAssignable checks that none of the page roles newly
// assigns the global role "Global Admin". An already existing assignment
// of the same role on the same page remains allowed: it only gives the user
// the role's permissions on this page, and without this tolerance a legacy
// user could no longer be saved at all without stripping their rights. That is why
// no data migration is needed either: old rows
// keep their effect, only new ones are no longer created.
func (h *Handler) pageRolesAssignable(userID uint, pageRoles []PageRoleInput) bool {
	ids := make([]uint, 0, len(pageRoles))
	for _, pr := range pageRoles {
		ids = append(ids, pr.RoleID)
	}
	globalOnly, err := h.globalOnlyRoleIDs(h.DB, ids)
	if err != nil {
		return false // O2: fail closed
	}
	for _, pr := range pageRoles {
		if !globalOnly[pr.RoleID] {
			continue
		}
		var n int64
		h.DB.Model(&models.UserStatusPageRole{}).
			Where("user_id = ? AND status_page_id = ? AND role_id = ?", userID, pr.StatusPageID, pr.RoleID).
			Count(&n)
		if n == 0 {
			return false
		}
	}
	return true
}

// abortGlobalAdminRoleNotAssignable responds with 400 and an error code.
func abortGlobalAdminRoleNotAssignable(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error": "The Global Admin role cannot be assigned per status page; use is_global_admin instead",
		"code":  codeGlobalAdminRoleNotAssignable,
	})
}

// adminChangeConflict checks whether a deletion (deleting) or a
// step that takes Global Admin from the target (removesAdmin: deleting or
// demoting a global admin) would hit one's own account or the last Global
// Admin. Returns the error code or "".
// Self-deletion deliberately goes only via DELETE /api/me (with confirmation and
// handling of the pages of which one is the only admin).
func (h *Handler) adminChangeConflict(c *gin.Context, target models.User, deleting, removesAdmin bool) string {
	isSelf := target.ID == getUserID(c)
	if deleting && isSelf {
		return codeCannotDeleteSelf
	}
	if !removesAdmin {
		return ""
	}
	if isSelf {
		return codeCannotDemoteSelf
	}
	if !h.otherGlobalAdminExists(target.ID) {
		return codeLastGlobalAdmin
	}
	return ""
}

// abortAdminChangeConflict responds with 409 and an error code (409 instead of 403,
// because the caller is authorized and only the current state
// forbids the step; apiFetch also passes the body including the code on for 409).
func abortAdminChangeConflict(c *gin.Context, code string) {
	msg := map[string]string{
		codeCannotDeleteSelf: "You cannot delete your own account here; use your profile instead",
		codeCannotDemoteSelf: "You cannot remove your own Global Admin rights",
		codeLastGlobalAdmin:  "This is the last Global Admin; appoint another Global Admin first",
	}[code]
	c.JSON(http.StatusConflict, gin.H{"error": msg, "code": code})
}
