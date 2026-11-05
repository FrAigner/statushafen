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

// Automatische Übersetzung: Der Bearbeiter lässt Titel und
// Nachricht aus der Standardsprache der Seite in eine weitere Sprache
// vorschlagen. Das Ergebnis wird nie gespeichert, sondern nur als Entwurf ins
// Formular geliefert.
//
// Konfiguration (Systemeinstellungen, nur Global Admin):
//   - translation.provider: "" (Standard = LibreTranslate aus der Umgebung),
//     "libretranslate" oder "deepl"
//   - translation.url: LibreTranslate-Adresse (nur https, öffentlich, SSRF-geprüft)
//   - translation.api_key: Schlüssel (LibreTranslate optional, DeepL Pflicht)
//
// Umgebung des Betreibers: LIBRETRANSLATE_URL / LIBRETRANSLATE_API_KEY. Diese
// Adresse ist vertrauenswürdig und darf intern sein (Compose-Dienst, http).
//
// Der Schlüssel liegt wie OIDCClientSecret in der Datenbank, verlässt den
// Server aber nie: Antworten melden nur api_key_set.

const (
	settingTranslationProvider = "translation.provider"
	settingTranslationURL      = "translation.url"
	settingTranslationAPIKey   = "translation.api_key"

	providerLibreTranslate = "libretranslate"
	providerDeepL          = "deepl"

	envLibreTranslateURL = "LIBRETRANSLATE_URL"
	envLibreTranslateKey = "LIBRETRANSLATE_API_KEY"

	// maxTranslateRunes begrenzt Titel plus Nachricht je Anfrage (Kontingent
	// des Dienstes schonen, Missbrauch begrenzen).
	maxTranslateRunes = 5000
)

var errTranslationNotConfigured = errors.New("keine Übersetzung konfiguriert")

// translationConfig ist die wirksame Konfiguration.
type translationConfig struct {
	Provider string // "libretranslate" oder "deepl", leer = nicht konfiguriert
	URL      string
	APIKey   string
	// FromEnv: Adresse stammt aus der Betreiber-Umgebung (vertrauenswürdig).
	FromEnv bool
}

// storedTranslation sind die rohen Werte aus der SystemSetting-Tabelle.
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

// getSystemSettingString liest einen Text aus der SystemSetting-Tabelle, leer wenn er fehlt.
func (h *Handler) getSystemSettingString(key string) string {
	var setting models.SystemSetting
	if err := h.DB.Where("key = ?", key).First(&setting).Error; err != nil {
		return ""
	}
	return setting.Value
}

// effectiveTranslationConfig führt gespeicherte Werte und Umgebung zusammen.
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

// translationProvider baut den Provider zur wirksamen Konfiguration. Eine aus
// den Einstellungen stammende Adresse wird erneut geprüft (nur https, Port
// 443) und läuft über den SSRF-geschützten Client.
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

// translationErrorResponse bildet Fehler des Dienstes auf Antwort und Code ab.
// Geloggt wird nur der bereinigte Fehlertext, nie Adresse oder Schlüssel.
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

// TranslateTextHandler übersetzt Titel und/oder Nachricht aus der Standardsprache
// der Seite in eine weitere Sprache der Seite (POST /pages/:page_id/translate).
// Die Route verlangt update auf Vorfällen, Wartung oder Ankündigungen; der
// Handler liest und schreibt keine Datensätze.
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
	// Updates haben keinen Titel; es genügt, wenn Titel ODER Nachricht
	// Text enthält. Ein leerer Titel wird nicht an den Dienst geschickt.
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
		// Ersatzdienst: Fällt DeepL aus (Kontingent erschöpft, Schlüssel abgelehnt, nicht erreichbar,
		// Sprache nicht unterstützt), übersetzt der LibreTranslate-Dienst des Betreibers, sofern
		// er eingerichtet ist und die Sprache kann. Scheitert auch er, gilt der Fehler von DeepL.
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

// translateTitleAndMessage übersetzt den Titel und, wenn vorhanden, die Nachricht mit einem Anbieter in
// EINER Anfrage (Stapel), soweit der Anbieter das kann.
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

// envLibreTranslateProvider baut den LibreTranslate-Dienst aus der Betreiber-Umgebung (vertrauenswürdig,
// darf intern sein) oder liefert nil, wenn LIBRETRANSLATE_URL nicht gesetzt ist.
func (h *Handler) envLibreTranslateProvider() translation.Provider {
	url := strings.TrimSpace(os.Getenv(envLibreTranslateURL))
	if url == "" {
		return nil
	}
	return translation.NewLibreTranslate(url, os.Getenv(envLibreTranslateKey), h.translationTrustedClient())
}
