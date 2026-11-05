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

// readSystemSettingBool wie getSystemSettingBool, unterscheidet aber "nicht
// gesetzt" (Default) von einem Lesefehler: ein Fehler wird
// zurückgegeben statt still als Default zu gelten.
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

// Systemeinstellungen: globale, erweiterbare Key/Value-Schalter.
// Erster Schalter ist die Self-Service-Bestellung (öffentliche Route /order,
//), zweiter der globale E-Mail-Versand. Konvention:
// Ein fehlender Key bedeutet den jeweiligen Default - für beide Schalter "an",
// damit bestehende Deployments unverändert bleiben und erst ein explizites
// "false" die Funktion abschaltet.

const (
	// settingOrdersEnabled steuert, ob die Self-Service-Bestellung aktiv ist.
	// Fehlt der Key, gilt der Default "an" (siehe getSystemSettingBool).
	settingOrdersEnabled = "orders.enabled"
	// settingEmailEnabled schaltet den gesamten E-Mail-Versand.
	// Fehlt der Key, gilt "an"; ausgewertet wird er zentral in deliverMail
	// (smtp.go), der einzigen Stelle mit smtp.SendMail.
	settingEmailEnabled = "email.enabled"
	// settingSelfMonitorEnabled schaltet die automatische Selbstüberwachung der
	// Systemseite. Fehlt der Key, gilt "aus"
	// (Betreiber-Entscheidung): nur ein ausdrücklich gesetztes "an"
	// schaltet sie ein.
	settingSelfMonitorEnabled = "selfmonitor.enabled"
	// selfMonitorEnabledDefault ist die einzige Quelle dieses Defaults (Schalter-
	// Tabelle und Monitor).
	selfMonitorEnabledDefault = false
)

// boolSetting beschreibt einen über GET/PUT /api/settings verwalteten
// bool-Schalter: Key in der SystemSetting-Tabelle, Feldname im JSON-Vertrag
// und Default bei fehlendem Key. adminBoolSettings ist die einzige
// Stelle, an der ein neuer Schalter eingetragen wird - GET, PUT und Audit
// laufen generisch darüber, statt pro Schalter kopiert zu werden.
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

// getSystemSettingBool liest einen bool-Wert aus der SystemSetting-Tabelle.
// Fehlt der Key (oder tritt ein DB-Fehler auf), wird der übergebene Default
// zurückgegeben - so bleibt "Key fehlt ⇒ Default" die einzige
// Quelle der Wahrheit und bestehende Deployments ohne Zeile verhalten sich
// unverändert. Als "true" gilt ausschließlich der exakte Wert "true".
func (h *Handler) getSystemSettingBool(key string, def bool) bool {
	var setting models.SystemSetting
	if err := h.DB.Where("key = ?", key).First(&setting).Error; err != nil {
		return def
	}
	return setting.Value == "true"
}

// setSystemSettingBool legt den Key an oder aktualisiert ihn (Upsert per
// ON CONFLICT auf dem uniqueIndex key). Der Wert wird kanonisch als
// "true"/"false" gespeichert, damit getSystemSettingBool deterministisch
// vergleichen kann. Bewusst ein einzelnes Statement statt First+Save/Create -
// das vermeidet eine Race zwischen Prüfung und Insert und einen toten
// Fehler-Zweig für den ohnehin nie auftretenden Nicht-NotFound-Fehler.
func (h *Handler) setSystemSettingBool(key string, value bool) error {
	return h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&models.SystemSetting{Key: key, Value: strconv.FormatBool(value)}).Error
}

// currentSettings liefert alle Admin-Schalter mit ihrem aktuellen Wert.
func (h *Handler) currentSettings() gin.H {
	out := gin.H{}
	for _, s := range adminBoolSettings {
		out[s.field] = h.getSystemSettingBool(s.key, s.def)
	}
	return out
}

// GetSettingsHandler liefert die (für die Admin-Oberfläche relevanten) globalen
// Systemeinstellungen (orders_enabled, email_enabled). Die Antwort ist bewusst
// ein Objekt (nicht ein nackter Bool), damit weitere Schalter additiv
// dazukommen können, ohne die Vertragsform zu brechen.
func (h *Handler) GetSettingsHandler(c *gin.Context) {
	c.JSON(http.StatusOK, h.currentSettings())
}

// UpdateSettingsHandler setzt einen oder mehrere Schalter. Jedes
// Feld ist optional (Pointer, damit "fehlt" von explizit false unterscheidbar
// bleibt); gespeichert und auditiert werden nur die gesetzten Felder. Fehlen
// alle bekannten Felder, wird der Request wie bisher mit 400 abgelehnt. Die
// Antwort enthält den vollständigen aktuellen Stand aller Schalter.
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

// GetPublicOrdersEnabledHandler meldet ohne Auth, ob die Self-Service-Bestellung
// aktiv ist (Landing-CTA und /order-Seite blenden sich danach aus). Vorbild:
// GetPublicUiOverridesHandler (ui_override.go).
func (h *Handler) GetPublicOrdersEnabledHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": h.getSystemSettingBool(settingOrdersEnabled, true)})
}
