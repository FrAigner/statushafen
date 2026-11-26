package handlers

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// DeletedUserMarker replaces the email address on account deletion
// in all records that are deliberately kept
// (audit log, incident timeline, post-mortem actions, page orders).
// Deliberately a fixed text rather than empty: ActorEmail "" or ActorID nil in the
// audit log otherwise stands for system/automation, and a deleted human should
// remain distinguishable.
const DeletedUserMarker = "[gelöschter Nutzer]"

// pagesWritePermission is the page permission that makes a user an admin
// of a status page (corresponds to permPagesWrite in main.go, with which
// RequirePermission protects page management).
const pagesWritePermission = "pages:update"

// meDeleteRequest: body of DELETE /api/me. ConfirmEmail is required for all
// accounts (a deliberate action, analogous to typing the name on GitHub),
// Password additionally for accounts with a password (re-authentication, so that a
// token left lying around is not enough on its own). ConfirmDeletePageIDs is the
// explicit confirmation that the pages of which the user is the only admin
// are irrevocably deleted along with the account.
type meDeleteRequest struct {
	Password             string `json:"password"`
	ConfirmEmail         string `json:"confirm_email"`
	ConfirmDeletePageIDs []uint `json:"confirm_delete_page_ids"`
}

// meDeletePage describes a status page that would be deleted with the account
// or was deleted.
type meDeletePage struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// DeleteMeHandler serves DELETE /api/me: deletes the user's own account (GDPR Art. 17).
// Operator decisions (of 2026-10-28): audit logs are anonymized instead of deleted; pages whose
// only admin is the user are deleted along with the account, but only after
// explicit confirmation - otherwise 409 with the list of these pages.
// Additionally (conservative, not decided by the operator): the last Global
// Admin cannot delete themselves, otherwise the platform would be without
// administration.
func (h *Handler) DeleteMeHandler(c *gin.Context) {
	var req meDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Anfrage", "code": "invalid_request"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, getUserID(c)).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found", "code": "unauthorized"})
		return
	}

	if scope := middleware.SSOPageID(c); scope != 0 && !h.pageSSOMayDeleteAccount(user, scope) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Mit der Anmeldung über eine Statusseite nicht möglich. Melde dich dafür unter der Hauptadresse an.",
			"code":  "page_sso_session",
		})
		return
	}

	if !accountDeletionConfirmed(user, req) {
		// 403 instead of 401: the frontend logs out on 401, here the user should
		// only be able to correct the input.
		c.JSON(http.StatusForbidden, gin.H{"error": "Bestätigung fehlgeschlagen", "code": "confirmation_invalid"})
		return
	}

	if user.IsGlobalAdmin && !h.otherGlobalAdminExists(user.ID) {
		c.JSON(http.StatusConflict, gin.H{
			"error": "Du bist der letzte Global Admin. Ernenne zuerst einen weiteren Global Admin.",
			"code":  "last_global_admin",
		})
		return
	}

	pages, err := h.soleAdminPages(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Konto konnte nicht gelöscht werden", "code": "server_error"})
		return
	}
	if !allPagesConfirmed(pages, req.ConfirmDeletePageIDs) {
		c.JSON(http.StatusConflict, gin.H{
			"error": "Du bist einziger Admin dieser Statusseiten. Sie werden mit deinem Konto unwiderruflich gelöscht und müssen ausdrücklich bestätigt werden.",
			"code":  "sole_admin_pages",
			"pages": pages,
		})
		return
	}

	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		return deleteAccount(tx, user, pages)
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Konto konnte nicht gelöscht werden", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Konto gelöscht", "deleted_pages": pages})
}

// accountDeletionConfirmed checks the confirmation from the request: the email
// must match (ignoring case and whitespace), and
// accounts with a password must provide it correctly. SSO accounts without a password
// confirm by email only (see report: a fresh
// SSO login cannot be verified without an iat claim in the token).
func accountDeletionConfirmed(user models.User, req meDeleteRequest) bool {
	if !strings.EqualFold(strings.TrimSpace(req.ConfirmEmail), user.Email) {
		return false
	}
	return user.PasswordHash == "" || database.CheckPasswordHash(req.Password, user.PasswordHash)
}

// pageSSOMayDeleteAccount reports whether a session limited to the SSO of
// pageID may delete the account. The page's IdP is run by the tenant and can
// mint such a session for every account linked to it; without a password the
// deletion is confirmed by e-mail only. So it may delete only an account that
// exists solely through this page: no other sign-in (global provider, legacy
// SSO, another page's SSO) and no membership on another page. With a password,
// accountDeletionConfirmed re-authenticates by password. False on a database
// error (fail closed).
func (h *Handler) pageSSOMayDeleteAccount(user models.User, pageID uint) bool {
	if user.PasswordHash != "" {
		return true
	}
	if user.IsGlobalAdmin || user.SSOProvider != "" || user.SSOSubject != "" {
		return false
	}
	var identities, memberships int64
	if h.DB.Model(&models.UserSSOIdentity{}).Where("user_id = ? AND status_page_id <> ?", user.ID, pageID).Count(&identities).Error != nil {
		return false
	}
	if h.DB.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id <> ?", user.ID, pageID).Count(&memberships).Error != nil {
		return false
	}
	return identities == 0 && memberships == 0
}

// otherGlobalAdminExists: fail-closed - if the count fails, n stays 0
// and the deletion is rejected rather than possibly leaving the platform without a Global
// Admin.
func (h *Handler) otherGlobalAdminExists(userID uint) bool {
	var n int64
	h.DB.Model(&models.User{}).Where("is_global_admin = ? AND id <> ?", true, userID).Count(&n)
	return n > 0
}

