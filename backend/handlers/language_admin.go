package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Stage 1 von 5 des Mehrsprachigkeits-Features: Admin-CRUD für
// den globalen Sprachkatalog (Language) sowie Lesen/Schreiben der pro
// Statusseite aktivierten Sprachen + Default (StatusPageLanguage). Ersetzt
// die vorherige, implizit auf en/de fixierte Sprachannahme
// durch einen echten, offen erweiterbaren Katalog.

// GetLanguagesHandler liefert den kompletten globalen Sprachkatalog. Bewusst
// für jeden eingeloggten User lesbar (siehe Kommentar in main.go) - auch ein
// Seiten-Admin ohne Global-Admin-Rechte muss die verfügbaren Sprachen sehen
// können, um sie für seine Statusseite zu aktivieren.
func (h *Handler) GetLanguagesHandler(c *gin.Context) {
	var languages []models.Language
	if err := h.DB.Order("code asc").Find(&languages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch languages", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, languages)
}

// languageUpsertReq wird für Create UND Update verwendet - Code ist beim
// Update bewusst nicht gebunden ("required" würde sonst jeden Update-Request
// ohne mitgeschicktes Code-Feld ablehnen, obwohl UpdateLanguageHandler den
// Code ohnehin ignoriert (nicht änderbar, siehe Handler-Kommentar)). Die
// Pflichtprüfung für Code findet stattdessen explizit in
// CreateLanguageHandler statt.
type languageUpsertReq struct {
	Code     string `json:"code"`
	Name     string `json:"name" binding:"required"`
	IsActive *bool  `json:"is_active"`
}

// CreateLanguageHandler legt einen neuen Eintrag im globalen Sprachkatalog
// an - global-admin-only, analog zum Tag-Katalog (siehe handlers/tag.go).
func (h *Handler) CreateLanguageHandler(c *gin.Context) {
	var req languageUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	code := strings.ToLower(strings.TrimSpace(req.Code))
	name := strings.TrimSpace(req.Name)
	if name == "" {
		abortInvalidInput(c, codeNameRequired, "name darf nicht leer sein")
		return
	}
	// Format prüfen (vorher nur "nicht leer", Frontend nur maxlength).
	if !languageCodeRegex.MatchString(code) {
		abortInvalidInput(c, codeLanguageCodeInvalid, "code muss ein Sprachcode wie de oder en-us sein")
		return
	}

	var count int64
	if err := h.DB.Model(&models.Language{}).Where("code = ?", code).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check for duplicate language", "code": "server_error"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Eine Sprache mit diesem Code existiert bereits", "code": "language_exists"})
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	// Gelöschte Sprachen bleiben als Soft-Delete-Zeile stehen (Grabstein für
	// den Seed, FK von StatusPageLanguage, vollständiger Unique-Index auf code). Wird
	// derselbe Code neu angelegt, lebt diese Zeile mit dem neuen Namen wieder auf.
	var tomb models.Language
	tombErr := h.DB.Unscoped().Where("code = ? AND deleted_at IS NOT NULL", code).First(&tomb).Error
	if tombErr == nil {
		tomb.Name, tomb.IsActive = name, isActive
		tomb.DeletedAt = gorm.DeletedAt{}
		if err := h.DB.Unscoped().Save(&tomb).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create language", "code": "server_error"})
			return
		}
		h.RecordAudit(c, "language.created", "config", "Language", strconv.FormatUint(uint64(tomb.ID), 10), tomb.Code, "")
		c.JSON(http.StatusCreated, tomb)
		return
	}
	if !errors.Is(tombErr, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check for deleted language", "code": "server_error"})
		return
	}
	lang := models.Language{Code: code, Name: name, IsActive: isActive}
	// GORM ersetzt das Nullwert-false durch default:true; eine inaktiv angelegte
	// Sprache wird deshalb im selben Schritt ausdrücklich deaktiviert (eine
	// Transaktion, damit bei Fehlern keine aktive Zeile übrig bleibt).
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&lang).Error; err != nil {
			return err
		}
		if isActive {
			return nil
		}
		return tx.Model(&lang).Update("is_active", false).Error
	})
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "Eine Sprache mit diesem Code existiert bereits", "code": "language_exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create language", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "language.created", "config", "Language", strconv.FormatUint(uint64(lang.ID), 10), lang.Code, "")
	c.JSON(http.StatusCreated, lang)
}

