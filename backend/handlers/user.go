package handlers

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Repeated literals (go:S1192).
const preloadUserStatusPageRolesRole = "UserStatusPageRoles.Role"

// userResponse attaches the assigned tags (replacement for the
// former AllowedCountries field) - analogous to IncidentResponse/Components in
// incident.go, so that the admin frontend can display the current tag assignment
// and prefill it in the edit form.
type userResponse struct {
	models.User
	Tags []models.Tag `json:"Tags"`
}

func (h *Handler) GetUsersHandler(c *gin.Context) {
	users := []models.User{}
	if err := h.DB.Preload("Role").Preload("UserStatusPageRoles").Preload(preloadUserStatusPageRolesRole).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users", "code": "server_error"})
		return
	}

	out := make([]userResponse, len(users))
	for i, u := range users {
		out[i] = userResponse{User: u, Tags: h.tagsForUser(u.ID)}
	}
	c.JSON(http.StatusOK, out)
}

type PageRoleInput struct {
	StatusPageID uint `json:"status_page_id"`
	RoleID       uint `json:"role_id"`
}

func (h *Handler) CreateUserHandler(c *gin.Context) {
	type CreateUserReq struct {
		Email string `json:"email" binding:"required"`
		// Password is required while the password login is on and refused
		// while it is off (the account then signs in via OIDC).
		Password      string          `json:"password"`
		TagIDs        []uint          `json:"tag_ids"`
		IsGlobalAdmin bool            `json:"is_global_admin"`
		PageRoles     []PageRoleInput `json:"page_roles"`
	}
	var req CreateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}

	if req.Password == "" && passwordLoginEnabled() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if req.Password != "" && (rejectDisabledPasswordLogin(c) || rejectInvalidPassword(c, req.Password)) {
		return
	}

	var existing models.User
	if err := h.DB.Where("email = ?", req.Email).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User with this email already exists", "code": "email_taken"})
		return
	}

	// User tags are deliberately global only, never bound to a status page
	// - AllowedCountries was always cross-page.
	tags, err := h.resolveGlobalTagsByIDs(req.TagIDs)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}

	if !h.pageRolesAssignable(0, req.PageRoles) {
		abortGlobalAdminRoleNotAssignable(c)
		return
	}

	hash := ""
	if req.Password != "" {
		if hash, err = hashPassword(req.Password); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user", "code": "server_error"})
			return
		}
	}
	user := models.User{
		Email:         req.Email,
		PasswordHash:  hash,
		IsGlobalAdmin: req.IsGlobalAdmin,
	}

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		for _, pr := range dedupePageRoleInputs(req.PageRoles) {
			if pr.RoleID > 0 { // 0 means no role
				if err := tx.Create(&models.UserStatusPageRole{
					UserID:       user.ID,
					StatusPageID: pr.StatusPageID,
					RoleID:       pr.RoleID,
				}).Error; err != nil {
					return err
				}
			}
		}
		return h.replaceUserTags(tx, user.ID, tags)
	})

	if err != nil {
		if isUniqueViolation(err) { // same response as the pre-check
			c.JSON(http.StatusBadRequest, gin.H{"error": "User with this email already exists", "code": "email_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user", "code": "server_error"})
		return
	}

	h.DB.Preload("Role").Preload("UserStatusPageRoles").Preload(preloadUserStatusPageRolesRole).First(&user, user.ID)
	h.RecordAudit(c, "user.created", "user", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email, "")
	c.JSON(http.StatusCreated, userResponse{User: user, Tags: tags})
}

type UpdateUserReq struct {
	Email         string          `json:"email" binding:"required"`
	Password      string          `json:"password"` // optional
	TagIDs        []uint          `json:"tag_ids"`
	IsGlobalAdmin bool            `json:"is_global_admin"`
	PageRoles     []PageRoleInput `json:"page_roles"`
}

