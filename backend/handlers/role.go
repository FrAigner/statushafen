package handlers

import (
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// roleResponse ergänzt GlobalOnly, damit das Frontend die nicht
// als Seitenrolle vergebbare Rolle aus den Seitenrollen-Auswahlen ausblendet,
// ohne den Rollennamen selbst zu kennen. Die Regel bleibt im Backend.
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
		// der Altwert "<ressource>:write" wird nie mehr gespeichert
		// (ein noch gecachtes Alt-Frontend schickt ihn), sondern zu CRUD.
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

	// Soft-Delete löst kein FK-OnDelete aus, die Seitenrollen-
	// Zuordnungen dieser Rolle explizit entfernen (Hard-Delete, wie
	// replaceUserPageRoles). Zusammen mit dem Löschen der Rolle in einer Transaktion.
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		// Mit der Rolle endet die Mitgliedschaft, die Seiten-SSO-
		// Verknüpfungen dieser Konten auf den betroffenen Seiten fallen mit weg.
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
		// Statusseiten, die diese Rolle als Standard-Rolle (Auto-Join)
		// führen, würden sonst auf eine gelöschte Rolle zeigen.
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
