package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/translation"

	"github.com/gin-gonic/gin"
)

// Automatic translation: the editor has the title and
// message suggested from the page's default language into another language.
// The result is never stored, only delivered as a draft into the
// form.
//
// Configuration (system settings, global admin only):
//   - translation.provider: "" (default = LibreTranslate from the environment),
//     "libretranslate" or "deepl"
//   - translation.url: LibreTranslate address (https only, public, SSRF-checked)
//   - translation.api_key: key (optional for LibreTranslate, required for DeepL)
//
// Operator environment: LIBRETRANSLATE_URL / LIBRETRANSLATE_API_KEY. This
// address is trusted and may be internal (compose service, http).
//
// Like OIDCClientSecret, the key lives in the database but never leaves the
// server: responses only report api_key_set.

const (
	settingTranslationProvider = "translation.provider"
	settingTranslationURL      = "translation.url"
	settingTranslationAPIKey   = "translation.api_key"

	providerLibreTranslate = "libretranslate"
	providerDeepL          = "deepl"

	envLibreTranslateURL = "LIBRETRANSLATE_URL"
	envLibreTranslateKey = "LIBRETRANSLATE_API_KEY"

	// maxTranslateRunes limits title plus message per request (quota
	// of the service, limit abuse).
	maxTranslateRunes = 5000
)

var errTranslationNotConfigured = errors.New("keine Übersetzung konfiguriert")

// translationConfig is the effective configuration.
type translationConfig struct {
	Provider string // "libretranslate" or "deepl", empty = not configured
	URL      string
	APIKey   string
	// FromEnv: the address comes from the operator environment (trusted).
	FromEnv bool
}

// storedTranslation are the raw values from the SystemSetting table.
type storedTranslation struct {
	Provider, URL, APIKey string
}

func (h *Handler) storedTranslation() storedTranslation {
	return storedTranslation{
		Provider: h.getSystemSettingString(settingTranslationProvider),
		URL:      h.getSystemSettingString(settingTranslationURL),
		APIKey:   h.getSystemSettingString(settingTranslationAPIKey),
	}
}

// getSystemSettingString reads a text from the SystemSetting table, empty if it is missing.
func (h *Handler) getSystemSettingString(key string) string {
	var setting models.SystemSetting
	if err := h.DB.Where("key = ?", key).First(&setting).Error; err != nil {
		return ""
	}
	return setting.Value
}

// effectiveTranslationConfig merges stored values and environment.
func (h *Handler) effectiveTranslationConfig() translationConfig {
	st := h.storedTranslation()
	envURL := strings.TrimSpace(os.Getenv(envLibreTranslateURL))
	switch st.Provider {
	case providerDeepL:
		return translationConfig{Provider: providerDeepL, APIKey: st.APIKey}
	case providerLibreTranslate:
		cfg := translationConfig{Provider: providerLibreTranslate, URL: st.URL, APIKey: st.APIKey}
		if cfg.URL == "" {
			cfg.URL, cfg.FromEnv = envURL, true
			if cfg.APIKey == "" {
				cfg.APIKey = os.Getenv(envLibreTranslateKey)
			}
		}
		if cfg.URL == "" {
			return translationConfig{}
		}
		return cfg
	default:
		if envURL == "" {
			return translationConfig{}
		}
		return translationConfig{Provider: providerLibreTranslate, URL: envURL, APIKey: os.Getenv(envLibreTranslateKey), FromEnv: true}
	}
}

func (h *Handler) translationSafeClient() *http.Client {
	if h.TranslationSafeClient != nil {
		return h.TranslationSafeClient
	}
	return externalstatus.NewSafeClient(externalstatus.ClientConfig{})
}

func (h *Handler) translationTrustedClient() *http.Client {
	if h.TranslationTrustedClient != nil {
		return h.TranslationTrustedClient
	}
	return &http.Client{Timeout: translation.RequestTimeout}
}

// translationProvider builds the provider for the effective configuration. An address
// coming from the settings is checked again (https only, port
// 443) and goes through the SSRF-protected client.
func (h *Handler) translationProvider() (translation.Provider, string, error) {
	cfg := h.effectiveTranslationConfig()
	switch cfg.Provider {
	case providerDeepL:
		if cfg.APIKey == "" {
			return nil, "", errTranslationNotConfigured
		}
		return translation.NewDeepL(cfg.APIKey, h.TranslationDeepLURL, h.translationSafeClient()), providerDeepL, nil
	case providerLibreTranslate:
		if cfg.FromEnv {
			return translation.NewLibreTranslate(cfg.URL, cfg.APIKey, h.translationTrustedClient()), providerLibreTranslate, nil
		}
		base, err := externalstatus.ValidateBaseURL(cfg.URL)
		if err != nil {
			return nil, "", errTranslationNotConfigured
		}
		return translation.NewLibreTranslate(base, cfg.APIKey, h.translationSafeClient()), providerLibreTranslate, nil
	default:
		return nil, "", errTranslationNotConfigured
	}
}