// UpdateLanguageHandler ändert Name/IsActive eines Katalog-Eintrags. Der Code
// selbst ist bewusst nicht änderbar - er ist der fachliche Schlüssel, über
// den StatusPageLanguage (und künftig lokalisierter Content) referenziert.
func (h *Handler) UpdateLanguageHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid language ID", "code": "invalid_id"})
		return
	}
	var lang models.Language
	if err := h.DB.First(&lang, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Language not found", "code": "language_not_found"})
		return
	}

	var req languageUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		abortInvalidInput(c, codeNameRequired, "name darf nicht leer sein")
		return
	}
	lang.Name = name
	if req.IsActive != nil {
		lang.IsActive = *req.IsActive
	}
	if err := h.DB.Save(&lang).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update language", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "language.updated", "config", "Language", strconv.FormatUint(id, 10), lang.Code, "")
	c.JSON(http.StatusOK, lang)
}

// languageBlocked beendet die Lösch-Transaktion mit einer fachlichen Ablehnung (409).
type languageBlocked struct {
	code  string
	error string
	count int
	pages []string
}

func (b *languageBlocked) Error() string { return b.code }

// DeleteLanguageHandler löscht eine Sprache aus dem globalen Katalog
// (global-admin-only). Regeln, alle in EINER Transaktion geprüft:
//  1. Standardsprache mindestens einer Statusseite: 409 language_is_default mit den
//     Seitennamen (pages) und der Anzahl (count). Eine Seite ohne Standardsprache
//     soll nie entstehen, der Admin stellt die Seite erst um.
//  2. "In Benutzung" heißt: es gibt noch aktive Mail-Vorlagen (NotificationTemplate.
//     Language) mit genau diesem Code; sie würden sonst nie mehr versendet bzw. ließen
//     sich nicht mehr bearbeiten: 409 language_in_use mit count. Nicht dazu zählen die
//     UI-Sprache der Nutzer (User.PreferredLanguage kennt nur de/en, nicht den Katalog)
//     und gespeicherte Übersetzungen von Vorfällen/Komponenten/Ankündigungen.
//  3. Sonst: Zuordnungen StatusPageLanguage (alle nicht Standard, siehe 1) und die
//     Sprache werden soft-gelöscht. Die Zeile bleibt als Grabstein: der Seed
//     (seedLanguageCatalog) prüft Unscoped und legt eine bewusst gelöschte Sprache
//     nicht neu an; CreateLanguageHandler lässt sie bei gleichem Code wieder aufleben.
//
// Bereits gespeicherte Übersetzungen bleiben in der Datenbank, werden aber nicht mehr
// ausgeliefert: der öffentliche Pfad lehnt Sprachen ab, die die Seite nicht aktiviert
// hat (400), und fragt nur Code und Standardsprache der Seite ab.
func (h *Handler) DeleteLanguageHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid language ID", "code": "invalid_id"})
		return
	}
	var lang models.Language
	if err := h.DB.First(&lang, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Language not found", "code": "language_not_found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load language", "code": "server_error"})
		}
		return
	}

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var pageNames []string
		if err := tx.Model(&models.StatusPage{}).
			Joins("JOIN status_page_languages spl ON spl.status_page_id = status_pages.id AND spl.deleted_at IS NULL").
			Where("spl.language_code = ? AND spl.is_default = ?", lang.Code, true).
			Order("status_pages.name").Pluck("status_pages.name", &pageNames).Error; err != nil {
			return err
		}
		if len(pageNames) > 0 {
			return &languageBlocked{code: "language_is_default", error: "Sprache ist Standardsprache einer Statusseite", count: len(pageNames), pages: pageNames}
		}
		var templates int64
		if err := tx.Model(&models.NotificationTemplate{}).Where("language = ?", lang.Code).Count(&templates).Error; err != nil {
			return err
		}
		if templates > 0 {
			return &languageBlocked{code: "language_in_use", error: "Zur Sprache gibt es noch Mail-Vorlagen", count: int(templates)}
		}
		if err := tx.Where("language_code = ?", lang.Code).Delete(&models.StatusPageLanguage{}).Error; err != nil {
			return err
		}
		return tx.Delete(&lang).Error
	})
	var blocked *languageBlocked
	if errors.As(err, &blocked) {
		body := gin.H{"error": blocked.error, "code": blocked.code, "count": blocked.count}
		if blocked.pages != nil {
			body["pages"] = blocked.pages
		}
		c.JSON(http.StatusConflict, body)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete language", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "language.deleted", "config", "Language", strconv.FormatUint(id, 10), lang.Code, "")
	c.Status(http.StatusNoContent)
}

