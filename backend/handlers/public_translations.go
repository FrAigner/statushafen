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
// gebündelter öffentlicher Lese-Endpunkt für die Übersetzungen
// einer Statusseite.
//
//	GET /api/public/pages/:slug/translations?lang=xx
//
// Die Statusseite zeigt ein Vielfaches an Objekten; ein Abruf je Komponente/
// Vorfall/Ankündigung (die Einzel-Endpunkte unter /api/{components,incidents,
// notices}/:id/translations) wären zig Requests pro Seitenaufruf. Dieser
// Endpunkt liefert alles in einer Antwort, Schlüssel = ID des Objekts.
//
// Auswahl je Objekt und Feldpaar (Titel+Text zusammen, nie gemischt):
//  1. bestätigte Übersetzung in der gewünschten Sprache lang,
//  2. sonst bestätigte Übersetzung in der Standardsprache der Seite,
//  3. sonst fehlt der Eintrag - das Frontend zeigt das Original.
//
// Entwürfe (Status draft) kommen nie vor. Berücksichtigt werden nur die Objekte,
// die auch das Dashboard ausliefert: sichtbare Komponenten (Visible, samt
// Gruppen-Nachfahren), Vorfälle der Seite und aktive Ankündigungen
// im Zeitfenster. Zugriff wie das Dashboard (authorizePublicPageRead).
// ─────────────────────────────────────────────────────────────────────────────

const publicTranslationsCacheTTL = 60 * time.Second

// langCodePattern erlaubt die Codes des Sprachkatalogs ("de", "zh-hant-tw", klein
// geschrieben; Eingaben werden in Kleinbuchstaben gewandelt); die endgültige
// Prüfung ist der Abgleich mit den Sprachen der Seite.
var langCodePattern = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})*$`)

type publicTitleText struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// publicTranslationsResponse: je Typ eine Map ID (als Zeichenkette) -> Texte.
// Bei Komponenten ist Text die Beschreibung, bei Vorfällen und Ankündigungen die
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

// publicTranslationsCache hält die Antwort je (Seite, Sprache) kurz vor. Die
// Zugriffsprüfung passiert VOR dem Cache; der Schlüssel enthält nur validierte
// Sprachen der Seite, die Map ist also durch die Zahl der Seitensprachen begrenzt.
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

// GetPublicTranslationsHandler: siehe Kopfkommentar.
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

// resolvePublicLanguage ermittelt die Sprache für die öffentliche Auslieferung
// (Query lang, leer = Standardsprache der Seite) und die Standardsprache. Sie
// antwortet selbst bei Fehlern (500 bei DB-Fehler, 400 für eine nicht aktive
// Sprache) und liefert dann false. Gemeinsam genutzt von den Übersetzungen der
// Seite und der Vorfall-Updates.
func (h *Handler) resolvePublicLanguage(c *gin.Context, page models.StatusPage) (lang, def string, ok bool) {
	var langs []models.StatusPageLanguage
	if err := h.DB.Where(whereStatusPageIDEquals, page.ID).Find(&langs).Error; err != nil {
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
	if lang == "" {
		lang = def
	} else if !langCodePattern.MatchString(lang) || !allowed[lang] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Language not enabled for this page", "code": "invalid_request"})
		return "", "", false
	}
	return lang, def, true
}

// pickTranslation wählt je Objekt die Übersetzung: gewünschte Sprache vor
// Standardsprache. rows[objektID][sprache] -> Texte.
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

	// Sichtbare Komponenten wie im Dashboard.
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
