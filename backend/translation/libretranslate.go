package translation

import (
	"encoding/json"
	"net/http"
	"strings"
)

// LibreTranslate spricht die API einer LibreTranslate-Instanz an:
// Standarddienst, selbst betreibbar, die Texte verlassen den eigenen Betrieb
// nicht. Der API-Schlüssel ist optional (nur bei Instanzen mit Schlüsselpflicht).
type LibreTranslate struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewLibreTranslate baut den Client. baseURL ohne abschließenden Schrägstrich
// oder mit - beides geht. Die Prüfung der Adresse (SSRF) liegt beim Aufrufer,
// der auch den passenden http.Client mitgibt.
func NewLibreTranslate(baseURL, apiKey string, client *http.Client) *LibreTranslate {
	return &LibreTranslate{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: client}
}

// Translate übersetzt text von sourceLang nach targetLang.
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

// TranslateMany übersetzt mehrere Texte in einer Anfrage (LibreTranslate nimmt "q" als Liste).
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
