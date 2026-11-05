package handlers

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) LoginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameters", "code": "invalid_request"})
		return
	}

	// Sperre je Konto und Quelle plus Verzögerung bei verteiltem Raten (siehe
	// middleware.AccountThrottle). Prüfen und Zählen sind ein Schritt: Der
	// Versuch zählt ab hier als Fehlversuch, bis er gelingt. Die Prüfung steht
	// vor der Kontosuche und gilt für bekannte wie unbekannte Adressen gleich,
	// sie verrät also nichts über die Existenz eines Kontos. Die Sperre gilt nur
	// für die Quelle des Angreifers: Der Nutzer von einer anderen Adresse bleibt
	// zugelassen.
	email := limitEmail(req.Email)
	admission := h.loginThrottle.Admit(email, c.ClientIP())
	if admission.Blocked {
		c.Header("Retry-After", strconv.Itoa(int(math.Ceil(admission.RetryAfter.Seconds()))))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Zu viele Anfragen. Bitte später erneut versuchen.", "code": "rate_limited"})
		return
	}
	if !waitForThrottle(c.Request.Context(), admission.Delay) {
		c.Abort() // Client hat die Verbindung geschlossen
		return
	}

	var user models.User
	// Eine Adresse über der RFC-Grenze kann kein Konto haben: keine Suche.
	err := gorm.ErrRecordNotFound
	if len(req.Email) <= maxEmailLength {
		err = h.DB.Preload("Role").Where("email = ?", req.Email).First(&user).Error
	}
	// Bei unbekanntem Konto trotzdem einen vollen Hash-Vergleich rechnen
	// (leerer Hash = Dummy), sonst verrät die Laufzeit, ob es das Konto gibt.
	hash := ""
	if err == nil {
		hash = user.PasswordHash
	}
	if !database.CheckPasswordConstantTime(req.Password, hash) {
		h.RecordAudit(c, "auth.login_failed", "auth", "User", "", email, "")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password", "code": "invalid_credentials"})
		return
	}
	admission.Succeeded()

	token, err := middleware.GenerateToken(user.ID, user.Email, user.Role.Name, user.Role.Permissions, user.IsGlobalAdmin, middleware.WithTokenVersion(user.TokenVersion))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate JWT token"})
		return
	}

	// erfolgreiche Passwort-Logins fehlten im Audit-Log. Der
	// Request ist ab hier authentifiziert - Akteur ist der User selbst
	// (auch für user_id im Access-Log).
	c.Set("userID", user.ID)
	c.Set("email", user.Email)
	h.RecordAudit(c, "auth.login", "auth", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email, "")

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		// id fehlte (anders als bei /api/me), dadurch erkannte die
		// Benutzerverwaltung nach einem Passwort-Login die eigene Zeile nicht.
		"id":                 user.ID,
		"email":              user.Email,
		"role":               user.Role.Name,
		"permissions":        user.Role.Permissions,
		"tag_ids":            tagIDList(h.tagsForUser(user.ID)),
		"is_global_admin":    user.IsGlobalAdmin,
		"preferred_language": user.PreferredLanguage,
		"help_tooltips":      !user.HideHelpTooltips,
	})
}

