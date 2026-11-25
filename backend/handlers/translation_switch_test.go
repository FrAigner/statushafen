package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// removeTranslationSwitch deletes the row the shared fixture stores, so the test
// sees a fresh installation.
func removeTranslationSwitch(t *testing.T, f translationFixture) {
	t.Helper()
	require.NoError(t, f.db.Unscoped().Where("key = ?", "translation.enabled").Delete(&models.SystemSetting{}).Error)
}

// The switch for automatic translation and additional content languages is a
// system setting (translation.enabled). A missing row means "off".
func TestTranslationSwitch_DefaultOffAndToggle(t *testing.T) {
	f := newTranslationFixture(t)
	removeTranslationSwitch(t, f)
	f.r.GET("/api/public/translation/enabled", f.h.GetPublicTranslationEnabledHandler)
	public := func() any {
		w := doStatusPageRequest(f.r, "GET", "/api/public/translation/enabled", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out["enabled"]
	}

	w := doStatusPageRequest(f.r, "GET", "/api/settings/translation", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, false, got["enabled"], "missing row means off")
	assert.Equal(t, false, public())

	out := f.put(t, map[string]any{"enabled": true})
	assert.Equal(t, true, out["enabled"])
	assert.Equal(t, true, public())
	var s models.SystemSetting
	require.NoError(t, f.db.Where("key = ?", "translation.enabled").First(&s).Error)
	assert.Equal(t, "true", s.Value)
	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ? AND target_id = ?", "system_setting.updated", "translation.enabled").First(&audit).Error)
	assert.Equal(t, "enabled", audit.TargetName)

	// Switching off alone is a valid request (not settings_field_required).
	out = f.put(t, map[string]any{"enabled": false})
	assert.Equal(t, false, out["enabled"])
	assert.Equal(t, false, public())

	w = doStatusPageRequest(f.r, "PUT", "/api/settings/translation", map[string]any{"enabled": "yes"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", codeOf(t, w.Body.Bytes()))
}

// A failing write of the switch is reported as 500 and not audited.
func TestTranslationSwitch_SaveError(t *testing.T) {
	f := newTranslationFixture(t)
	removeTranslationSwitch(t, f)
	require.NoError(t, f.db.Exec("CREATE TRIGGER no_setting BEFORE INSERT ON system_settings BEGIN SELECT RAISE(ABORT, 'locked'); END").Error)
	w := doStatusPageRequest(f.r, "PUT", "/api/settings/translation", map[string]any{"enabled": true})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", codeOf(t, w.Body.Bytes()))
	var n int64
	f.db.Model(&models.AuditLog{}).Where("target_id = ?", "translation.enabled").Count(&n)
	assert.Zero(t, n)
}
