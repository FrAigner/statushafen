package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// readSystemSettingBool is like getSystemSettingBool, but distinguishes "not
// set" (default) from a read error: an error is
// returned instead of silently counting as the default.
func (h *Handler) readSystemSettingBool(key string, def bool) (bool, error) {
	var setting models.SystemSetting
	if err := h.DB.Where("key = ?", key).First(&setting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return def, nil
		}
		return def, err
	}
	return setting.Value == "true", nil
}

// System settings: global, extensible key/value switches.
// The first switch is self-service ordering (public route /order,
//), the second is global email sending. Convention:
// A missing key means the respective default - "on" for both switches,
// so that existing deployments remain unchanged and only an explicit
// "false" turns the feature off.

const (
	// settingOrdersEnabled controls whether self-service ordering is active.
	// If the key is missing, the default "on" applies (see getSystemSettingBool).
	settingOrdersEnabled = "orders.enabled"
	// settingEmailEnabled switches all email sending.
	// If the key is missing, "on" applies; it is evaluated centrally in deliverMail
	// (smtp.go), the only place with smtp.SendMail.
	settingEmailEnabled = "email.enabled"
	// settingSelfMonitorEnabled switches the automatic self-monitoring of the
	// system page. If the key is missing, "off" applies
	// (operator decision): only an explicitly set "on"
	// turns it on.
	settingSelfMonitorEnabled = "selfmonitor.enabled"
	// selfMonitorEnabledDefault is the single source of this default (switch
	// table and monitor).
	selfMonitorEnabledDefault = false
)

// boolSetting describes a bool switch managed via GET/PUT /api/settings:
// key in the SystemSetting table, field name in the JSON contract
// and default for a missing key. adminBoolSettings is the only
// place where a new switch is registered - GET, PUT and audit
// run generically over it instead of being copied per switch.
type boolSetting struct {
	key   string
	field string
	def   bool
}

var adminBoolSettings = []boolSetting{
	{key: settingOrdersEnabled, field: "orders_enabled", def: true},
	{key: settingEmailEnabled, field: "email_enabled", def: true},
	{key: settingSelfMonitorEnabled, field: "selfmonitor_enabled", def: selfMonitorEnabledDefault},
}

// getSystemSettingBool reads a bool value from the SystemSetting table.
// If the key is missing (or a DB error occurs), the passed default is
// returned - so "key missing => default" remains the single
// source of truth and existing deployments without a row behave
// unchanged. Only the exact value "true" counts as "true".
func (h *Handler) getSystemSettingBool(key string, def bool) bool {
	var setting models.SystemSetting
	if err := h.DB.Where("key = ?", key).First(&setting).Error; err != nil {
		return def
	}
	return setting.Value == "true"
}

// setSystemSettingBool creates the key or updates it (upsert via
// ON CONFLICT on the uniqueIndex key). The value is stored canonically as
// "true"/"false" so that getSystemSettingBool can compare deterministically.
// Deliberately a single statement instead of First+Save/Create -
// this avoids a race between check and insert and a dead
// error branch for the non-NotFound error that never occurs anyway.
func (h *Handler) setSystemSettingBool(key string, value bool) error {
	return h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&models.SystemSetting{Key: key, Value: strconv.FormatBool(value)}).Error
}

// currentSettings returns all admin switches with their current value.
func (h *Handler) currentSettings() gin.H {
	out := gin.H{}
	for _, s := range adminBoolSettings {
		out[s.field] = h.getSystemSettingBool(s.key, s.def)
	}
	return out
}

// GetSettingsHandler returns the global system settings (relevant for the
// admin UI) (orders_enabled, email_enabled). The response is deliberately
// an object (not a bare bool) so that further switches can be added
// additively without breaking the contract shape.
func (h *Handler) GetSettingsHandler(c *gin.Context) {
	c.JSON(http.StatusOK, h.currentSettings())
}

// UpdateSettingsHandler sets one or more switches. Each
// field is optional (pointer, so that "missing" remains distinguishable from explicit false);
// only the fields that are set are stored and audited. If
// all known fields are missing, the request is rejected with 400 as before. The
// response contains the complete current state of all switches.
func (h *Handler) UpdateSettingsHandler(c *gin.Context) {
	var req map[string]*bool
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	updated := 0
	for _, s := range adminBoolSettings {
		v := req[s.field]
		if v == nil {
			continue
		}
		if err := h.setSystemSettingBool(s.key, *v); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings", "code": "server_error"})
			return
		}
		h.RecordAudit(c, "system_setting.updated", "config", "SystemSetting", s.key, s.field, strconv.FormatBool(*v))
		updated++
	}
	if updated == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mindestens ein Feld (orders_enabled, email_enabled, selfmonitor_enabled) ist erforderlich", "code": "settings_field_required"})
		return
	}
	c.JSON(http.StatusOK, h.currentSettings())
}

// GetPublicOrdersEnabledHandler reports without auth whether self-service ordering
// is active (landing CTA and /order page hide themselves accordingly). Model:
// GetPublicUiOverridesHandler (ui_override.go).
func (h *Handler) GetPublicOrdersEnabledHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": h.getSystemSettingBool(settingOrdersEnabled, true)})
}