// GetPageLanguagesHandler liefert die für eine Statusseite aktivierten
// Sprachen inkl. Default-Markierung.
func (h *Handler) GetPageLanguagesHandler(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	var pageLanguages []models.StatusPageLanguage
	if err := h.DB.Preload("Language").Where("status_page_id = ?", pageID).Order("language_code asc").Find(&pageLanguages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch page languages", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, pageLanguages)
}

type putPageLanguagesReq struct {
	// Codes: die vollständige neue Menge aktivierter Sprachcodes
	// - kein PATCH/Diff, das UI schickt immer den gewünschten Endzustand,
	// analog zum Muster in UpdateMySubscriptionHandler.
	Codes   []string `json:"codes" binding:"required"`
	Default string   `json:"default" binding:"required"`
}

// PutPageLanguagesHandler ersetzt die komplette Sprachauswahl
// einer Statusseite durch die übergebene Menge + Default. Bewusst als
// Replace statt granularem Add/Remove: verhindert, dass eine Seite
// zwischenzeitlich ganz ohne Default-Sprache oder mit einem Default
// dasteht, der nicht in der aktivierten Menge enthalten ist - beides wird
// hier als atomare Transaktion validiert und durchgesetzt.
func (h *Handler) PutPageLanguagesHandler(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	var req putPageLanguagesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Felder", "code": "invalid_request"})
		return
	}
	if len(req.Codes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mindestens eine Sprache muss aktiviert sein", "code": "language_none_enabled"})
		return
	}
	defaultCode := strings.ToLower(strings.TrimSpace(req.Default))
	codes := make([]string, 0, len(req.Codes))
	hasDefault := false
	for _, raw := range req.Codes {
		code := strings.ToLower(strings.TrimSpace(raw))
		if code == "" {
			continue
		}
		codes = append(codes, code)
		if code == defaultCode {
			hasDefault = true
		}
	}
	if !hasDefault {
		c.JSON(http.StatusBadRequest, gin.H{"error": "default muss Teil von codes sein", "code": "language_default_not_in_codes"})
		return
	}

	var validCount int64
	if err := h.DB.Model(&models.Language{}).Where("code IN ? AND is_active = ?", codes, true).Count(&validCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate languages", "code": "server_error"})
		return
	}
	if int(validCount) != len(codes) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mindestens ein Sprachcode ist unbekannt oder deaktiviert", "code": "language_codes_unknown"})
		return
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("status_page_id = ?", pageID).Delete(&models.StatusPageLanguage{}).Error; err != nil {
			return err
		}
		for _, code := range codes {
			entry := models.StatusPageLanguage{StatusPageID: uint(pageID), LanguageCode: code, IsDefault: code == defaultCode}
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save page languages", "code": "server_error"})
		return
	}

	var pageLanguages []models.StatusPageLanguage
	if err := h.DB.Where("status_page_id = ?", pageID).Order("language_code asc").Find(&pageLanguages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch page languages", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "page_languages.updated", "config", "StatusPageLanguage", strconv.FormatUint(uint64(pageID), 10), strings.Join(codes, ","), "")
	c.JSON(http.StatusOK, pageLanguages)
}
