package handlers

import (
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// roleResponse adds GlobalOnly so that the frontend hides the role that
// cannot be assigned as a page role from the page role selections,
// without knowing the role name itself. The rule stays in the backend.
type roleResponse struct {
	models.Role
	GlobalOnly bool `json:"GlobalOnly"`
}

func (h *Handler) GetRolesHandler(c *gin.Context) {
	roles := []models.Role{}
	if err := h.DB.Find(&roles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch roles", "code": "server_error"})
		return
	}
	out := make([]roleResponse, len(roles))
	for i, r := range roles {
		out[i] = roleResponse{Role: r, GlobalOnly: r.GlobalOnly()}
	}
	c.JSON(http.StatusOK, out)
}

const roleNameTakenMessage = "Eine Rolle mit diesem Namen existiert bereits"

func (h *Handler) CreateRoleHandler(c *gin.Context) {
	type CreateRoleReq struct {
		Name        string `json:"name" binding:"required"`
		Permissions string `json:"permissions"`
	}
	var req CreateRoleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}

	role := models.Role{
		Name: req.Name,
		// the legacy value "<ressource>:write" is never stored anymore
		// (a still-cached old frontend sends it), but mapped to CRUD.
		Permissions: permissions.Normalize(req.Permissions),
	}
	if err := h.DB.Create(&role).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": roleNameTakenMessage, "code": "role_name_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create role", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "role.created", "user", "Role", strconv.FormatUint(uint64(role.ID), 10), role.Name, "")
	c.JSON(http.StatusCreated, role)
}

func (h *Handler) UpdateRoleHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID", "code": "invalid_id"})
		return
	}
	var role models.Role
	if err := h.DB.First(&role, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found", "code": "role_not_found"})
		return
	}

	type UpdateRoleReq struct {
		Name        string `json:"name" binding:"required"`
		Permissions string `json:"permissions"`
	}
	var req UpdateRoleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid fields", "code": "invalid_request"})
		return
	}

	role.Name = req.Name
	role.Permissions = permissions.Normalize(req.Permissions) // s. CreateRoleHandler

	if err := h.DB.Save(&role).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": roleNameTakenMessage, "code": "role_name_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update role", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "role.updated", "user", "Role", strconv.FormatUint(uint64(role.ID), 10), role.Name, "")
	c.JSON(http.StatusOK, role)
}

func (h *Handler) DeleteRoleHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID", "code": "invalid_id"})
		return
	}
	var role models.Role
	if err := h.DB.First(&role, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found", "code": "role_not_found"})
		return
	}

	// Soft delete triggers no FK OnDelete, so explicitly remove the page-role
	// assignments of this role (hard delete, like
	// replaceUserPageRoles). Together with deleting the role in one transaction.
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		// With the role the membership ends; the page SSO
		// links of these accounts on the affected pages are dropped as well.
		var members []models.UserStatusPageRole
		if err := tx.Where("role_id = ?", role.ID).Find(&members).Error; err != nil {
			return err
		}
		if err := deleteSSOIdentitiesForRoles(tx, members); err != nil {
			return err
		}
		if err := tx.Unscoped().Where("role_id = ?", role.ID).Delete(&models.UserStatusPageRole{}).Error; err != nil {
			return err
		}
		// Status pages that have this role as their default role (auto-join)
		// would otherwise point to a deleted role.
		if err := tx.Model(&models.StatusPage{}).Where("default_role_id = ?", role.ID).
			Update("default_role_id", nil).Error; err != nil {
			return err
		}
		return tx.Delete(&role).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete role", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "role.deleted", "user", "Role", strconv.FormatUint(uint64(role.ID), 10), role.Name, "")
	c.JSON(http.StatusOK, gin.H{"message": "Role deleted successfully"})
}
