package translation

import (
	"encoding/json"
	"net/http"
	"strings"
)

// LibreTranslate talks to the API of a LibreTranslate instance:
// the default service, self-hostable, the texts do not leave the own
// operation. The API key is optional (only for instances that require a key).
type LibreTranslate struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewLibreTranslate builds the client. baseURL with or without a trailing
// slash - both work. Checking the address (SSRF) is up to the caller,
// who also supplies the matching http.Client.
func NewLibreTranslate(baseURL, apiKey string, client *http.Client) *LibreTranslate {
	return &LibreTranslate{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: client}
}

// Translate translates text from sourceLang to targetLang.
func (p *LibreTranslate) Translate(text, sourceLang, targetLang string) (string, error) {
	body := map[string]string{"q": text, "source": strings.ToLower(sourceLang), "target": strings.ToLower(targetLang), "format": "text"}
	if p.apiKey != "" {
		body["api_key"] = p.apiKey
	}
	status, raw, err := postJSON(p.client, p.baseURL+"/translate", nil, body)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", statusError(status)
	}
	var resp struct {
		TranslatedText string `json:"translatedText"`
	}
	if json.Unmarshal(raw, &resp) != nil || resp.TranslatedText == "" {
		return "", ErrUnavailable
	}
	return resp.TranslatedText, nil
}

// TranslateMany translates several texts in one request (LibreTranslate takes "q" as a list).
func (p *LibreTranslate) TranslateMany(texts []string, sourceLang, targetLang string) ([]string, error) {
	body := map[string]any{"q": texts, "source": strings.ToLower(sourceLang), "target": strings.ToLower(targetLang), "format": "text"}
	if p.apiKey != "" {
		body["api_key"] = p.apiKey
	}
	status, raw, err := postJSON(p.client, p.baseURL+"/translate", nil, body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, statusError(status)
	}
	var resp struct {
		TranslatedText []string `json:"translatedText"`
	}
	if json.Unmarshal(raw, &resp) != nil || len(resp.TranslatedText) != len(texts) {
		return nil, ErrUnavailable
	}
	for _, t := range resp.TranslatedText {
		if t == "" {
			return nil, ErrUnavailable
		}
	}
	return resp.TranslatedText, nil
}
