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
	if rejectDisabledPasswordLogin(c) {
		return
	}
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameters", "code": "invalid_request"})
		return
	}

	// Lockout per account and source plus delay for distributed guessing (see
	// middleware.AccountThrottle). Checking and counting are one step: the
	// attempt counts as a failed attempt from here on until it succeeds. The check sits
	// before the account lookup and applies equally to known and unknown addresses,
	// so it reveals nothing about the existence of an account. The lockout only applies
	// to the attacker's source: the user from a different address remains
	// zugelassen.
	email := limitEmail(req.Email)
	admission := h.loginThrottle.Admit(email, c.ClientIP())
	if admission.Blocked {
		c.Header("Retry-After", strconv.Itoa(int(math.Ceil(admission.RetryAfter.Seconds()))))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Zu viele Anfragen. Bitte später erneut versuchen.", "code": "rate_limited"})
		return
	}
	if !waitForThrottle(c.Request.Context(), admission.Delay) {
		c.Abort() // client closed the connection
		return
	}

	var user models.User
	// An address above the RFC limit cannot have an account: no lookup.
	err := gorm.ErrRecordNotFound
	if len(req.Email) <= maxEmailLength {
		err = h.DB.Preload("Role").Where("email = ?", req.Email).First(&user).Error
	}
	// For an unknown account, still compute a full hash comparison
	// (empty hash = dummy), otherwise the runtime reveals whether the account exists.
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

	// On a verified custom domain the session is limited to its page, like an
	// OIDC session handed to a custom domain (see login_origin.go).
	origin := h.browserOrigin(c)
	isGlobalAdmin := user.IsGlobalAdmin && origin.pageID == 0
	token, err := middleware.GenerateToken(user.ID, user.Email, user.Role.Name, user.Role.Permissions, isGlobalAdmin,
		middleware.WithTokenVersion(user.TokenVersion), middleware.WithSSOPageID(origin.pageID), middleware.WithCustomDomain(origin.host))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not generate JWT token"})
		return
	}

	// successful password logins were missing from the audit log. The
	// request is authenticated from here on - the actor is the user themselves
	// (also for user_id in the access log).
	c.Set("userID", user.ID)
	c.Set("email", user.Email)
	h.RecordAudit(c, "auth.login", "auth", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email, "")

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		// id was missing (unlike /api/me), as a result the
		// user management did not recognize its own row after a password login.
		"id":                 user.ID,
		"email":              user.Email,
		"role":               user.Role.Name,
		"permissions":        user.Role.Permissions,
		"tag_ids":            tagIDList(h.tagsForUser(user.ID)),
		"is_global_admin":    isGlobalAdmin,
		"preferred_language": user.PreferredLanguage,
		"help_tooltips":      !user.HideHelpTooltips,
	})
}

// waitForThrottle waits the time demanded by the throttle without holding a lock
// and aborts as soon as the client closes the connection (false).
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
	// Safe lookup with the "ok" pattern
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

	// This function is already implemented safely!
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

// UpdateMeRequest: deliberately only the settings of the own account, not a
// full profile PATCH, since there is no real profile management
// yet. Both fields are optional (pointers, so that "not sent" can be distinguished from
// false/""), at least one must be set:
// - preferred_language: UI-Sprache
// - help_tooltips: help tooltips in the admin area on/off
type UpdateMeRequest struct {
	PreferredLanguage *string `json:"preferred_language"`
	HelpTooltips      *bool   `json:"help_tooltips"`
}

// meUpdates validates the request and builds from it the columns to be written and
// the response. Returns an error message for 400 if nothing or something
// invalid was sent.
func meUpdates(req UpdateMeRequest) (map[string]any, gin.H, string) {
	columns := map[string]any{}
	resp := gin.H{}
	if req.PreferredLanguage != nil {
		lang := *req.PreferredLanguage
		// allow only the two languages selectable in the frontend,
		// otherwise arbitrary text ends up in the column.
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

// UpdateMeHandler: PATCH-style endpoint at /api/me for saving the
// settings of the logged-in user (language, help tooltips). Anonymous visitors persist their language choice instead
// client-side in localStorage - no backend endpoint is needed for that.
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

	// targeted update of the sent columns instead of Save() on the
	// whole user. Save writes all columns (and associations) back and
	// can fail on data that has nothing to do with these settings.
	if err := h.DB.Model(&models.User{}).Where("id = ?", user.ID).Updates(columns).Error; err != nil {
		slog.Error("Kontoeinstellungen konnten nicht gespeichert werden", "user_id", user.ID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not save settings", "code": "server_error"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// myPageEntry attaches the permissions that apply on this page to the status page,
// so that the frontend can actually evaluate page-specific rights (UserStatusPageRole)
// instead of only knowing the global role.
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
		// Fetch pages where user has a role, incl. name and permissions of that role -
		// the frontend needs RoleName to show the user their actual role per
		// status page (instead of the possibly outdated global role from the JWT).
		// A page SSO session only sees the page of its IdP
		// (tenant selection in the admin area).
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