// translationErrorResponse maps service errors to a response and status code.
// Only the sanitized error text is logged, never the address or key.
func translationErrorResponse(c *gin.Context, err error) {
	slog.Warn("Übersetzung fehlgeschlagen", "error", logging.SanitizeErr(err))
	switch {
	case errors.Is(err, errTranslationNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Translation is not configured", "code": "translation_not_configured"})
	case errors.Is(err, translation.ErrAuth):
		c.JSON(http.StatusBadGateway, gin.H{"error": "Translation service rejected the credentials", "code": "translation_auth_failed"})
	case errors.Is(err, translation.ErrQuota):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Translation quota reached", "code": "translation_quota_exceeded"})
	case errors.Is(err, translation.ErrUnsupportedLanguage):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "Language pair not supported", "code": "translation_language_unsupported"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "Translation service unavailable", "code": "translation_unavailable"})
	}
}

// TranslateTextHandler translates title and/or message from the page's default language
// into another language of the page (POST /pages/:page_id/translate).
// The route requires update on incidents, maintenance or notices; the
// handler neither reads nor writes records.
func (h *Handler) TranslateTextHandler(c *gin.Context) {
	id64, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	pageID := uint(id64)
	var req struct {
		TargetLanguage string `json:"target_language" binding:"required"`
		Title          string `json:"title"`
		Message        string `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "code": "invalid_request"})
		return
	}
	// Updates have no title; it is enough if title OR message
	// contains text. An empty title is not sent to the service.
	if strings.TrimSpace(req.Title) == "" && strings.TrimSpace(req.Message) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "code": "invalid_request"})
		return
	}
	if utf8.RuneCountInString(req.Title)+utf8.RuneCountInString(req.Message) > maxTranslateRunes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Text too long", "code": "translation_text_too_long"})
		return
	}
	target := strings.ToLower(strings.TrimSpace(req.TargetLanguage))
	if !h.requireAdditionalLanguage(c, pageID, target) {
		return
	}
	provider, name, err := h.translationProvider()
	if err != nil {
		translationErrorResponse(c, err)
		return
	}
	source := h.pageDefaultLanguageCode(pageID)
	title, message, err := translateTitleAndMessage(provider, source, target, req.Title, req.Message)
	fallback := false
	if err != nil && name == providerDeepL {
		// Fallback service: if DeepL fails (quota exhausted, key rejected, unreachable,
		// language not supported), the operator's LibreTranslate service translates, provided
		// it is set up and supports the language. If it also fails, DeepL's error applies.
		if lt := h.envLibreTranslateProvider(); lt != nil {
			if t2, m2, err2 := translateTitleAndMessage(lt, source, target, req.Title, req.Message); err2 == nil {
				slog.Info("Übersetzung über Ersatzdienst", "von", name, "zu", providerLibreTranslate, "grund", logging.SanitizeErr(err))
				title, message, err, name, fallback = t2, m2, nil, providerLibreTranslate, true
			}
		}
	}
	if err != nil {
		translationErrorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"title": title, "message": message, "provider": name, "source_language": source, "fallback": fallback})
}

// translateTitleAndMessage translates the title and, if present, the message with one provider in
// ONE request (batch), as far as the provider supports it.
func translateTitleAndMessage(provider translation.Provider, source, target, title, message string) (string, string, error) {
	var texts []string
	hasTitle, hasMessage := strings.TrimSpace(title) != "", strings.TrimSpace(message) != ""
	if hasTitle {
		texts = append(texts, title)
	}
	if hasMessage {
		texts = append(texts, message)
	}
	out, err := translation.TranslateMany(provider, texts, source, target)
	if err != nil {
		return "", "", err
	}
	outTitle, outMessage := "", ""
	if hasTitle {
		outTitle, out = out[0], out[1:]
	}
	if hasMessage {
		outMessage = out[0]
	}
	return outTitle, outMessage, nil
}

// envLibreTranslateProvider builds the LibreTranslate service from the operator environment (trusted,
// may be internal) or returns nil if LIBRETRANSLATE_URL is not set.
func (h *Handler) envLibreTranslateProvider() translation.Provider {
	url := strings.TrimSpace(os.Getenv(envLibreTranslateURL))
	if url == "" {
		return nil
	}
	return translation.NewLibreTranslate(url, os.Getenv(envLibreTranslateKey), h.translationTrustedClient())
}
