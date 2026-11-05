package handlers

import (
	"net/http"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Fehlercodes der Benutzerverwaltung. Das Frontend übersetzt sie
// über users.errors.<code>; die englische "error"-Meldung bleibt für API-Nutzer.
const (
	codeGlobalAdminRoleNotAssignable = "global_admin_role_not_assignable"
	codeCannotDeleteSelf             = "cannot_delete_self"
	codeCannotDemoteSelf             = "cannot_demote_self"
	codeLastGlobalAdmin              = "last_global_admin"
)

// globalOnlyRoleIDs liefert die IDs der übergebenen Rollen, die nicht als
// Seitenrolle vergeben werden dürfen (models.Role.GlobalOnly).
// O2: Ein DB-Fehler kommt als Fehler zurück; die Aufrufer sperren
// dann (fail closed), statt die Rolle als "vergebbar" durchzulassen.
func (h *Handler) globalOnlyRoleIDs(db *gorm.DB, roleIDs []uint) (map[uint]bool, error) {
	out := map[uint]bool{}
	if len(roleIDs) == 0 {
		return out, nil
	}
	var roles []models.Role
	if err := db.Where("id IN ? AND name = ?", roleIDs, models.GlobalAdminRoleName).Find(&roles).Error; err != nil {
		return nil, err
	}
	for _, r := range roles {
		out[r.ID] = true
	}
	return out, nil
}

// pageRolesAssignable prüft, ob keine der Seitenrollen die
// globale Rolle "Global Admin" neu vergibt. Eine bereits bestehende Zuordnung
// derselben Rolle auf derselben Seite bleibt erlaubt: Sie gibt dem Nutzer nur
// die Permissions der Rolle auf dieser Seite, und ohne diese Toleranz ließe
// sich ein Altbestand-Nutzer gar nicht mehr speichern, ohne ihm Rechte zu
// entziehen. Deshalb ist auch keine Datenmigration nötig: Alte Zeilen
// behalten ihre Wirkung, nur neue entstehen nicht mehr.
func (h *Handler) pageRolesAssignable(userID uint, pageRoles []PageRoleInput) bool {
	ids := make([]uint, 0, len(pageRoles))
	for _, pr := range pageRoles {
		ids = append(ids, pr.RoleID)
	}
	globalOnly, err := h.globalOnlyRoleIDs(h.DB, ids)
	if err != nil {
		return false // O2: fail closed
	}
	for _, pr := range pageRoles {
		if !globalOnly[pr.RoleID] {
			continue
		}
		var n int64
		h.DB.Model(&models.UserStatusPageRole{}).
			Where("user_id = ? AND status_page_id = ? AND role_id = ?", userID, pr.StatusPageID, pr.RoleID).
			Count(&n)
		if n == 0 {
			return false
		}
	}
	return true
}

// abortGlobalAdminRoleNotAssignable antwortet mit 400 und Fehlercode.
func abortGlobalAdminRoleNotAssignable(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error": "The Global Admin role cannot be assigned per status page; use is_global_admin instead",
		"code":  codeGlobalAdminRoleNotAssignable,
	})
}

// adminChangeConflict prüft, ob ein Löschen (deleting) bzw. ein
// Schritt, der dem Ziel Global Admin nimmt (removesAdmin: Löschen oder
// Herabstufen eines Global Admins), das eigene Konto oder den letzten Global
// Admin träfe. Liefert den Fehlercode oder "".
// Selbstlöschung geht bewusst nur über DELETE /api/me (mit Bestätigung und
// Behandlung der Seiten, deren einziger Admin man ist).
func (h *Handler) adminChangeConflict(c *gin.Context, target models.User, deleting, removesAdmin bool) string {
	isSelf := target.ID == getUserID(c)
	if deleting && isSelf {
		return codeCannotDeleteSelf
	}
	if !removesAdmin {
		return ""
	}
	if isSelf {
		return codeCannotDemoteSelf
	}
	if !h.otherGlobalAdminExists(target.ID) {
		return codeLastGlobalAdmin
	}
	return ""
}

// abortAdminChangeConflict antwortet mit 409 und Fehlercode (409 statt 403,
// weil der Aufrufer berechtigt ist und nur der aktuelle Zustand den Schritt
// verbietet; apiFetch reicht bei 409 außerdem den Body samt Code weiter).
func abortAdminChangeConflict(c *gin.Context, code string) {
	msg := map[string]string{
		codeCannotDeleteSelf: "You cannot delete your own account here; use your profile instead",
		codeCannotDemoteSelf: "You cannot remove your own Global Admin rights",
		codeLastGlobalAdmin:  "This is the last Global Admin; appoint another Global Admin first",
	}[code]
	c.JSON(http.StatusConflict, gin.H{"error": msg, "code": code})
}
