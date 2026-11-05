package handlers

import (
	"log/slog"
	"strings"

	"github.com/fraigner/statushafen/models"
)

// Teil 1: Versandsprache der Benachrichtigungen an Abonnenten.
//
// Vorher war "de" fest verdrahtet (Vorfälle, Ankündigungen, Abo-Bestätigung);
// Vorlagen in anderen Sprachen ließen sich anlegen, wurden aber nie versendet.
// Jetzt gilt die Reihenfolge: gewünschte Sprache (Subscriber.Language,
// nur wenn die Seite sie aktiv hat) -> Standardsprache der
// Statusseite -> eingebauter Standardtext. Die eingebauten Texte gibt es in de
// und en (outageMailLang); jede andere Standardsprache bekommt dort Englisch
// bzw. ihre eigene Vorlage, falls eine gepflegt ist.

// pageDefaultLanguageCode liefert den Code der Standardsprache der Seite, ohne
// Pflege "de" (wie die Backfill-Vorbelegung bestehender Seiten).
func (h *Handler) pageDefaultLanguageCode(pageID uint) string {
	var l models.StatusPageLanguage
	if err := h.DB.Where("status_page_id = ? AND is_default = ?", pageID, true).First(&l).Error; err != nil || l.LanguageCode == "" {
		return "de"
	}
	return l.LanguageCode
}

// subscriberMailTemplate wählt die Vorlage (Audience subscriber) für einen
// Abonnenten: zuerst die gewünschte Sprache preferred (leer = keine Angabe),
// dann die Standardsprache der Seite. Ohne Treffer ist die Vorlage nil und der
// Aufrufer nimmt den eingebauten Standardtext. lang ist die Sprache de/en für
// diesen eingebauten Text bzw. die Platzhalter ({{.Language}}, {{.StatusLabel}}).
func (h *Handler) subscriberMailTemplate(pageID uint, kind, preferred string) (tmpl *models.NotificationTemplate, lang string) {
	def := h.pageDefaultLanguageCode(pageID)
	candidates := []string{def}
	// eine inzwischen deaktivierte oder aus dem Katalog entfernte
	// Sprache gilt nicht mehr; der Abonnent fällt dann auf die Seitenstandard-
	// sprache zurück (die Sprache bleibt am Abo gespeichert und wirkt wieder,
	// sobald sie reaktiviert wird).
	if preferred = h.validSubscriberLanguage(pageID, preferred); preferred != "" && preferred != def {
		candidates = []string{preferred, def}
	}
	for _, code := range candidates {
		if t := h.lookupNotificationTemplate(pageID, kind, NotificationAudienceSubscriber, code); t != nil {
			return t, outageMailLang(code)
		}
	}
	return nil, outageMailLang(candidates[0])
}

// memoSubscriberTemplate liefert subscriberMailTemplate für eine Art, gemerkt je
// gewünschter Sprache: Ein Versandlauf an viele Abonnenten schlägt Vorlage und
// Seitensprache so nur einmal je Sprache nach. Nicht für nebenläufigen Zugriff
// gedacht (die Versandschleifen laufen nacheinander).
func (h *Handler) memoSubscriberTemplate(pageID uint, kind string) func(preferred string) (*models.NotificationTemplate, string) {
	type result struct {
		tmpl *models.NotificationTemplate
		lang string
	}
	cache := map[string]result{}
	return func(preferred string) (*models.NotificationTemplate, string) {
		r, ok := cache[preferred]
		if !ok {
			r.tmpl, r.lang = h.subscriberMailTemplate(pageID, kind, preferred)
			cache[preferred] = r
		}
		return r.tmpl, r.lang
	}
}

// validSubscriberLanguage normalisiert die vom Abonnenten gewünschte Mailsprache:
// nur eine bei der Seite aktive Sprache wird übernommen, alles andere (leer,
// unbekannt, nicht aktiviert) ergibt "" = Seitenstandardsprache.
func (h *Handler) validSubscriberLanguage(pageID uint, code string) string {
	lang, err := h.resolveSubscriberLanguage(pageID, code)
	if err != nil {
		// Lesefehler ist nicht "ungültig". Der Versand fällt auf die
		// Seitenstandardsprache zurück; der Fehlertext bleibt aus dem Log.
		slog.Error("Abonnentensprache nicht prüfbar", "status_page_id", pageID)
		return ""
	}
	return lang
}

// resolveSubscriberLanguage wie validSubscriberLanguage, unterscheidet aber einen
// Lesefehler (err != nil) von "ungültig" (leer, err == nil). Schreibende Pfade
// antworten bei einem Fehler mit 500, statt eine gültige Sprache still zu
// überschreiben.
func (h *Handler) resolveSubscriberLanguage(pageID uint, code string) (string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return "", nil
	}
	var n int64
	// auch im Sprachkatalog aktiv (Language.IsActive), nicht nur der
	// Seite zugeordnet - eine deaktivierte Katalogsprache wird nicht mehr angeboten.
	if err := h.DB.Model(&models.StatusPageLanguage{}).
		Where("status_page_id = ? AND language_code = ? AND language_code IN (?)", pageID, code,
			h.DB.Model(&models.Language{}).Select("code").Where("is_active = ?", true)).
		Count(&n).Error; err != nil {
		slog.Error("Abonnentensprache nicht prüfbar", "status_page_id", pageID)
		return "", err
	} else if n == 0 {
		return "", nil
	}
	return code, nil
}

// subscriberChatLang ist die Sprache (de/en) für Chat-Texte: die effektive
// Sprache des Abonnenten (gültig und aktiv, sonst Seitenstandard), unabhängig
// davon, welche Mail-Vorlage existiert. Gemerkt je gewünschter Sprache.
func (h *Handler) memoSubscriberChatLang(pageID uint) func(preferred string) string {
	cache := map[string]string{}
	return func(preferred string) string {
		if l, ok := cache[preferred]; ok {
			return l
		}
		code := h.validSubscriberLanguage(pageID, preferred)
		if code == "" {
			code = h.pageDefaultLanguageCode(pageID)
		}
		l := outageMailLang(code)
		cache[preferred] = l
		return l
	}
}

// staffMailLang bildet User.PreferredLanguage auf die Sprache der Mails an interne
// Nutzer ab: "de" -> de, alles andere inkl. leer -> en (bisheriges
// Verhalten, damit Nutzer ohne gespeicherte Sprache nichts Neues erleben).
func staffMailLang(preferred string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(preferred)), "de") {
		return "de"
	}
	return "en"
}

// staffNotificationTemplate wählt die Vorlage (Audience staff) für interne
// Nutzer: erst in deren Sprache, dann die englische (Vorlagen, die vor der Umstellung nur
// auf Englisch gepflegt wurden, gelten weiter). tmplLang ist die Sprache für die
// Platzhalter. Ohne Treffer ist die Vorlage nil (eingebauter Text in lang).
func (h *Handler) staffNotificationTemplate(pageID uint, kind, lang string) (tmpl *models.NotificationTemplate, tmplLang string) {
	if t := h.lookupNotificationTemplate(pageID, kind, NotificationAudienceStaff, lang); t != nil {
		return t, lang
	}
	if lang != "en" {
		if t := h.lookupNotificationTemplate(pageID, kind, NotificationAudienceStaff, "en"); t != nil {
			return t, "en"
		}
	}
	return nil, lang
}
