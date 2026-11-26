package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/fraigner/statushafen/permissions"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Responses of the page user API carry only what the administration
// needs. The keys keep their previous spelling (ID, User.Email,
// Role.Name) so the frontend stays unchanged.
type pageUserDTO struct {
	ID           uint            `json:"ID"`
	UserID       uint            `json:"UserID"`
	StatusPageID uint            `json:"StatusPageID"`
	RoleID       uint            `json:"RoleID"`
	User         pageUserAccount `json:"User"`
	Role         pageUserRole    `json:"Role"`
}

type pageUserAccount struct {
	ID    uint   `json:"ID"`
	Email string `json:"Email"`
}

type pageUserRole struct {
	ID   uint   `json:"ID"`
	Name string `json:"Name"`
}

func newPageUserDTO(e models.UserStatusPageRole) pageUserDTO {
	return pageUserDTO{
		ID: e.ID, UserID: e.UserID, StatusPageID: e.StatusPageID, RoleID: e.RoleID,
		User: pageUserAccount{ID: e.User.ID, Email: e.User.Email},
		Role: pageUserRole{ID: e.Role.ID, Name: e.Role.Name},
	}
}

// respondPageUserAssigned: added by email -> neutral response (reveals
// nothing about the account), otherwise the assignment without account data.
func respondPageUserAssigned(c *gin.Context, status int, e models.UserStatusPageRole, byEmail bool) {
	if byEmail {
		c.JSON(http.StatusOK, gin.H{"message": pageUserEmailAcceptedMsg})
		return
	}
	c.JSON(status, pageUserDTO{ID: e.ID, UserID: e.UserID, StatusPageID: e.StatusPageID, RoleID: e.RoleID})
}

// isEffectiveGlobalAdmin: fresh DB view of IsGlobalAdmin (page SSO
// sessions never count).
func (h *Handler) isEffectiveGlobalAdmin(c *gin.Context) bool {
	var u models.User
	if err := h.DB.First(&u, getUserID(c)).Error; err != nil {
		return false
	}
	return middleware.EffectiveGlobalAdmin(c, u)
}

// actorPagePermissions: permission list of the signed-in user on the page
// (empty if they have no role there).
func (h *Handler) actorPagePermissions(c *gin.Context, pageID uint) string {
	var ur models.UserStatusPageRole
	if err := h.DB.Preload("Role").Where(whereUserIDAndStatusPageID, getUserID(c), pageID).First(&ur).Error; err != nil {
		return ""
	}
	return ur.Role.Permissions
}

// resolvePageUserTarget determines the target account. Returns: account,
// found, status != 0 if the response has already been sent.
//   - Global Admin: user_id (unknown -> 404 user_not_found) or email.
//   - Otherwise: user_id only for existing members of this page; any other
//     account only by email. With neither -> 400 (the same for a known and an
//     unknown ID, so accounts cannot be enumerated).
func (h *Handler) resolvePageUserTarget(c *gin.Context, isGA bool, pageID, userID uint, email string) (models.User, bool, int) {
	var u models.User
	email = strings.ToLower(strings.TrimSpace(email))
	if userID != 0 {
		memberOnly := h.DB.Model(&models.UserStatusPageRole{}).Where(whereUserIDAndStatusPageID, userID, pageID)
		var n int64
		memberOnly.Count(&n)
		if !isGA && n == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Add accounts by email address", "code": "user_email_required"})
			return u, false, http.StatusBadRequest
		}
		if err := h.DB.First(&u, userID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found", "code": "user_not_found"})
			return u, false, http.StatusNotFound
		}
		return u, true, 0
	}
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Add accounts by email address", "code": "user_email_required"})
		return u, false, http.StatusBadRequest
	}
	lookupErr := h.DB.Where("lower(email) = ?", email).First(&u).Error
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		// A DB error is not an "unknown address" (otherwise an invitation without
		// a mail would be created and the error would stay invisible): fail closed.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
		return u, false, http.StatusInternalServerError
	}
	if lookupErr != nil && isGA {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found", "code": "user_not_found"})
		return u, false, http.StatusNotFound
	}
	// Timing oracle: the three follow-up queries run for found
	// AND unknown addresses (for an unknown address with ID 0, which finds
	// nothing) and all unconditionally (no short-circuit via ||), so the number of
	// queries does not reveal whether an account exists. Equal runtime
	// remains an approximation (cache, row count); the query count is equal.
	otherPageRole := h.hasRoleOnOtherPage(u.ID, pageID)
	otherPageSSO := h.hasSSOIdentityOnOtherPage(u.ID, pageID)
	var memberN int64
	if err := h.DB.Model(&models.UserStatusPageRole{}).Where(whereUserIDAndStatusPageID, u.ID, pageID).Count(&memberN).Error; err != nil {
		// Review finding 5: no "memberN = 0" on a DB error (fail closed).
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
		return u, false, http.StatusInternalServerError
	}
	if lookupErr != nil {
		return u, false, 0
	}
	// Global Admin accounts, accounts with an (old or page) SSO link, or a role on another page
	// can only be attached by a Global Admin (otherwise the victim would lose the SSO login
	// of their page). Treated like "unknown": same response, nothing written.
	// Members of this page can still be re-ranked.
	if !isGA && (u.IsGlobalAdmin || u.SSOProvider != "" || u.SSOSubject != "" || otherPageRole || otherPageSSO) && memberN == 0 {
		return models.User{}, false, 0
	}
	return u, true, 0
}

// roleCoveredByActor: the role's permissions lie entirely within the caller's
// permissions. A missing (e.g. soft-deleted) role never counts as
// covered; only Global Admins change such members.
func roleCoveredByActor(actorPerms string, r models.Role) bool {
	return r.ID != 0 && permissions.Covers(actorPerms, r.Permissions)
}

func abortRoleExceedsOwn(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"error": "The role grants more rights than you have on this page", "code": "role_exceeds_own_permissions"})
}
