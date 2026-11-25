// Package translation defines (stage 1 of 5) the interface
// to an external translation service. The actual DeepL integration is
// deliberately NOT part of this stage - only the seam (interface +
// no-op stub) is created here, against which later stages (translation
// suggestions for incident/component/notice content) can program without
// waiting for the real provider.
package translation

// Provider translates text from a source to a target language (each as a
// language code from the language catalog, see models.Language.Code). A
// translation suggestion always ends up as i18n.StatusDraft - an admin must
// confirm it before publication (see backend/i18n).
type Provider interface {
	Translate(text, sourceLang, targetLang string) (string, error)
}

// NoopProvider is the default provider as long as no real translation
// service is configured: it returns the input text unchanged instead of
// throwing an error, so callers (later stages) can adopt the return value
// as a draft suggestion without worry - an admin then simply sees
// the original text for confirmation/manual translation instead of
// the feature failing entirely.
type NoopProvider struct{}

func (NoopProvider) Translate(text, _, _ string) (string, error) {
	return text, nil
}

// BatchProvider translates several texts in one request. This halves the waiting time when translating
// title and message (operator request 31.10.2026); the service sees both texts together, and the order
// of the results matches that of the input.
type BatchProvider interface {
	TranslateMany(texts []string, sourceLang, targetLang string) ([]string, error)
}

// TranslateMany translates texts in one call if the vendor supports batches, otherwise one after another.
func TranslateMany(p Provider, texts []string, sourceLang, targetLang string) ([]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if b, ok := p.(BatchProvider); ok {
		return b.TranslateMany(texts, sourceLang, targetLang)
	}
	out := make([]string, len(texts))
	for i, text := range texts {
		res, err := p.Translate(text, sourceLang, targetLang)
		if err != nil {
			return nil, err
		}
		out[i] = res
	}
	return out, nil
}
