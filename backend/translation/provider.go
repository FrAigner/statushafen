// Package translation definiert (Stage 1 von 5) die Schnittstelle
// zu einem externen Übersetzungsdienst. Die eigentliche DeepL-Anbindung ist
// bewusst NICHT Teil dieser Stufe - hier entsteht nur die Seam (Interface +
// No-Op-Stub), gegen die spätere Stages (Übersetzungsvorschläge
// für Incident/Component/Notice-Content) programmieren können, ohne auf den
// echten Provider zu warten.
package translation

// Provider übersetzt Text von einer Quell- in eine Zielsprache (jeweils als
// Sprachcode aus dem Language-Katalog, siehe models.Language.Code). Ein
// Übersetzungsvorschlag landet immer als i18n.StatusDraft - ein Admin muss
// ihn vor Veröffentlichung bestätigen (siehe backend/i18n).
type Provider interface {
	Translate(text, sourceLang, targetLang string) (string, error)
}

// NoopProvider ist der Default-Provider, solange kein echter Übersetzungs-
// dienst konfiguriert ist: er gibt den Eingabetext unverändert zurück, statt
// einen Fehler zu werfen, damit Aufrufer (spätere Stages) den Rückgabewert
// bedenkenlos als Draft-Vorschlag übernehmen können - ein Admin sieht dann
// schlicht den Originaltext zur Bestätigung/manuellen Übersetzung, statt
// dass das Feature ganz ausfällt.
type NoopProvider struct{}

func (NoopProvider) Translate(text, _, _ string) (string, error) {
	return text, nil
}

// BatchProvider übersetzt mehrere Texte in einer Anfrage. Das halbiert die Wartezeit beim Übersetzen von
// Titel und Nachricht (Betreiber-Wunsch 31.10.2026); der Dienst sieht beide Texte gemeinsam, die Reihenfolge
// der Ergebnisse entspricht der der Eingabe.
type BatchProvider interface {
	TranslateMany(texts []string, sourceLang, targetLang string) ([]string, error)
}

// TranslateMany übersetzt texts in einem Aufruf, wenn der Anbieter Stapel kann, sonst nacheinander.
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
