package translation

import (
	"encoding/json"
	"net/http"
	"strings"
)

const (
	deeplFreeURL = "https://api-free.deepl.com"
	deeplProURL  = "https://api.deepl.com"
)

// DeepL spricht die DeepL-API an (eigener Zugang des Betreibers).
// Die Texte gehen an DeepL; das steht auch im Hinweis der Oberfläche.
type DeepL struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewDeepL baut den Client. Schlüssel mit Endung ":fx" gehören zum Free-
// Angebot und haben einen eigenen Host. baseURL überschreibt den Host (nur
// Tests); leer = fester DeepL-Host, also kein frei eingebbares Ziel.
func NewDeepL(apiKey, baseURL string, client *http.Client) *DeepL {
	if baseURL == "" {
		baseURL = deeplProURL
		if strings.HasSuffix(apiKey, ":fx") {
			baseURL = deeplFreeURL
		}
	}
	return &DeepL{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: client}
}

// deeplTarget bildet einen Sprachcode des Katalogs auf DeepLs Zielsprache ab:
// DeepL verlangt für Englisch und Portugiesisch eine Variante.
func deeplTarget(code string) string {
	switch c := strings.ToUpper(code); c {
	case "EN":
		return "EN-GB"
	case "PT":
		return "PT-PT"
	default:
		return c
	}
}

// Translate übersetzt text von sourceLang nach targetLang.
func (p *DeepL) Translate(text, sourceLang, targetLang string) (string, error) {
	body := map[string]any{
		"text":        []string{text},
		"source_lang": strings.ToUpper(sourceLang),
		"target_lang": deeplTarget(targetLang),
	}
	status, raw, err := postJSON(p.client, p.baseURL+"/v2/translate", map[string]string{"Authorization": "DeepL-Auth-Key " + p.apiKey}, body)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", statusError(status)
	}
	var resp struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if json.Unmarshal(raw, &resp) != nil || len(resp.Translations) == 0 || resp.Translations[0].Text == "" {
		return "", ErrUnavailable
	}
	return resp.Translations[0].Text, nil
}

// TranslateMany übersetzt mehrere Texte in einer Anfrage (DeepL nimmt "text" als Liste).
func (p *DeepL) TranslateMany(texts []string, sourceLang, targetLang string) ([]string, error) {
	body := map[string]any{
		"text":        texts,
		"source_lang": strings.ToUpper(sourceLang),
		"target_lang": deeplTarget(targetLang),
	}
	status, raw, err := postJSON(p.client, p.baseURL+"/v2/translate", map[string]string{"Authorization": "DeepL-Auth-Key " + p.apiKey}, body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, statusError(status)
	}
	var resp struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if json.Unmarshal(raw, &resp) != nil || len(resp.Translations) != len(texts) {
		return nil, ErrUnavailable
	}
	out := make([]string, len(texts))
	for i, tr := range resp.Translations {
		if tr.Text == "" {
			return nil, ErrUnavailable
		}
		out[i] = tr.Text
	}
	return out, nil
}