// replaceUserPageRoles deletes the existing UserStatusPageRole rows of a
// user (hard delete, to avoid duplicates/a cluttered DB) and creates
// the newly passed ones. Extracted from UpdateUserHandler so that its
// cognitive complexity does not exceed the SonarCloud limit (go:S3776).
// The result reports whether the assignment (page, role) actually changed.
func replaceUserPageRoles(tx *gorm.DB, userID uint, pageRoles []PageRoleInput) (bool, error) {
	pageRoles = dedupePageRoleInputs(pageRoles)
	var before []models.UserStatusPageRole
	if err := tx.Where("user_id = ?", userID).Find(&before).Error; err != nil {
		return false, err
	}
	changed := pageRolesDiffer(before, pageRoles)
	if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.UserStatusPageRole{}).Error; err != nil {
		return false, err
	}
	// Pages where the membership ends also lose
	// the account's page SSO link.
	var dropped []models.UserStatusPageRole
	for _, b := range before {
		if !slices.ContainsFunc(pageRoles, func(pr PageRoleInput) bool { return pr.RoleID != 0 && pr.StatusPageID == b.StatusPageID }) {
			dropped = append(dropped, b)
		}
	}
	if err := deleteSSOIdentitiesForRoles(tx, dropped); err != nil {
		return false, err
	}
	// Rows whose role is unchanged keep their origin (an auto-join stays an
	// auto-join); new or changed rows get the default (assigned).
	origin := map[[2]uint]string{}
	for _, b := range before {
		origin[[2]uint{b.StatusPageID, b.RoleID}] = b.JoinedVia
	}
	for _, pr := range pageRoles {
		if pr.RoleID == 0 {
			continue
		}
		if err := tx.Create(&models.UserStatusPageRole{
			UserID:       userID,
			StatusPageID: pr.StatusPageID,
			RoleID:       pr.RoleID,
			JoinedVia:    origin[[2]uint{pr.StatusPageID, pr.RoleID}],
		}).Error; err != nil {
			return false, err
		}
	}
	return changed, nil
}

// pageRolesDiffer compares the existing page roles with the new
// assignment; entries without a role (RoleID 0) count as not present.
func pageRolesDiffer(before []models.UserStatusPageRole, after []PageRoleInput) bool {
	type pair struct{ page, role uint }
	old := map[pair]bool{}
	for _, b := range before {
		old[pair{b.StatusPageID, b.RoleID}] = true
	}
	n := 0
	for _, a := range after {
		if a.RoleID == 0 {
			continue
		}
		n++
		if !old[pair{a.StatusPageID, a.RoleID}] {
			return true
		}
	}
	return n != len(old)
}

// bumpTokenVersion increments users.token_version, thereby invalidating all
// previously issued login tokens of the account (see middleware.checkSession).
// UpdateColumn leaves updated_at untouched and triggers no hooks.
func bumpTokenVersion(tx *gorm.DB, userID uint) error {
	return tx.Model(&models.User{}).Where("id = ?", userID).
		UpdateColumn("token_version", gorm.Expr("token_version + 1")).Error
}

// applyUserUpdate persists the field updates, the page roles and the
// tags of a user in one transaction.
// adminChanged: global-admin flag was changed. A password change and changed
// global-admin rights revoke the account's tokens. Page roles do not: they
// are read from the database on every request and take effect immediately.
func (h *Handler) applyUserUpdate(user *models.User, req UpdateUserReq, tags []models.Tag, adminChanged bool) error {
	return h.DB.Transaction(func(tx *gorm.DB) error {
		// Lock the user row first; a concurrently deleted
		// account yields ErrRecordNotFound (404) instead of a role on the account.
		if err := lockLiveUser(tx, user.ID); err != nil {
			return err
		}
		updates := map[string]interface{}{
			"email":           user.Email,
			"is_global_admin": user.IsGlobalAdmin,
		}
		if req.Password != "" {
			updates["password_hash"] = user.PasswordHash
		}
		if err := tx.Model(user).Updates(updates).Error; err != nil {
			return err
		}
		if _, err := replaceUserPageRoles(tx, user.ID, req.PageRoles); err != nil {
			return err
		}
		if req.Password != "" || adminChanged {
			if err := bumpTokenVersion(tx, user.ID); err != nil {
				return err
			}
		}
		return h.replaceUserTags(tx, user.ID, tags)
	})
}

