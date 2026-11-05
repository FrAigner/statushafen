package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// meExportPageRole beschreibt eine einzelne Rollenzuweisung des Users für
// den Datenexport, mit aufgelöstem Namen von Statusseite und Rolle, damit
// der Export ohne weitere Nachschlagewerke lesbar ist.
type meExportPageRole struct {
	StatusPageID   uint   `json:"status_page_id"`
	StatusPageName string `json:"status_page_name"`
	RoleID         uint   `json:"role_id"`
	RoleName       string `json:"role_name"`
}

// meExportResponse fasst alle personenbezogenen Daten eines eingeloggten
// Users für den DSGVO-Datenexport zusammen. Enthält
// bewusst NICHT PasswordHash (schon per json:"-" auf models.User
// ausgeschlossen) und keine Daten anderer Nutzer - jede Teilmenge wird
// explizit per userID gefiltert statt z.B. über Preloads voller Relationen.
type meExportResponse struct {
	Profile       models.User          `json:"profile"`
	PageRoles     []meExportPageRole   `json:"page_roles"`
	Subscriptions []models.Subscriber  `json:"subscriptions"`
	AuditLogs     []models.AuditLog    `json:"audit_logs"`
	Invitations   []meExportInvitation `json:"invitations"`
}

// meExportInvitation: offene Einladung an die eigene Adresse,
// ohne Token-Hash und ohne Angaben zum Einladenden.
type meExportInvitation struct {
	StatusPageID   uint      `json:"status_page_id"`
	StatusPageName string    `json:"status_page_name"`
	RoleID         uint      `json:"role_id"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// MeExportHandler liefert GET /api/me/export: alle personenbezogenen Daten
// des eingeloggten Users als JSON-Download (DSGVO Art.
// 15/20). Die Route hängt hinter authGroup (JWT-Middleware in main.go) -
// ohne gültiges Token antwortet bereits die Middleware mit 401, bevor dieser
// Handler überhaupt läuft.
func (h *Handler) MeExportHandler(c *gin.Context) {
	userID := getUserID(c)

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found", "code": "unauthorized"})
		return
	}

	// H4: Jeder Lesefehler bricht den Export mit 500 ab. Ein
	// unvollständiger Export wäre für einen DSGVO-Auszug stillschweigend falsch.
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

	// Selbst angelegte Abos: Ersteller-Abos mit fremder Adresse nur, solange der
	// User noch zur Seite gehört (Global Admin oder Rolle), sonst nur die mit
	// der eigenen Adresse.
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

// exportInvitations (F7/H4): offene Einladungen, die diesem Konto
// gelten (InvitedUserID = Konto, nicht abgelaufen). Eine Einladung ohne Konto
// (ID 0) oder an ein früheres Konto mit gleicher Adresse gehört nicht in den
// Auszug. Die Seitennamen kommen mit EINER Abfrage statt einer je Einladung.
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
