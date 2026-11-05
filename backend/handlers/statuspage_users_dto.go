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

// Antworten der Seitenbenutzer-API tragen nur, was die Verwaltung
// braucht. Die Schlüssel behalten die bisherige Schreibweise (ID, User.Email,
// Role.Name), damit das Frontend unverändert bleibt.
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

// respondPageUserAssigned: per E-Mail hinzugefügt -> neutrale Antwort (verrät
// nichts über das Konto), sonst die Zuordnung ohne Kontodaten.
func respondPageUserAssigned(c *gin.Context, status int, e models.UserStatusPageRole, byEmail bool) {
	if byEmail {
		c.JSON(http.StatusOK, gin.H{"message": pageUserEmailAcceptedMsg})
		return
	}
	c.JSON(status, pageUserDTO{ID: e.ID, UserID: e.UserID, StatusPageID: e.StatusPageID, RoleID: e.RoleID})
}

// isEffectiveGlobalAdmin: frischer DB-Blick auf IsGlobalAdmin (Seiten-SSO-
// Sitzungen zählen nie).
func (h *Handler) isEffectiveGlobalAdmin(c *gin.Context) bool {
	var u models.User
	if err := h.DB.First(&u, getUserID(c)).Error; err != nil {
		return false
	}
	return middleware.EffectiveGlobalAdmin(c, u)
}

// actorPagePermissions: Rechte-Liste des angemeldeten Nutzers auf der Seite
// (leer, wenn er dort keine Rolle hat).
func (h *Handler) actorPagePermissions(c *gin.Context, pageID uint) string {
	var ur models.UserStatusPageRole
	if err := h.DB.Preload("Role").Where(whereUserIDAndStatusPageID, getUserID(c), pageID).First(&ur).Error; err != nil {
		return ""
	}
	return ur.Role.Permissions
}

// resolvePageUserTarget bestimmt das Zielkonto. Rückgabe: Konto,
// gefunden, Status != 0 wenn die Antwort schon gesendet wurde.
//   - Global Admin: user_id (unbekannt -> 404 user_not_found) oder E-Mail.
//   - Sonst: user_id nur für bestehende Mitglieder dieser Seite; jedes andere
//     Konto nur per E-Mail. Ohne beides -> 400 (für bekannte und unbekannte
//     ID gleich, damit sich Konten nicht abzählen lassen).
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
		// Ein DB-Fehler ist keine "unbekannte Adresse" (sonst entstünde
		// eine Einladung ohne Mail und der Fehler bliebe unsichtbar): fail closed.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
		return u, false, http.StatusInternalServerError
	}
	if lookupErr != nil && isGA {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found", "code": "user_not_found"})
		return u, false, http.StatusNotFound
	}
	// Timing-Orakel: Die drei Folgeabfragen laufen für gefundene
	// UND unbekannte Adressen (bei unbekannter Adresse mit ID 0, die nichts
	// findet) und alle unbedingt (kein Kurzschluss durch ||), damit die Zahl der
	// Abfragen nicht verrät, ob ein Konto existiert. Die Laufzeit-Gleichheit
	// bleibt eine Annäherung (Cache, Zeilenzahl), die Abfragezahl ist gleich.
	otherPageRole := h.hasRoleOnOtherPage(u.ID, pageID)
	otherPageSSO := h.hasSSOIdentityOnOtherPage(u.ID, pageID)
	var memberN int64
	if err := h.DB.Model(&models.UserStatusPageRole{}).Where(whereUserIDAndStatusPageID, u.ID, pageID).Count(&memberN).Error; err != nil {
		// Review-Fund 5: kein "memberN = 0" bei DB-Fehler (fail closed).
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
		return u, false, http.StatusInternalServerError
	}
	if lookupErr != nil {
		return u, false, 0
	}
	// Global-Admin-Konten, Konten mit (altem oder Seiten-)SSO-Verknüpfung oder Rolle auf einer anderen Seite
	// hängt nur ein Global Admin an (sonst verlöre das Opfer die SSO-Anmeldung
	// seiner Seite). Wie "unbekannt" behandelt: gleiche Antwort, nichts geschrieben.
	// Mitglieder dieser Seite bleiben umstufbar.
	if !isGA && (u.IsGlobalAdmin || u.SSOProvider != "" || u.SSOSubject != "" || otherPageRole || otherPageSSO) && memberN == 0 {
		return models.User{}, false, 0
	}
	return u, true, 0
}

// roleCoveredByActor: die Rechte der Rolle liegen vollständig in den Rechten
// des Aufrufers. Eine fehlende (z. B. soft-gelöschte) Rolle gilt nie als
// abgedeckt; nur Global Admins ändern solche Mitglieder.
func roleCoveredByActor(actorPerms string, r models.Role) bool {
	return r.ID != 0 && permissions.Covers(actorPerms, r.Permissions)
}

func abortRoleExceedsOwn(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"error": "The role grants more rights than you have on this page", "code": "role_exceeds_own_permissions"})
}
