package handlers

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fraigner/statushafen/i18n"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// bundled public read endpoint for the translations
// of a status page.
//
//	GET /api/public/pages/:slug/translations?lang=xx
//
// The status page shows many more objects; one request per component/
// incident/notice (the single endpoints under /api/{components,incidents,
// notices}/:id/translations) would be dozens of requests per page view. This
// endpoint returns everything in one response, key = ID of the object.
//
// Selection per object and field pair (title+text together, never mixed):
//  1. confirmed translation in the requested language lang,
//  2. otherwise confirmed translation in the page's default language,
//  3. otherwise the entry is missing - the frontend shows the original.
//
// Drafts (status draft) never appear. Only the objects are considered
// that the dashboard also delivers: visible components (Visible, including
// group descendants), incidents of the page and active notices
// within the time window. Access like the dashboard (authorizePublicPageRead).
// ─────────────────────────────────────────────────────────────────────────────

const publicTranslationsCacheTTL = 60 * time.Second

// langCodePattern allows the codes of the language catalog ("de", "zh-hant-tw", lowercase;
// inputs are converted to lowercase); the final
// check is the comparison with the page's languages.
var langCodePattern = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})*$`)

type publicTitleText struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// publicTranslationsResponse: per type a map ID (as string) -> texts.
// For components, text is the description; for incidents and notices the
// Meldung.
type publicTranslationsResponse struct {
	Language        string                     `json:"language"`
	DefaultLanguage string                     `json:"default_language"`
	Components      map[string]publicTitleText `json:"components"`
	Incidents       map[string]publicTitleText `json:"incidents"`
	Notices         map[string]publicTitleText `json:"notices"`
}

type publicTranslationsCacheKey struct {
	pageID uint
	lang   string
}

type publicTranslationsCacheEntry struct {
	at   time.Time
	resp publicTranslationsResponse
}

// publicTranslationsCache keeps the response per (page, language) briefly. The
// access check happens BEFORE the cache; the key contains only validated
// languages of the page, so the map is bounded by the number of page languages.
type publicTranslationsCache struct {
	mu      sync.Mutex
	entries map[publicTranslationsCacheKey]publicTranslationsCacheEntry
}

func (c *publicTranslationsCache) get(k publicTranslationsCacheKey, now time.Time) (publicTranslationsResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok || now.Sub(e.at) >= publicTranslationsCacheTTL || now.Before(e.at) {
		return publicTranslationsResponse{}, false
	}
	return e.resp, true
}

func (c *publicTranslationsCache) put(k publicTranslationsCacheKey, now time.Time, r publicTranslationsResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[publicTranslationsCacheKey]publicTranslationsCacheEntry{}
	}
	c.entries[k] = publicTranslationsCacheEntry{at: now, resp: r}
}

// GetPublicTranslationsHandler: see the header comment.
func (h *Handler) GetPublicTranslationsHandler(c *gin.Context) {
	var page models.StatusPage
	if err := h.DB.Where(slugWhereClause, c.Param("slug")).First(&page).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Status page not found", "code": "page_not_found"})
		return
	}
	if !h.authorizePublicPageRead(c, page) {
		return
	}

	lang, def, ok := h.resolvePublicLanguage(c, page)
	if !ok {
		return
	}

	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	key := publicTranslationsCacheKey{pageID: page.ID, lang: lang}
	if resp, ok := h.translationsCache.get(key, now); ok {
		c.JSON(http.StatusOK, resp)
		return
	}
	resp, err := h.buildPublicTranslations(page, lang, def, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	h.translationsCache.put(key, now, resp)
	c.JSON(http.StatusOK, resp)
}

// resolvePublicLanguage determines the language for public delivery
// (query lang, empty = the page's default language) and the default language. It
// itself responds on errors (500 on DB error, 400 for an inactive
// language) and then returns false. Shared by the page's translations
// and the incident updates.
func (h *Handler) resolvePublicLanguage(c *gin.Context, page models.StatusPage) (lang, def string, ok bool) {
	langs, err := h.pageContentLanguages(page.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return "", "", false
	}
	allowed := map[string]bool{}
	for _, l := range langs {
		allowed[l.LanguageCode] = true
		if l.IsDefault {
			def = l.LanguageCode
		}
	}
	lang = strings.ToLower(c.Query("lang"))
	// With the switch off there is only the default language: a requested other
	// language is ignored instead of answered with 400 (no second round trip).
	if lang == "" || !h.translationEnabled() {
		lang = def
	} else if !langCodePattern.MatchString(lang) || !allowed[lang] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Language not enabled for this page", "code": "invalid_request"})
		return "", "", false
	}
	return lang, def, true
}

// pickTranslation selects the translation per object: requested language before
// default language. rows[objectID][language] -> texts.
func pickTranslation(rows map[uint]map[string]publicTitleText, id uint, lang, def string) (publicTitleText, bool) {
	byLang := rows[id]
	if t, ok := byLang[lang]; ok {
		return t, true
	}
	if t, ok := byLang[def]; ok {
		return t, true
	}
	return publicTitleText{}, false
}

func (h *Handler) buildPublicTranslations(page models.StatusPage, lang, def string, now time.Time) (publicTranslationsResponse, error) {
	resp := publicTranslationsResponse{
		Language: lang, DefaultLanguage: def,
		Components: map[string]publicTitleText{},
		Incidents:  map[string]publicTitleText{},
		Notices:    map[string]publicTitleText{},
	}
	wanted := []string{lang}
	if def != "" && def != lang {
		wanted = append(wanted, def)
	}

	// Visible components as in the dashboard.
	var comps []models.Component
	if err := h.DB.Select("id", "parent_id", "visible", "archived_at").Where(whereStatusPageIDEquals, page.ID).Find(&comps).Error; err != nil {
		return resp, err
	}
	comps = withoutHiddenComponents(comps)
	compIDs := make([]uint, 0, len(comps))
	for _, comp := range comps {
		compIDs = append(compIDs, comp.ID)
	}
	var incIDs []uint
	if err := h.DB.Model(&models.Incident{}).Where(whereStatusPageIDEquals, page.ID).Pluck("id", &incIDs).Error; err != nil {
		return resp, err
	}
	var noticeIDs []uint
	if err := h.DB.Model(&models.GeneralNotice{}).Where("status_page_id = ? AND is_active = ?", page.ID, true).
		Where("start_at IS NULL OR start_at <= ?", now).
		Where("end_at IS NULL OR end_at >= ?", now).
		Pluck("id", &noticeIDs).Error; err != nil {
		return resp, err
	}

	if len(compIDs) > 0 {
		var rows []models.ComponentTranslation
		if err := h.DB.Scopes(i18n.ScopeConfirmedOnly).Where("component_id IN ? AND language_code IN ?", compIDs, wanted).Find(&rows).Error; err != nil {
			return resp, err
		}
		byID := map[uint]map[string]publicTitleText{}
		for _, r := range rows {
			addTranslation(byID, r.ComponentID, r.LanguageCode, publicTitleText{r.Title, r.Description})
		}
		fillTranslations(resp.Components, compIDs, byID, lang, def)
	}
	if len(incIDs) > 0 {
		var rows []models.IncidentTranslation
		if err := h.DB.Scopes(i18n.ScopeConfirmedOnly).Where("incident_id IN ? AND language_code IN ?", incIDs, wanted).Find(&rows).Error; err != nil {
			return resp, err
		}
		byID := map[uint]map[string]publicTitleText{}
		for _, r := range rows {
			addTranslation(byID, r.IncidentID, r.LanguageCode, publicTitleText{r.Title, r.Message})
		}
		fillTranslations(resp.Incidents, incIDs, byID, lang, def)
	}
	if len(noticeIDs) > 0 {
		var rows []models.GeneralNoticeTranslation
		if err := h.DB.Scopes(i18n.ScopeConfirmedOnly).Where("general_notice_id IN ? AND language_code IN ?", noticeIDs, wanted).Find(&rows).Error; err != nil {
			return resp, err
		}
		byID := map[uint]map[string]publicTitleText{}
		for _, r := range rows {
			addTranslation(byID, r.GeneralNoticeID, r.LanguageCode, publicTitleText{r.Title, r.Message})
		}
		fillTranslations(resp.Notices, noticeIDs, byID, lang, def)
	}
	return resp, nil
}

func addTranslation(byID map[uint]map[string]publicTitleText, id uint, lang string, t publicTitleText) {
	if byID[id] == nil {
		byID[id] = map[string]publicTitleText{}
	}
	byID[id][lang] = t
}

func fillTranslations(out map[string]publicTitleText, ids []uint, byID map[uint]map[string]publicTitleText, lang, def string) {
	for _, id := range ids {
		if t, ok := pickTranslation(byID, id, lang, def); ok {
			out[strconv.FormatUint(uint64(id), 10)] = t
		}
	}
}