func (h *Handler) UpdateUserHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID", "code": "invalid_id"})
		return
	}
	var user models.User
	if err := h.DB.First(&user, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found", "code": "user_not_found"})
		return
	}

	var req UpdateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid fields", "code": "invalid_request"})
		return
	}
	// empty = password unchanged, otherwise the minimum length applies.
	if req.Password != "" && (rejectDisabledPasswordLogin(c) || rejectInvalidPassword(c, req.Password)) {
		return
	}

	tags, err := h.resolveGlobalTagsByIDs(req.TagIDs)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}

	if !h.pageRolesAssignable(user.ID, req.PageRoles) {
		abortGlobalAdminRoleNotAssignable(c)
		return
	}

	wasGlobalAdmin := user.IsGlobalAdmin
	// Removing the rights from oneself or from the last global admin
	// would lock out platform administration.
	if code := h.adminChangeConflict(c, user, false, wasGlobalAdmin && !req.IsGlobalAdmin); code != "" {
		abortAdminChangeConflict(c, code)
		return
	}
	user.Email = req.Email
	user.IsGlobalAdmin = req.IsGlobalAdmin
	if req.Password != "" {
		hash, err := hashPassword(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user", "code": "server_error"})
			return
		}
		user.PasswordHash = hash
	}

	if err := h.applyUserUpdate(&user, req, tags, wasGlobalAdmin != req.IsGlobalAdmin); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) { // Account has been deleted in the meantime
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found", "code": "user_not_found"})
			return
		}
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "User with this email already exists", "code": "email_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user", "code": "server_error"})
		return
	}

	h.DB.Preload("Role").Preload("UserStatusPageRoles").Preload(preloadUserStatusPageRolesRole).First(&user, user.ID)
	h.RecordAudit(c, "user.updated", "user", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email, "")
	// Security-relevant partial changes as separate actions,
	// so that they are filterable in the audit log instead of being folded into user.updated.
	if req.Password != "" {
		h.RecordAudit(c, "user.password_changed", "user", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email, "")
	}
	if wasGlobalAdmin != user.IsGlobalAdmin {
		h.RecordAudit(c, "user.global_admin_changed", "user", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email, "is_global_admin="+strconv.FormatBool(user.IsGlobalAdmin))
	}
	c.JSON(http.StatusOK, userResponse{User: user, Tags: tags})
}

func (h *Handler) DeleteUserHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID", "code": "invalid_id"})
		return
	}
	var user models.User
	if err := h.DB.First(&user, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found", "code": "user_not_found"})
		return
	}

	// Self-deletion and deletion of the last global admin with
	// 409 and error code instead of 400 without a code (and by ID instead of by email).
	if code := h.adminChangeConflict(c, user, true, user.IsGlobalAdmin); code != "" {
		abortAdminChangeConflict(c, code)
		return
	}

	// Also remove the user's page roles (soft delete triggers no
	// FK OnDelete), atomically with deleting the user.
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		// Invitations removed and user row locked BEFORE roles
		// are removed (concurrent acceptance, see lockAccountForDeletion).
		if err := lockAccountForDeletion(tx, user); err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", user.ID).Delete(&models.UserStatusPageRole{}).Error; err != nil {
			return err
		}
		// Tags and SSO links are also cleaned up by the admin path,
		// as in me_delete, otherwise they remain as orphans.
		if err := tx.Unscoped().Where("user_id = ?", user.ID).Delete(&models.UserTag{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", user.ID).Delete(&models.UserSSOIdentity{}).Error; err != nil {
			return err
		}
		if err := bumpTokenVersion(tx, user.ID); err != nil {
			return err
		}
		return tx.Delete(&user).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "user.deleted", "user", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email, "")
	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}
