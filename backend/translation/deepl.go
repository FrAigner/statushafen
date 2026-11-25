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

// DeepL talks to the DeepL API (the operator's own account).
// The texts go to DeepL; this is also stated in the notice in the UI.
type DeepL struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewDeepL builds the client. Keys ending in ":fx" belong to the free
// tier and have their own host. baseURL overrides the host (tests
// only); empty = fixed DeepL host, so no freely enterable target.
func NewDeepL(apiKey, baseURL string, client *http.Client) *DeepL {
	if baseURL == "" {
		baseURL = deeplProURL
		if strings.HasSuffix(apiKey, ":fx") {
			baseURL = deeplFreeURL
		}
	}
	return &DeepL{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: client}
}

// deeplTarget maps a language code of the catalog to DeepL's target language:
// DeepL requires a variant for English and Portuguese.
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

// Translate translates text from sourceLang to targetLang.
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

// TranslateMany translates several texts in one request (DeepL takes "text" as a list).
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