// waitForThrottle wartet die vom Throttle verlangte Zeit, ohne ein Schloss zu
// halten, und bricht ab, sobald der Client die Verbindung schließt (false).
func waitForThrottle(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (h *Handler) MeHandler(c *gin.Context) {
	// Sicheres Auslesen mit "ok" pattern
	var userID uint
	if uid, exists := c.Get("userID"); exists && uid != nil {
		if val, ok := uid.(uint); ok {
			userID = val
		}
	}

	var role string
	if r, exists := c.Get("role"); exists && r != nil {
		if val, ok := r.(string); ok {
			role = val
		}
	}

	var permissions string
	if p, exists := c.Get("permissions"); exists && p != nil {
		if val, ok := p.(string); ok {
			permissions = val
		}
	}

	// Diese Funktion ist bereits sicher implementiert!
	email := getEmail(c)

	var user models.User
	h.DB.First(&user, userID)

	c.JSON(http.StatusOK, gin.H{
		"id":                 userID,
		"email":              email,
		"role":               role,
		"permissions":        permissions,
		"tag_ids":            tagIDList(h.tagsForUser(userID)),
		"is_global_admin":    middleware.EffectiveGlobalAdmin(c, user),
		"preferred_language": user.PreferredLanguage,
		"help_tooltips":      !user.HideHelpTooltips,
	})
}

// UpdateMeRequest: bewusst nur die Einstellungen des eigenen Kontos, kein
// voller Profil-PATCH, da es noch keine echte Profilverwaltung
// gibt. Beide Felder sind optional (Zeiger, damit "nicht gesendet" von
// false/"" unterscheidbar ist), mindestens eines muss gesetzt sein:
// - preferred_language: UI-Sprache
// - help_tooltips: Hilfe-Tooltips im Admin-Bereich an/aus
type UpdateMeRequest struct {
	PreferredLanguage *string `json:"preferred_language"`
	HelpTooltips      *bool   `json:"help_tooltips"`
}

// meUpdates prüft den Request und baut daraus die zu schreibenden Spalten und
// die Antwort. Liefert eine Fehlermeldung für 400, wenn nichts oder etwas
// Ungültiges gesendet wurde.
func meUpdates(req UpdateMeRequest) (map[string]any, gin.H, string) {
	columns := map[string]any{}
	resp := gin.H{}
	if req.PreferredLanguage != nil {
		lang := *req.PreferredLanguage
		// nur die zwei im Frontend wählbaren Sprachen zulassen,
		// sonst landet beliebiger Text in der Spalte.
		if lang != "de" && lang != "en" {
			return nil, nil, "Unsupported language, expected de or en"
		}
		columns["preferred_language"] = lang
		resp["preferred_language"] = lang
	}
	if req.HelpTooltips != nil {
		columns["hide_help_tooltips"] = !*req.HelpTooltips
		resp["help_tooltips"] = *req.HelpTooltips
	}
	if len(columns) == 0 {
		return nil, nil, "Invalid request parameters"
	}
	return columns, resp, ""
}

// UpdateMeHandler: PATCH-artiger Endpunkt an /api/me zum Speichern der
// Einstellungen des eingeloggten Users (Sprache, Hilfe-Tooltips). Anonyme Besucher persistieren ihre Sprachwahl stattdessen
// client-seitig im localStorage - dafür ist kein Backend-Endpunkt nötig.
func (h *Handler) UpdateMeHandler(c *gin.Context) {
	userIDVal, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized", "code": "unauthorized"})
		return
	}
	userID, ok := userIDVal.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session", "code": "server_error"})
		return
	}

	var req UpdateMeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameters", "code": "invalid_request"})
		return
	}
	columns, resp, problem := meUpdates(req)
	if problem != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": problem, "code": "invalid_request"})
		return
	}

	var user models.User
	if err := h.DB.Select("id").First(&user, userID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found", "code": "unauthorized"})
		return
	}

	// gezieltes Update der gesendeten Spalten statt Save() des
	// ganzen Users. Save schreibt alle Spalten (und Assoziationen) zurück und
	// kann an Daten scheitern, die mit diesen Einstellungen nichts zu tun haben.
	if err := h.DB.Model(&models.User{}).Where("id = ?", user.ID).Updates(columns).Error; err != nil {
		slog.Error("Kontoeinstellungen konnten nicht gespeichert werden", "user_id", user.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not save settings", "code": "server_error"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// myPageEntry hängt die auf dieser Seite geltenden Permissions an die Statusseite an,
// damit das Frontend seitenbezogene Rechte (UserStatusPageRole) tatsächlich auswerten
// kann, statt nur die globale Rolle zu kennen.
type myPageEntry struct {
	models.StatusPage
	Permissions string `json:"Permissions"`
	RoleName    string `json:"RoleName"`
}

func (h *Handler) GetMyPagesHandler(c *gin.Context) {
	userIDVal, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized", "code": "unauthorized"})
		return
	}
	userID, ok := userIDVal.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session", "code": "server_error"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found", "code": "unauthorized"})
		return
	}

	pages := []myPageEntry{}
	if middleware.EffectiveGlobalAdmin(c, user) {
		var all []models.StatusPage
		h.DB.Find(&all)
		for _, p := range all {
			pages = append(pages, myPageEntry{StatusPage: p, RoleName: "Global Admin"})
		}
	} else {
		// Fetch pages where user has a role, inkl. Name und Permissions dieser Rolle –
		// das Frontend braucht RoleName, um dem Nutzer seine tatsächliche Rolle je
		// Statusseite anzuzeigen (statt der u.U. veralteten globalen Rolle aus dem JWT).
		// Eine Seiten-SSO-Sitzung sieht nur die Seite ihres IdP
		// (Tenant-Auswahl im Admin-Bereich).
		q := h.DB.Preload("StatusPage").Preload("Role").Where("user_id = ?", userID)
		if scope := middleware.SSOPageID(c); scope != 0 {
			q = q.Where("status_page_id = ?", scope)
		}
		var roles []models.UserStatusPageRole
		q.Find(&roles)
		for _, r := range roles {
			pages = append(pages, myPageEntry{StatusPage: r.StatusPage, Permissions: r.Role.Permissions, RoleName: r.Role.Name})
		}
	}

	c.JSON(http.StatusOK, pages)
}