// soleAdminPages returns all (non-deleted) status pages on which the
// user has pages:write and no other non-deleted user does. Roles
// of soft-deleted users do not count: DeleteUserHandler leaves their
// role rows in place, but they can no longer manage the page anyway.
// Global Admins deliberately do not count as page admins (they manage the
// platform, not the page).
func (h *Handler) soleAdminPages(userID uint) ([]meDeletePage, error) {
	var mine []models.UserStatusPageRole
	if err := h.DB.Preload("Role").Preload("StatusPage").Where("user_id = ?", userID).Order("status_page_id").Find(&mine).Error; err != nil {
		return nil, err
	}
	pages := []meDeletePage{}
	for _, r := range mine {
		if r.StatusPage.ID == 0 || !permissionListContains(r.Role.Permissions, pagesWritePermission) {
			continue
		}
		var others []models.UserStatusPageRole
		if err := h.DB.Preload("Role").
			Joins("JOIN users ON users.id = user_status_page_roles.user_id AND users.deleted_at IS NULL").
			Where("user_status_page_roles.status_page_id = ? AND user_status_page_roles.user_id <> ?", r.StatusPageID, userID).
			Find(&others).Error; err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(others, func(o models.UserStatusPageRole) bool {
			return permissionListContains(o.Role.Permissions, pagesWritePermission)
		}) {
			pages = append(pages, meDeletePage{ID: r.StatusPage.ID, Name: r.StatusPage.Name, Slug: r.StatusPage.Slug})
		}
	}
	return pages, nil
}

// permissionListContains checks a comma-separated permissions list (same
// logic as permissions.Has).
func permissionListContains(list, required string) bool {
	return permissions.Has(list, required)
}

// allPagesConfirmed: every affected page must be named in the confirmation.
// Additionally named pages are harmless (they are not
// deleted), for example when another admin was added between the 409 and the confirmation.
// Conversely, if a new page is added, there is another 409.
func allPagesConfirmed(pages []meDeletePage, confirmed []uint) bool {
	for _, p := range pages {
		if !slices.Contains(confirmed, p.ID) {
			return false
		}
	}
	return true
}

// deleteAccount deletes or anonymizes all of the user's data within the
// transaction tx. Personal data without a retention reason is hard
// deleted (Unscoped, no soft delete); records kept for other purposes remain with
// DeletedUserMarker instead of the email. The pages are deleted as in
// DeleteStatusPageHandler (soft delete of the StatusPage).
func deleteAccount(tx *gorm.DB, user models.User, pages []meDeletePage) error {
	uid := strconv.FormatUint(uint64(user.ID), 10)
	email := user.Email
	m := DeletedUserMarker

	steps := []func() error{
		// previously (security review) at the end of the list:
		// delete invitations to and from this account and lock the user row,
		// BEFORE roles are removed (concurrent acceptance).
		func() error { return lockAccountForDeletion(tx, user) },
		func() error {
			return tx.Model(&models.AuditLog{}).Where("actor_id = ? OR actor_email = ?", user.ID, email).
				Updates(map[string]interface{}{"actor_id": nil, "actor_email": m, "actor_role": "", "ip": ""}).Error
		},
		func() error {
			return tx.Model(&models.AuditLog{}).Where("target_name = ? OR (target_type = ? AND target_id = ?)", email, "User", uid).
				Update("target_name", m).Error
		},
		func() error {
			return tx.Model(&models.AuditLog{}).Where("details LIKE ?", "%"+email+"%").
				Update("details", gorm.Expr("REPLACE(details, ?, ?)", email, m)).Error
		},
		func() error {
			return tx.Model(&models.IncidentUpdate{}).Where("author_email = ?", email).Update("author_email", m).Error
		},
		func() error {
			return tx.Model(&models.ActionItem{}).Where("owner = ?", email).Update("owner", m).Error
		},
		func() error {
			return tx.Unscoped().Model(&models.PageOrder{}).Where("user_id = ? OR email = ?", user.ID, email).
				Updates(map[string]interface{}{"user_id": nil, "email": m, "request_ip": ""}).Error
		},
		func() error {
			return tx.Unscoped().Where("created_by_user_id = ?", user.ID).Delete(&models.Subscriber{}).Error
		},
		func() error {
			return tx.Unscoped().Where("user_id = ?", user.ID).Delete(&models.UserStatusPageRole{}).Error
		},
		func() error { return tx.Unscoped().Where("user_id = ?", user.ID).Delete(&models.UserTag{}).Error },
		func() error { return tx.Where("user_id = ?", user.ID).Delete(&models.UserSSOIdentity{}).Error },
		func() error { return tx.Where("link_user_id = ?", user.ID).Delete(&models.OIDCLoginState{}).Error },
	}
	// Two separate steps per page instead of an inner loop: this way
	// every error passes through the same abort point below.
	for _, p := range pages {
		pageID := p.ID
		steps = append(steps,
			func() error { return tx.Delete(&models.StatusPage{}, pageID).Error },
			func() error {
				return tx.Create(&models.AuditLog{
					Action: "statuspage.deleted", Category: "config", TargetType: "StatusPage",
					TargetID: strconv.FormatUint(uint64(pageID), 10), TargetName: p.Name,
					StatusPageID: &pageID, ActorEmail: m, Details: `{"reason":"account_deleted"}`,
				}).Error
			},
		)
	}
	steps = append(steps,
		func() error {
			return tx.Create(&models.AuditLog{
				Action: "user.account_deleted", Category: "user", TargetType: "User",
				TargetID: uid, TargetName: m, ActorEmail: m,
			}).Error
		},
		// the user's own read markers of the message center (even without FK cascade).
		func() error { return tx.Where("user_id = ?", user.ID).Delete(&models.NotificationRead{}).Error },
		func() error { return tx.Unscoped().Delete(&models.User{}, user.ID).Error },
	)
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}
