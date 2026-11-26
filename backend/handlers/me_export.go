package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// meExportPageRole describes a single role assignment of the user for
// the data export, with the resolved names of status page and role, so that
// the export is readable without further lookups.
type meExportPageRole struct {
	StatusPageID   uint   `json:"status_page_id"`
	StatusPageName string `json:"status_page_name"`
	RoleID         uint   `json:"role_id"`
	RoleName       string `json:"role_name"`
}

// meExportResponse summarizes all personal data of a logged-in
// user for the GDPR data export. Deliberately does
// NOT contain PasswordHash (already excluded via json:"-" on models.User)
// and no data of other users - every subset is
// explicitly filtered by userID instead of e.g. full relation preloads.
type meExportResponse struct {
	Profile       models.User          `json:"profile"`
	PageRoles     []meExportPageRole   `json:"page_roles"`
	Subscriptions []models.Subscriber  `json:"subscriptions"`
	AuditLogs     []models.AuditLog    `json:"audit_logs"`
	Invitations   []meExportInvitation `json:"invitations"`
}

// meExportInvitation: open invitation to the user's own address,
// without token hash and without details about the inviter.
type meExportInvitation struct {
	StatusPageID   uint      `json:"status_page_id"`
	StatusPageName string    `json:"status_page_name"`
	RoleID         uint      `json:"role_id"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// MeExportHandler serves GET /api/me/export: all personal data
// of the logged-in user as a JSON download (GDPR Art.
// 15/20). The route sits behind authGroup (JWT middleware in main.go) -
// without a valid token the middleware already answers with 401, before this
// handler even runs.
func (h *Handler) MeExportHandler(c *gin.Context) {
	userID := getUserID(c)

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found", "code": "unauthorized"})
		return
	}

	// H4: every read error aborts the export with 500. An
	// incomplete export would be silently wrong for a GDPR extract.
	exportFailed := func() {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Export", "code": "server_error"})
	}
	var roles []models.UserStatusPageRole
	if err := h.DB.Preload("StatusPage").Preload("Role").Where("user_id = ?", userID).Find(&roles).Error; err != nil {
		exportFailed()
		return
	}
	pageRoles := make([]meExportPageRole, len(roles))
	for i, r := range roles {
		pageRoles[i] = meExportPageRole{
			StatusPageID:   r.StatusPageID,
			StatusPageName: r.StatusPage.Name,
			RoleID:         r.RoleID,
			RoleName:       r.Role.Name,
		}
	}

	// Self-created subscriptions: creator subscriptions with a foreign address only while the
	// user still belongs to the page (Global Admin or role), otherwise only those with
	// their own address.
	subsQuery := h.DB.Where("created_by_user_id = ?", userID)
	if !user.IsGlobalAdmin {
		subsQuery = subsQuery.Where("lower(email) = ? OR status_page_id IN (?)", strings.ToLower(strings.TrimSpace(user.Email)),
			h.DB.Model(&models.UserStatusPageRole{}).Select("status_page_id").Where("user_id = ?", userID))
	}
	subscriptions := []models.Subscriber{}
	if err := subsQuery.Find(&subscriptions).Error; err != nil {
		exportFailed()
		return
	}

	auditLogs := []models.AuditLog{}
	if err := h.DB.Where("actor_id = ?", userID).Order("created_at desc").Find(&auditLogs).Error; err != nil {
		exportFailed()
		return
	}

	invitations, err := h.exportInvitations(user)
	if err != nil {
		exportFailed()
		return
	}

	c.Header("Content-Disposition", `attachment; filename="me-export.json"`)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, meExportResponse{
		Profile:       user,
		PageRoles:     pageRoles,
		Subscriptions: subscriptions,
		AuditLogs:     auditLogs,
		Invitations:   invitations,
	})
}

// exportInvitations (F7/H4): open invitations that apply to this account
// (InvitedUserID = account, not expired). An invitation without an account
// (ID 0) or to a former account with the same address does not belong in the
// extract. The page names come with ONE query instead of one per invitation.
func (h *Handler) exportInvitations(user models.User) ([]meExportInvitation, error) {
	var invs []models.PageInvitation
	if err := h.DB.Where("invited_user_id = ? AND email = ? AND expires_at > ?", user.ID, strings.ToLower(strings.TrimSpace(user.Email)), time.Now()).
		Order("created_at desc").Find(&invs).Error; err != nil {
		return nil, err
	}
	names := map[uint]string{}
	if len(invs) > 0 {
		ids := make([]uint, 0, len(invs))
		for _, inv := range invs {
			ids = append(ids, inv.StatusPageID)
		}
		var pages []models.StatusPage
		if err := h.DB.Select("id", "name").Where("id IN ?", ids).Find(&pages).Error; err != nil {
			return nil, err
		}
		for _, p := range pages {
			names[p.ID] = p.Name
		}
	}
	out := make([]meExportInvitation, len(invs))
	for i, inv := range invs {
		out[i] = meExportInvitation{StatusPageID: inv.StatusPageID, StatusPageName: names[inv.StatusPageID], RoleID: inv.RoleID, CreatedAt: inv.CreatedAt, ExpiresAt: inv.ExpiresAt}
	}
	return out, nil
}
