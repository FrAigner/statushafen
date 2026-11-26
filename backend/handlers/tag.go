package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// Repeated literals (go:S1192).
const whereTagIDEquals = "tag_id = ?"

// tagNameTakenMessage is the 409 message when an active tag in the same scope
// already carries the name (additionally with error code tag_name_taken).
const tagNameTakenMessage = "Ein Tag mit diesem Namen existiert in diesem Scope bereits"

// GetTagsHandler returns the tag catalog (step 3/4).
// Without status_page_id all tags are returned (global + all
// status pages) - for the global management page, which shows both scopes in
// one overview. With status_page_id only the tags relevant for this page
// are returned (global + exactly this page), analogous to the
// page_id scoping in GetComponentsHandler.
func (h *Handler) GetTagsHandler(c *gin.Context) {
	tags := []models.Tag{}
	query := h.DB.Order("status_page_id asc, name asc")

	if pageIDStr := c.Query("status_page_id"); pageIDStr != "" {
		pageID, err := strconv.ParseUint(pageIDStr, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status_page_id parameter", "code": "invalid_id"})
			return
		}
		query = query.Where("status_page_id IS NULL OR status_page_id = ?", pageID)
	}

	if err := query.Find(&tags).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tags", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, tags)
}

// GetPageTagsHandler returns the tags selectable for a status page:
// global tags plus the tags of exactly this page, never tags
// of other pages (tenant isolation). Unlike GetTagsHandler it only needs
// a page permission (route: components:read), so that page roles can use the
// tag field in component/incident forms. Every tag carries
// status_page_id anyway; the frontend uses it to lock global tags for
// page roles (the backend additionally checks assignment).
func (h *Handler) GetPageTagsHandler(c *gin.Context) {
	pageID, ok := parseTenantID(c)
	if !ok {
		return
	}
	if err := h.DB.Select("id").First(&models.StatusPage{}, pageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return
	}
	tags := []models.Tag{}
	if err := h.DB.Where("status_page_id IS NULL OR status_page_id = ?", pageID).
		Order("status_page_id asc, name asc").Find(&tags).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tags", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, tags)
}

// CreateTagHandler creates a new tag in the catalog - global (StatusPageID
// == nil) or bound to a status page, see models.Tag.
func (h *Handler) CreateTagHandler(c *gin.Context) {
	type CreateTagReq struct {
		Name         string `json:"name" binding:"required"`
		StatusPageID *uint  `json:"status_page_id"`
	}
	var req CreateTagReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name darf nicht leer sein", "code": "name_required"})
		return
	}

	if req.StatusPageID != nil {
		if err := h.DB.First(&models.StatusPage{}, *req.StatusPageID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
			return
		}
	}

	// Case-insensitive duplicate check in the same scope, consistent with the
	// partial unique indexes idx_tags_global_name_active / idx_tags_scoped_name_active from
	// CreateSoftDeleteUniqueIndexes() in backend/database/db.go. The
	// pre-check yields a clean 409 response instead of a raw DB error;
	// the indexes additionally remain as a DB-side safety net against races.
	dupQuery := h.DB.Model(&models.Tag{}).Where("LOWER(name) = LOWER(?)", name)
	if req.StatusPageID != nil {
		dupQuery = dupQuery.Where("status_page_id = ?", *req.StatusPageID)
	} else {
		dupQuery = dupQuery.Where("status_page_id IS NULL")
	}
	var count int64
	if err := dupQuery.Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check for duplicate tag", "code": "server_error"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": tagNameTakenMessage, "code": "tag_name_taken"})
		return
	}

	tag := models.Tag{Name: name, StatusPageID: req.StatusPageID}
	if err := h.DB.Create(&tag).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": tagNameTakenMessage, "code": "tag_name_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create tag", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "tag.created", "config", "Tag", strconv.FormatUint(uint64(tag.ID), 10), tag.Name, "")
	c.JSON(http.StatusCreated, tag)
}

// UpdateTagHandler renames an existing tag (the requirement explicitly names
// "create, rename and delete"). The scope (global vs. status page) is
// deliberately not changeable - a later move between the scopes
// is not part of the requirement and would put already assigned
// ComponentTag/IncidentTag/UserTag rows into an inconsistent
// state.
func (h *Handler) UpdateTagHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID", "code": "invalid_id"})
		return
	}
	var tag models.Tag
	if err := h.DB.First(&tag, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found", "code": "tag_not_found"})
		return
	}

	type UpdateTagReq struct {
		Name string `json:"name" binding:"required"`
	}
	var req UpdateTagReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name darf nicht leer sein", "code": "name_required"})
		return
	}

	dupQuery := h.DB.Model(&models.Tag{}).Where("LOWER(name) = LOWER(?) AND id <> ?", name, tag.ID)
	if tag.StatusPageID != nil {
		dupQuery = dupQuery.Where("status_page_id = ?", *tag.StatusPageID)
	} else {
		dupQuery = dupQuery.Where("status_page_id IS NULL")
	}
	var count int64
	if err := dupQuery.Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check for duplicate tag", "code": "server_error"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": tagNameTakenMessage, "code": "tag_name_taken"})
		return
	}

	tag.Name = name
	if err := h.DB.Save(&tag).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": tagNameTakenMessage, "code": "tag_name_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tag", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "tag.updated", "config", "Tag", strconv.FormatUint(id, 10), tag.Name, "")
	c.JSON(http.StatusOK, tag)
}

// DeleteTagHandler removes a tag from the catalog. The assignment of tags
// to components/incidents/users (ComponentTag/IncidentTag/UserTag) is not
// part of this change - when deleting, the corresponding
// join rows must nevertheless be cleaned up.
func (h *Handler) DeleteTagHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID", "code": "invalid_id"})
		return
	}
	var tag models.Tag
	if err := h.DB.First(&tag, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found", "code": "tag_not_found"})
		return
	}

	// Analogous to DeleteIncidentHandler: Tag uses soft delete like all models here
	// (gorm.Model -> UPDATE instead of DELETE), the DB-side
	// OnDelete:CASCADE on ComponentTag/IncidentTag/UserTag never fires
	// in that case - so clean up join rows explicitly instead of relying on the
	// FK constraint.
	if err := h.DB.Where(whereTagIDEquals, tag.ID).Delete(&models.ComponentTag{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean up component tag links", "code": "server_error"})
		return
	}
	if err := h.DB.Where(whereTagIDEquals, tag.ID).Delete(&models.IncidentTag{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean up incident tag links", "code": "server_error"})
		return
	}
	if err := h.DB.Where(whereTagIDEquals, tag.ID).Delete(&models.UserTag{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean up user tag links", "code": "server_error"})
		return
	}

	if err := h.DB.Delete(&tag).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete tag", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "tag.deleted", "config", "Tag", strconv.FormatUint(id, 10), tag.Name, "")
	c.JSON(http.StatusOK, gin.H{"message": "Tag deleted successfully"})
}
