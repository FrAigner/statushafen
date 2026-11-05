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

// Wiederholte Literale (go:S1192).
const preloadUserStatusPageRolesRole = "UserStatusPageRoles.Role"

// userResponse hängt die zugewiesenen Tags an (Ersatz für das
// frühere AllowedCountries-Feld) - analog zu IncidentResponse/Components in
// incident.go, damit das Admin-Frontend die aktuelle Tag-Zuweisung anzeigen
// und im Bearbeiten-Formular vorbefüllen kann.
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
		Email         string          `json:"email" binding:"required"`
		Password      string          `json:"password" binding:"required"`
		TagIDs        []uint          `json:"tag_ids"`
		IsGlobalAdmin bool            `json:"is_global_admin"`
		PageRoles     []PageRoleInput `json:"page_roles"`
	}
	var req CreateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}

	if rejectInvalidPassword(c, req.Password) {
		return
	}

	var existing models.User
	if err := h.DB.Where("email = ?", req.Email).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User with this email already exists", "code": "email_taken"})
		return
	}

	// User-Tags sind bewusst nur global gültig, nie an eine Statusseite
	// gebunden - AllowedCountries war schon immer seitenübergreifend.
	tags, err := h.resolveGlobalTagsByIDs(req.TagIDs)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}

	if !h.pageRolesAssignable(0, req.PageRoles) {
		abortGlobalAdminRoleNotAssignable(c)
		return
	}

	hash, err := hashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user", "code": "server_error"})
		return
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
		if isUniqueViolation(err) { // gleiche Antwort wie die Vorabprüfung
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

// replaceUserPageRoles löscht die bestehenden UserStatusPageRole-Zeilen eines
// Users (Hard Delete, um Duplikate/unübersichtliche DB zu vermeiden) und legt
// die neu übergebenen an. Ausgelagert aus UpdateUserHandler, damit dessen
// kognitive Komplexität die SonarCloud-Grenze (go:S3776) nicht überschreitet.
// Das Ergebnis meldet, ob sich die Zuordnung (Seite, Rolle) tatsächlich geändert hat.
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
	// Seiten, auf denen die Mitgliedschaft endet, verlieren auch
	// die Seiten-SSO-Verknüpfung des Kontos.
	var dropped []models.UserStatusPageRole
	for _, b := range before {
		if !slices.ContainsFunc(pageRoles, func(pr PageRoleInput) bool { return pr.RoleID != 0 && pr.StatusPageID == b.StatusPageID }) {
			dropped = append(dropped, b)
		}
	}
	if err := deleteSSOIdentitiesForRoles(tx, dropped); err != nil {
		return false, err
	}
	for _, pr := range pageRoles {
		if pr.RoleID == 0 {
			continue
		}
		if err := tx.Create(&models.UserStatusPageRole{
			UserID:       userID,
			StatusPageID: pr.StatusPageID,
			RoleID:       pr.RoleID,
		}).Error; err != nil {
			return false, err
		}
	}
	return changed, nil
}

// pageRolesDiffer vergleicht die bestehenden Seitenrollen mit der neuen
// Zuordnung; Einträge ohne Rolle (RoleID 0) zählen als nicht vorhanden.
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

// bumpTokenVersion erhöht users.token_version und macht damit alle bisher
// ausgestellten Login-Tokens des Kontos ungültig (siehe middleware.checkSession).
// UpdateColumn lässt updated_at unberührt und löst keine Hooks aus.
func bumpTokenVersion(tx *gorm.DB, userID uint) error {
	return tx.Model(&models.User{}).Where("id = ?", userID).
		UpdateColumn("token_version", gorm.Expr("token_version + 1")).Error
}

// applyUserUpdate persistiert die Felder-Updates, die Seiten-Rollen und die
// Tags eines Users in einer Transaktion.
// adminChanged: Global-Admin-Flag wurde geändert. Passwortwechsel und geänderte
// Global-Admin-Rechte widerrufen die Tokens des Kontos. Seitenrollen nicht: sie
// werden bei jeder Anfrage aus der Datenbank gelesen und wirken sofort.
func (h *Handler) applyUserUpdate(user *models.User, req UpdateUserReq, tags []models.Tag, adminChanged bool) error {
	return h.DB.Transaction(func(tx *gorm.DB) error {
		// Benutzerzeile zuerst sperren; ein gleichzeitig gelöschtes
		// Konto ergibt ErrRecordNotFound (404) statt einer Rolle am Konto.
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
	// leer = Passwort unverändert, sonst gilt die Mindestlänge.
	if req.Password != "" && rejectInvalidPassword(c, req.Password) {
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
	// sich selbst bzw. dem letzten Global Admin die Rechte zu
	// entziehen, sperrte die Plattformverwaltung aus.
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
		if errors.Is(err, gorm.ErrRecordNotFound) { // Konto inzwischen gelöscht
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
	// sicherheitsrelevante Teiländerungen als eigene Aktionen,
	// damit sie im Audit-Log filterbar sind statt in user.updated aufzugehen.
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

	// Selbstlöschung und Löschen des letzten Global Admins mit
	// 409 und Fehlercode statt 400 ohne Code (und per ID statt per E-Mail).
	if code := h.adminChangeConflict(c, user, true, user.IsGlobalAdmin); code != "" {
		abortAdminChangeConflict(c, code)
		return
	}

	// Seitenrollen des Benutzers mit entfernen (Soft-Delete löst kein
	// FK-OnDelete aus), atomar mit dem Löschen des Benutzers.
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		// Einladungen weg und Benutzerzeile gesperrt, BEVOR Rollen
		// entfernt werden (gleichzeitiges Annehmen, siehe lockAccountForDeletion).
		if err := lockAccountForDeletion(tx, user); err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", user.ID).Delete(&models.UserStatusPageRole{}).Error; err != nil {
			return err
		}
		// Tags und SSO-Verknüpfungen räumt – wie me_delete – auch der
		// Admin-Pfad auf, sonst bleiben sie als Waisen stehen.
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
