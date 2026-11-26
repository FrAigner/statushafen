package handlers

import (
	"log/slog"
	"strings"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"
)

// Part 1: sending language of the notifications to subscribers.
//
// Previously "de" was hard-wired (incidents, announcements, subscription confirmation);
// templates in other languages could be created but were never sent.
// Now the order applies: desired language (Subscriber.Language,
// only if the page has it active) -> default language of the
// status page -> built-in default text. The built-in texts exist in de
// and en (outageMailLang); any other default language gets English there
// or its own template, if one is maintained.

// pageDefaultLanguageCode returns the code of the page's default language, or
// "de" if none is maintained (like the backfill preset of existing pages).
func (h *Handler) pageDefaultLanguageCode(pageID uint) string {
	var l models.StatusPageLanguage
	if err := h.DB.Where("status_page_id = ? AND is_default = ?", pageID, true).First(&l).Error; err != nil || l.LanguageCode == "" {
		return "de"
	}
	return l.LanguageCode
}

// subscriberMailTemplate selects the template (audience subscriber) for a
// subscriber: first the desired language preferred (empty = not given),
// then the page's default language. Without a match the template is nil and the
// caller uses the built-in default text. lang is the language de/en for
// this built-in text and the placeholders ({{.Language}}, {{.StatusLabel}}).
func (h *Handler) subscriberMailTemplate(pageID uint, kind, preferred string) (tmpl *models.NotificationTemplate, lang string) {
	def := h.pageDefaultLanguageCode(pageID)
	candidates := []string{def}
	// a language that has since been disabled or removed from the catalog
	// no longer applies; the subscriber then falls back to the page default
	// language (the language stays stored on the subscription and takes effect again
	// as soon as it is reactivated).
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

// memoSubscriberTemplate returns subscriberMailTemplate for a kind, memoized per
// desired language: a send run to many subscribers thus looks up the template and
// page language only once per language. Not intended for concurrent access
// (the send loops run sequentially).
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

// validSubscriberLanguage normalizes the mail language desired by the subscriber:
// only a language active on the page is accepted, everything else (empty,
// unknown, not enabled) yields "" = page default language.
func (h *Handler) validSubscriberLanguage(pageID uint, code string) string {
	// With the switch off every mail goes out in the page default; the stored
	// preference is kept and applies again once the switch is on.
	if !h.translationEnabled() {
		return ""
	}
	lang, err := h.resolveSubscriberLanguage(pageID, code)
	if err != nil {
		// A read error is not "invalid". Sending falls back to the
		// page default language; the error text stays out of the log.
		slog.Error("Abonnentensprache nicht prüfbar", "status_page_id", pageID)
		return ""
	}
	return lang
}

// resolveSubscriberLanguage is like validSubscriberLanguage, but distinguishes a
// read error (err != nil) from "invalid" (empty, err == nil). Writing paths
// respond with 500 on an error instead of silently
// overwriting a valid language.
func (h *Handler) resolveSubscriberLanguage(pageID uint, code string) (string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	// Only content languages (de/en); anything else means the page default.
	if code == "" || !database.IsContentLanguage(code) {
		return "", nil
	}
	var n int64
	// also active in the language catalog (Language.IsActive), not just
	// assigned to the page - a disabled catalog language is no longer offered.
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

// subscriberChatLang is the language (de/en) for chat texts: the effective
// language of the subscriber (valid and active, otherwise page default), regardless
// of which mail template exists. Memoized per desired language.
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

// staffMailLang maps User.PreferredLanguage to the language of mails to internal
// users: "de" -> de, everything else incl. empty -> en (previous
// behavior, so that users without a stored language experience nothing new).
func staffMailLang(preferred string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(preferred)), "de") {
		return "de"
	}
	return "en"
}

// staffNotificationTemplate selects the template (audience staff) for internal
// users: first in their language, then English (templates that were maintained only
// in English before the changeover continue to apply). tmplLang is the language for the
// placeholders. Without a match the template is nil (built-in text in lang).
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
