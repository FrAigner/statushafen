package handlers

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// DeletedUserMarker ersetzt beim Löschen eines Kontos
// die E-Mail-Adresse in allen Datensätzen, die bewusst erhalten bleiben
// (Audit-Log, Incident-Timeline, Post-Mortem-Maßnahmen, Seitenbestellungen).
// Bewusst ein fester Text statt leer: ActorEmail "" bzw. ActorID nil steht im
// Audit-Log sonst für System/Automation, ein gelöschter Mensch soll davon
// unterscheidbar bleiben.
const DeletedUserMarker = "[gelöschter Nutzer]"

// pagesWritePermission ist die Seitenberechtigung, die einen Nutzer zum Admin
// einer Statusseite macht (entspricht permPagesWrite in main.go, mit der
// RequirePermission die Seitenverwaltung schützt).
const pagesWritePermission = "pages:update"

// meDeleteRequest: Body von DELETE /api/me. ConfirmEmail ist für alle Konten
// Pflicht (bewusste Handlung, analog zum Eintippen des Namens bei GitHub),
// Password zusätzlich für Konten mit Passwort (Re-Authentifizierung, damit ein
// liegen gelassenes Token allein nicht reicht). ConfirmDeletePageIDs ist die
// ausdrückliche Bestätigung, dass die Seiten, deren einziger Admin der Nutzer
// ist, unwiderruflich mitgelöscht werden.
type meDeleteRequest struct {
	Password             string `json:"password"`
	ConfirmEmail         string `json:"confirm_email"`
	ConfirmDeletePageIDs []uint `json:"confirm_delete_page_ids"`
}

// meDeletePage beschreibt eine Statusseite, die mit dem Konto gelöscht würde
// bzw. wurde.
type meDeletePage struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// DeleteMeHandler liefert DELETE /api/me: löscht das eigene Konto (DSGVO Art. 17).
// Entscheidungen des Betreibers (vom 2026-10-28): Audit-Logs werden anonymisiert statt gelöscht; Seiten, deren
// einziger Admin der Nutzer ist, werden mitgelöscht, aber nur nach
// ausdrücklicher Bestätigung - sonst 409 mit der Liste dieser Seiten.
// Zusätzlich (konservativ, nicht vom Betreiber entschieden): Der letzte Global
// Admin kann sich nicht selbst löschen, sonst wäre die Plattform ohne
// Verwaltung.
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

	if !accountDeletionConfirmed(user, req) {
		// 403 statt 401: das Frontend meldet bei 401 ab, hier soll der Nutzer
		// die Eingabe nur korrigieren können.
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

// accountDeletionConfirmed prüft die Bestätigung aus dem Request: Die E-Mail
// muss (ohne Rücksicht auf Groß-/Kleinschreibung und Leerraum) stimmen, und
// Konten mit Passwort müssen es korrekt angeben. SSO-Konten ohne Passwort
// bestätigen nur per E-Mail (siehe Bericht: eine frische
// SSO-Anmeldung lässt sich ohne iat-Claim im Token nicht prüfen).
func accountDeletionConfirmed(user models.User, req meDeleteRequest) bool {
	if !strings.EqualFold(strings.TrimSpace(req.ConfirmEmail), user.Email) {
		return false
	}
	return user.PasswordHash == "" || database.CheckPasswordHash(req.Password, user.PasswordHash)
}

// otherGlobalAdminExists: Fail-closed - schlägt die Zählung fehl, bleibt n 0
// und die Löschung wird abgelehnt statt die Plattform womöglich ohne Global
// Admin zurückzulassen.
func (h *Handler) otherGlobalAdminExists(userID uint) bool {
	var n int64
	h.DB.Model(&models.User{}).Where("is_global_admin = ? AND id <> ?", true, userID).Count(&n)
	return n > 0
}

// soleAdminPages liefert alle (nicht gelöschten) Statusseiten, auf denen der
// Nutzer pages:write hat und kein anderer, nicht gelöschter Nutzer. Rollen
// soft-gelöschter Nutzer zählen nicht: DeleteUserHandler lässt deren
// Rollenzeilen liegen, verwalten können sie die Seite trotzdem nicht mehr.
// Global Admins zählen bewusst nicht als Seiten-Admin (sie verwalten die
// Plattform, nicht die Seite).
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

// permissionListContains prüft eine kommagetrennte Permissions-Liste (gleiche
// Logik wie permissions.Has).
func permissionListContains(list, required string) bool {
	return permissions.Has(list, required)
}

// allPagesConfirmed: Jede betroffene Seite muss in der Bestätigung genannt
// sein. Zusätzlich genannte Seiten sind unschädlich (sie werden nicht
// gelöscht), etwa wenn zwischen 409 und Bestätigung ein weiterer Admin
// hinzukam. Kommt umgekehrt eine neue Seite hinzu, gibt es erneut 409.
func allPagesConfirmed(pages []meDeletePage, confirmed []uint) bool {
	for _, p := range pages {
		if !slices.Contains(confirmed, p.ID) {
			return false
		}
	}
	return true
}

// deleteAccount löscht bzw. anonymisiert alle Daten des Nutzers innerhalb der
// Transaktion tx. Persönliche Daten ohne Aufbewahrungsgrund werden hart
// gelöscht (Unscoped, kein Soft-Delete), Datensätze anderer Zwecke bleiben mit
// DeletedUserMarker statt der E-Mail erhalten. Die Seiten werden wie in
// DeleteStatusPageHandler gelöscht (Soft-Delete der StatusPage).
func deleteAccount(tx *gorm.DB, user models.User, pages []meDeletePage) error {
	uid := strconv.FormatUint(uint64(user.ID), 10)
	email := user.Email
	m := DeletedUserMarker

	steps := []func() error{
		// vorher (Security-Review) am Ende der Liste:
		// Einladungen an und von diesem Konto löschen und die Benutzerzeile sperren,
		// BEVOR Rollen entfernt werden (gleichzeitiges Annehmen).
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
		func() error { return tx.Where("link_user_id = ?", user.ID).Delete(&models.SAMLLoginState{}).Error },
	}
	// Je Seite zwei eigene Schritte statt einer inneren Schleife: so läuft
	// jeder Fehler durch dieselbe Abbruchstelle unten.
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
		// eigene Lesemarken des Nachrichtencenters (auch ohne FK-Cascade).
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
