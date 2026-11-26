package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// hexColorRegex validates hex color values such as "#007bff" (branding).
// An empty string is allowed - it means "no custom color set", and the
// public page then falls back to the global CSS defaults.
var hexColorRegex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// validHeaderStyles are the predefined layout variants for the
// status page header. An empty string is NOT allowed because the
// field has a "not null default 'classic'" DB default.
var validHeaderStyles = map[string]bool{
	"classic":  true,
	"centered": true,
	"minimal":  true,
}

// validPageLayouts are the selectable page layouts. Unlike
// with HeaderStyle, the empty string is invalid here too: if the field is missing from the
// body, the previous value or the default "standard" is kept (see
// applyStatusPageWritableFields); only an explicitly sent value is validated.
var validPageLayouts = map[string]bool{
	"standard": true,
	"liste":    true,
}

// validateBranding validates the new branding fields of a StatusPage
// and, if needed, returns a German-language error message, consistent
// with the rest of this handler's error style.
func validateBranding(page *models.StatusPage) (string, bool) {
	if page.PrimaryColor != "" && !hexColorRegex.MatchString(page.PrimaryColor) {
		return "PrimaryColor muss ein Hex-Farbwert sein (z.B. #007bff)", false
	}
	if page.SecondaryColor != "" && !hexColorRegex.MatchString(page.SecondaryColor) {
		return "SecondaryColor muss ein Hex-Farbwert sein (z.B. #007bff)", false
	}
	if page.HeaderStyle != "" && !validHeaderStyles[page.HeaderStyle] {
		return "HeaderStyle muss einer von classic, centered, minimal sein", false
	}
	if !validPageLayouts[page.PageLayout] {
		return "PageLayout muss einer von standard, liste sein", false
	}
	if !validLogoURL(page.LogoURL) {
		return "LogoURL muss leer, eine https-URL oder ein absoluter Pfad (z.B. /logo.png) sein", false
	}
	return "", true
}

// maxLogoURLLength limits the stored logo address.
const maxLogoURLLength = 2048

// validLogoURL: the logo ends up as <img src> in the visitors' browser. Allowed
// are an empty value, an https URL with a host, or an absolute path on
// the own host. Other schemes (javascript:, data:, http:), protocol-
// relative addresses and control characters are rejected.
func validLogoURL(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > maxLogoURLLength || strings.ContainsAny(raw, "\r\n\t") {
		return false
	}
	if strings.HasPrefix(raw, "/") {
		return !strings.HasPrefix(raw, "//")
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// validateNotificationFromAddress validates the optional NotificationFromAddress
// field. Empty is allowed (falls back to a slug default, see
// resolveNotificationFromAddress). If set, it must, like
// the existing SMTP_FROM value, be parseable via mail.ParseAddress
// ("Display Name <address>" or a bare address) and must not contain
// CR/LF - the same check sendAlertEmail applies to smtpCfg.From,
// merely moved forward here so that an admin immediately sees an understandable
// error message instead of a silently discarded send.
func validateNotificationFromAddress(page *models.StatusPage) (string, bool) {
	if page.NotificationFromAddress == "" {
		return "", true
	}
	if hasHeaderInjectionChars(page.NotificationFromAddress) {
		return "NotificationFromAddress darf keine Zeilenumbrüche enthalten", false
	}
	if _, err := mail.ParseAddress(page.NotificationFromAddress); err != nil {
		return "NotificationFromAddress muss eine gültige E-Mail-Adresse sein (z.B. \"Acme Status <status@acme.example>\")", false
	}
	return "", true
}

// resolveNotificationFromAddress returns the From header of an email for a
// status page. The address (and thus the SMTP envelope, see
// envelopeFrom) always belongs to the operator: it comes from operatorFrom
// (SMTP_FROM). The status page only contributes the display name. A freely
// selectable sender address would let the server be abused as a relay with a forged
// sender. The page's address (NotificationFromAddress) acts
// only as Reply-To, see resolveNotificationReplyTo.
func resolveNotificationFromAddress(operatorFrom string, page models.StatusPage) string {
	op, err := mail.ParseAddress(operatorFrom)
	if err != nil {
		// Unparseable operator configuration: leave it unchanged, the
		// SMTP server rejects it as before.
		return operatorFrom
	}
	name := sanitizeDisplayName(page.Name)
	if name == "" {
		return op.String()
	}
	return (&mail.Address{Name: name, Address: op.Address}).String()
}

// sanitizeDisplayName removes control characters (including CR/LF) from a
// display name and truncates it.
func sanitizeDisplayName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > 100 {
		name = string(r[:100])
	}
	return name
}

// resolveNotificationReplyTo returns the status page's reply address for
// the Reply-To header ("" without a valid value).
func resolveNotificationReplyTo(page models.StatusPage) string {
	if page.NotificationFromAddress == "" || hasHeaderInjectionChars(page.NotificationFromAddress) {
		return ""
	}
	addr, err := mail.ParseAddress(page.NotificationFromAddress)
	if err != nil {
		return ""
	}
	return addr.String()
}

// frontendURLHost returns the hostname from FRONTEND_URL (e.g.
// "example.com"), with "localhost" as a fallback if FRONTEND_URL is not
// set or not parseable (e.g. in the local dev setup, where FRONTEND_URL
// points to localhost).
func frontendURLHost() string {
	if u, err := url.Parse(getFrontendURL()); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "localhost"
}

// Repeated literals (go:S1192).
const (
	// slugWhereClause is the GORM where condition for a slug lookup, reused in
	// several places instead of duplicating the literal string.
	slugWhereClause            = "slug = ?"
	whereUserIDAndStatusPageID = "user_id = ? AND status_page_id = ?"
	whereStatusPageIDEquals    = "status_page_id = ?"
)

// validSlugRegex allows only lowercase letters, digits and single hyphens
// as separators (no leading/trailing/double hyphen) - the same
// character set that slugify() produces from a status page name, so that
// manually entered and automatically generated slugs follow the same format.
var validSlugRegex = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// slugSanitizeRegex replaces every run of disallowed characters with exactly
// one hyphen.
var slugSanitizeRegex = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a status page name into a URL-safe
// slug suggestion, under which the page is then automatically reachable as
// <slug>.<base domain> (see PublicRoot.vue, isOwnBaseDomain).
// German umlauts are transliterated instead of simply removed, so that e.g.
// "Müller GmbH" does not become "mller-gmbh".
func slugify(name string) string {
	s := strings.ToLower(name)
	umlautReplacer := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")
	s = umlautReplacer.Replace(s)
	s = slugSanitizeRegex.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// slugTakenMessage is the 409 message on a slug collision (create and update).
const slugTakenMessage = "Slug wird bereits von einer anderen Statusseite verwendet"

// slugReservedMessage: 400 message for reserved slugs.
const slugReservedMessage = "Dieser Slug ist reserviert und kann nicht verwendet werden"

// uniqueSlug appends "-2", "-3", ... on a collision with an existing slug
// until a free slug is found. Used only for automatically generated
// slugs (empty slug field in the form) - a manually entered
// slug should instead trigger a clear 409 error on collision, see
// CreateStatusPageHandler.
func (h *Handler) uniqueSlug(base string) string {
	if base == "" {
		base = "statusseite"
	}
	slug := base
	for i := 2; ; i++ {
		var existing models.StatusPage
		err := h.DB.Where(slugWhereClause, slug).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

// The directory is anonymously readable and grew without bound with the number of pages and the
// length of name and description. Now a call returns at most
// maxPublicDirectoryLimit pages (limit/offset for paging); name and
// description appear truncated, the page itself shows the full text.
const (
	maxPublicDirectoryLimit  = 200
	maxPublicDirectoryOffset = 100000
	publicDirectoryNameMax   = 120
	publicDirectoryDescMax   = 500
)

// truncateRunes truncates s to at most max characters; a truncation ends with "…".
func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}

func (h *Handler) GetPublicPagesHandler(c *gin.Context) {
	limit, ok := parseCount(c, "limit", maxPublicDirectoryLimit, 1)
	offset, offsetOK := parseCount(c, "offset", 0, 0)
	if !ok || !offsetOK || offset > maxPublicDirectoryOffset {
		respondInvalidInput(c)
		return
	}
	limit = min(limit, maxPublicDirectoryLimit)
	pages := []models.StatusPage{}
	// substr already in the query, so that a huge description is not first
	// loaded and then truncated; one character more than allowed shows the truncation.
	if err := h.DB.Model(&models.StatusPage{}).
		Select("id, slug, logo_url, primary_color, secondary_color, header_style, page_layout, "+
			"substr(name, 1, ?) AS name, substr(description, 1, ?) AS description",
			publicDirectoryNameMax+1, publicDirectoryDescMax+1).
		Where("is_public_indexed = ? AND is_private = ?", true, false).
		Order("id asc").Limit(limit).Offset(offset).
		Find(&pages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch public pages", "code": "server_error"})
		return
	}
	out := make([]publicStatusPage, 0, len(pages))
	for _, p := range pages {
		dto := toPublicStatusPage(p)
		dto.Name = truncateRunes(dto.Name, publicDirectoryNameMax)
		dto.Description = truncateRunes(dto.Description, publicDirectoryDescMax)
		out = append(out, dto)
	}
	c.JSON(http.StatusOK, out)
}

// publicStatusPage is the public representation of a status page.
// The public endpoints (directory,
// dashboard, slug lookup) previously serialized the complete model incl.
// internal tenant configuration (AutoJoinEmailDomains, DefaultRoleID,
// NotificationFromAddress, CustomDomain/-Status, IsPrivate, ...). The DTO
// contains exactly the fields the public frontend reads
// (LandingPage.vue: ID/Name/Slug/Description, StatusPage.vue additionally
// logo, colors, header style and layout). JSON names stay the same as in the model,
// so the frontend keeps working unchanged. Admin endpoints
// still return the full model.
type publicStatusPage struct {
	ID             uint   `json:"ID"`
	Name           string `json:"Name"`
	Slug           string `json:"Slug"`
	Description    string `json:"Description"`
	LogoURL        string `json:"LogoURL"`
	PrimaryColor   string `json:"PrimaryColor"`
	SecondaryColor string `json:"SecondaryColor"`
	HeaderStyle    string `json:"HeaderStyle"`
	PageLayout     string `json:"PageLayout"`
}

func toPublicStatusPage(p models.StatusPage) publicStatusPage {
	return publicStatusPage{
		ID:             p.ID,
		Name:           p.Name,
		Slug:           p.Slug,
		Description:    p.Description,
		LogoURL:        p.LogoURL,
		PrimaryColor:   p.PrimaryColor,
		SecondaryColor: p.SecondaryColor,
		HeaderStyle:    p.HeaderStyle,
		PageLayout:     p.PageLayout,
	}
}

// autoAssignDefaultRole automatically creates a UserStatusPageRole
// with page.DefaultRoleID when none exists - cross-tenant
// self-service onboarding: EVERY logged-in account automatically gets access on the first
// visit to a private page with DefaultRoleID set.
// This now applies only to accounts whose email domain is in the page's
// AutoJoinEmailDomains (otherwise anyone could gain access through
// self-registration). If DefaultRoleID or the
// allowlist is empty, "only explicitly invited users" applies and access
// is denied.
func (h *Handler) autoAssignDefaultRole(c *gin.Context, user models.User, page models.StatusPage) (int, string, bool) {
	userID := user.ID
	if page.DefaultRoleID == nil || !emailMatchesAutoJoin(user.Email, page.AutoJoinEmailDomains) {
		return http.StatusForbidden, "Access denied", false
	}

	// The role is checked again on joining.
	// After the page was saved, the role may have been extended with write or subscriber permissions
	// or deleted; existing pages were never checked.
	var defaultRole models.Role
	if err := h.DB.First(&defaultRole, *page.DefaultRoleID).Error; err != nil || !roleSafeAsDefault(defaultRole.Permissions) {
		return http.StatusForbidden, "Access denied", false
	}

	// a soft delete by RemoveStatusPageUserHandler (a deliberate,
	// properly audited admin decision) is indistinguishable from "never had a role" for the scoped query above in
	// checkPrivatePageAccess.
	// Without this Unscoped() check, a simple page visit would silently
	// undo the removal.
	var removed models.UserStatusPageRole
	if err := h.DB.Unscoped().Where(whereUserIDAndStatusPageID, userID, page.ID).First(&removed).Error; err == nil {
		return http.StatusForbidden, "Access denied", false
	}

	newRole := models.UserStatusPageRole{
		UserID:       userID,
		StatusPageID: page.ID,
		RoleID:       *page.DefaultRoleID,
		// The page's admins set the auto-join: no consent of the account, so
		// it never lets a login return to the page's custom domain.
		JoinedVia: models.JoinedViaAutoJoin,
	}
	if err := h.DB.Create(&newRole).Error; err != nil {
		// The SPA requests in parallel on the first visit; the request that
		// loses at the unique index still has access (the membership exists).
		if isUniqueViolation(err) {
			return 0, "", true
		}
		return http.StatusInternalServerError, "Fehler bei der Rollenzuweisung", false
	}
	// Previously entirely without an audit trail, although role assignment - including the
	// automatic one - should be logged just like the manual one
	// (AddStatusPageUserHandler: "statuspage.user_role_assigned").
	h.RecordAudit(c, "statuspage.user_role_auto_assigned", "config", "StatusPage",
		strconv.FormatUint(uint64(page.ID), 10), "", "")
	return 0, "", true
}

// checkPrivatePageAccess checks whether the logged-in user has access to a
// private status page (Global Admin, existing role or auto-join via
// DefaultRoleID). Extracted from GetPublicDashboardHandler so that its
// cognitive complexity does not exceed the SonarCloud limit (go:S3776).
func (h *Handler) checkPrivatePageAccess(c *gin.Context, page models.StatusPage) (int, string, bool) {
	userIDVal, exists := c.Get("userID")
	if !exists {
		return http.StatusUnauthorized, "Unauthorized", false
	}
	userID, ok := userIDVal.(uint)
	if !ok {
		return http.StatusUnauthorized, "Unauthorized", false
	}

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		return http.StatusUnauthorized, "Unauthorized", false
	}
	if middleware.EffectiveGlobalAdmin(c, user) {
		return 0, "", true
	}
	// neither read access nor auto-join outside the SSO page.
	if !middleware.SSOAllowsPage(c, page.ID) {
		return http.StatusForbidden, "Access denied", false
	}

	var userRole models.UserStatusPageRole
	if err := h.DB.Where(whereUserIDAndStatusPageID, userID, page.ID).First(&userRole).Error; err != nil {
		return h.autoAssignDefaultRole(c, user, page)
	}
	return 0, "", true
}

func (h *Handler) GetPublicDashboardHandler(c *gin.Context) {
	slug := c.Param("slug")
	var page models.StatusPage
	if err := h.DB.Where(slugWhereClause, slug).First(&page).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Status page not found", "code": "page_not_found"})
		return
	}

	if !h.authorizePublicPageRead(c, page) {
		return
	}
	h.writePublicDashboard(c, page)
}

// authorizePublicPageRead checks read access to the public view of a
// status page and, on rejection, writes the error response itself (false).
// A subscriber token that belongs to exactly this private status page
// grants read access without a user login - e.g. for a subscriber
// without an account who follows the link in a notification email.
// Otherwise the role check applies to private pages. Shared by
// dashboard and availability so that both follow the same rule.
func (h *Handler) authorizePublicPageRead(c *gin.Context, page models.StatusPage) bool {
	if !page.IsPrivate || h.subscriberTokenGrantsPageAccess(c, page.ID) {
		return true
	}
	if status, msg, ok := h.checkPrivatePageAccess(c, page); !ok {
		if status == http.StatusInternalServerError {
			c.JSON(status, gin.H{"error": msg, "code": codeForStatus(status)})
		} else {
			respondPageNotVisible(c)
		}
		return false
	}
	return true
}

// writePublicDashboard loads components/incidents/notices of a StatusPage
// already checked as accessible and writes the dashboard response.
// Extracted from GetPublicDashboardHandler so that both the regular
// session path and the subscriber token path use the same
// response structure.
func (h *Handler) writePublicDashboard(c *gin.Context, page models.StatusPage) {
	// Fetch components
	all := []models.Component{}
	h.DB.Where(whereStatusPageIDEquals, page.ID).Order("sort_order asc").Find(&all)
	// invisible components (including descendants) appear neither in the
	// tree nor in the components of incidents; remember the id set before filtering.
	hidden := hiddenComponentIDs(all)
	all = withoutHiddenComponents(all)
	// the status of a component shown on the public status page
	// is overlaid here - only for this response, not in the DB -
	// with open assigned incidents, so that an open
	// incident does not keep pretending "operational". The admin-side
	// GetComponentsHandler deliberately stays unchanged and returns the
	// manually set status, so that the admin form on save
	// does not accidentally store a derived status as the new manual
	// status (see effectiveComponentStatus in component.go).
	openComponentIDs := h.componentIDsWithOpenIncidents(page.ID)
	// effective_status/external_sources; status keeps its
	// previous meaning (stored + open incidents), part D shows
	// effective_status an.
	h.annotateEffectiveStatus(page.ID, all, openComponentIDs)
	tagsByComponent := h.tagsByComponentOfPage(page.ID)
	for i := range all {
		all[i].Tags = tagsByComponent.of(all[i].ID)
		if all[i].Status != "group" {
			all[i].Status = effectiveComponentStatus(all[i].Status, openComponentIDs[all[i].ID])
		}
	}
	tree := buildTree(all, nil)

	// Incidents: all open ones (at most 200) plus the newest resolved ones
	// (incident_limit, default 50, at most 200); the UI loads older ones
	// page by page via GET /api/pages/:page_id/incidents.
	incidents, more, ok := h.dashboardIncidents(c, page.ID)
	if !ok {
		return
	}
	stripHiddenIncidentComponents(incidents, hidden)

	// Fetch active General Notices: IsActive must be true, and
	// if StartAt/EndAt are set, "now" must lie within. Newest
	// first, analogous to incidents.
	now := time.Now()
	notices := []models.GeneralNotice{}
	h.DB.Where("status_page_id = ? AND is_active = ?", page.ID, true).
		Where("start_at IS NULL OR start_at <= ?", now).
		Where("end_at IS NULL OR end_at >= ?", now).
		Order("created_at desc").
		Find(&notices)

	c.JSON(http.StatusOK, gin.H{
		"page":       toPublicStatusPage(page),
		"languages":  h.publicPageLanguages(page.ID),
		"components": tree,
		"incidents":  h.toIncidentResponses(incidents),
		// incidents_more: there are older resolved incidents than the ones delivered.
		"incidents_more": more,
		"notices":        notices,
		// mirrored notices from external sources (part D).
		"external_incidents": h.publicExternalIncidents(page.ID),
	})
}

// dashboardIncidents loads the incidents of the public view: all open ones and
// the newest resolved ones, both limited. more reports further resolved ones.
// On an invalid incident_limit (400) or a database error (500)
// the response has been written (ok=false).
func (h *Handler) dashboardIncidents(c *gin.Context, pageID uint) (incidents []models.Incident, more, ok bool) {
	limit, valid := parseCount(c, "incident_limit", defaultIncidentListLimit, 1)
	if !valid {
		badIncidentWindow(c)
		return nil, false, false
	}
	limit = min(limit, maxIncidentListLimit)

	incidents = []models.Incident{}
	base := func() *gorm.DB {
		return h.DB.Where(whereStatusPageIDEquals, pageID).Preload("IncidentComponents.Component").Order(incidentNewestFirst)
	}
	// Errors abort (500) instead of showing a seemingly empty status page.
	if err := base().Where("status NOT IN ?", models.ResolvedIncidentStatuses).Limit(maxIncidentListLimit).Find(&incidents).Error; err != nil {
		slog.Error("Dashboard: offene Vorfälle nicht lesbar", "page", pageID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch incidents", "code": "server_error"})
		return nil, false, false
	}
	resolved := []models.Incident{}
	// One row more than requested shows whether there are older ones.
	if err := base().Where("status IN ?", models.ResolvedIncidentStatuses).Limit(limit + 1).Find(&resolved).Error; err != nil {
		slog.Error("Dashboard: behobene Vorfälle nicht lesbar", "page", pageID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch incidents", "code": "server_error"})
		return nil, false, false
	}
	if len(resolved) > limit {
		more = true
		resolved = resolved[:limit]
	}
	return append(incidents, resolved...), more, true
}

// publicLanguage is a language enabled for the status page:
// code, native name from the language catalog and default flag.
type publicLanguage struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	// IsActive: active in the language catalog. The language switcher shows
	// all page languages as before, the subscription selections only active ones.
	IsActive bool `json:"is_active"`
}

// publicPageLanguages returns the page's languages (sorted by code) for
// the public language switcher. Errors yield an empty list: the
// switcher then falls back to the UI languages, the dashboard
// stays usable.
func (h *Handler) publicPageLanguages(pageID uint) []publicLanguage {
	// Errors yield an empty list (see above).
	rows, _ := h.pageContentLanguages(pageID)
	out := make([]publicLanguage, 0, len(rows))
	for _, r := range rows {
		name := r.Language.Name
		if name == "" {
			name = r.LanguageCode
		}
		out = append(out, publicLanguage{Code: r.LanguageCode, Name: name, IsDefault: r.IsDefault, IsActive: r.Language.IsActive})
	}
	return out
}

func (h *Handler) GetStatusPagesHandler(c *gin.Context) {
	pages := []models.StatusPage{}
	if err := h.DB.Find(&pages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Laden der Statusseiten", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, pages)
}

// GET /api/pages/slug/:slug — a single status page by slug
//
// this endpoint is public (no AuthMiddleware, main.go
// does attach OptionalAuthMiddleware though) and previously returned private pages
// unchecked - anyone who knew/guessed the slug got name/description/
// CustomDomain even without permission. For page.IsPrivate without valid
// permission, 404 is now consistently returned (as for a non-
// existent slug), so that a caller cannot tell whether the
// page does not exist at all or is merely private.
func (h *Handler) GetStatusPageBySlugHandler(c *gin.Context) {
	slug := c.Param("slug")
	var page models.StatusPage
	if err := h.DB.Where(slugWhereClause, slug).First(&page).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return
	}

	if page.IsPrivate && !h.userHasRoleOnPrivatePage(c, page.ID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return
	}

	// public endpoint, therefore only the DTO.
	c.JSON(http.StatusOK, toPublicStatusPage(page))
}

// statusPageWritableFields is the bind target for create/update.
// Deliberately a separate DTO instead of binding directly into models.StatusPage: the
// model struct embeds gorm.Model, whose ID field carries no json:"-" -
// a request body with e.g. {"ID": 7, ...} would otherwise overwrite page.ID
// and thereby redirect Save() to a different (foreign) status page, independent
// of the :page_id in the URL. The DTO contains only the fields actually
// changeable by the client; WebhookTokenHash (json:"-") additionally stays excluded.
// CustomDomain/CustomDomainStatus are NOT included here for the same reason:
// they go exclusively through the dedicated
// endpoints in custom_domain.go, so that a domain change always reliably
// resets the verification status to "pending", instead of this
// generic bind accidentally combining it with a leftover "verified"/
// "active" marker.
type statusPageWritableFields struct {
	Name            string `json:"Name"`
	Slug            string `json:"Slug"`
	Description     string `json:"Description"`
	LogoURL         string `json:"LogoURL"`
	IsPublicIndexed bool   `json:"IsPublicIndexed"`
	IsPrivate       bool   `json:"IsPrivate"`
	DefaultRoleID   *uint  `json:"DefaultRoleID"`
	// AutoJoinEmailDomains: see models.StatusPage.
	AutoJoinEmailDomains string `json:"AutoJoinEmailDomains"`
	PrimaryColor         string `json:"PrimaryColor"`
	SecondaryColor       string `json:"SecondaryColor"`
	HeaderStyle          string `json:"HeaderStyle"`
	// PageLayout: pointer, so that "field missing" (keep value) stays
	// distinguishable from an explicitly sent empty value (400).
	PageLayout *string `json:"PageLayout"`
	// NotificationFromAddress: see validateNotificationFromAddress.
	NotificationFromAddress string `json:"NotificationFromAddress"`
}

func applyStatusPageWritableFields(page *models.StatusPage, in statusPageWritableFields) {
	page.Name = in.Name
	page.Slug = in.Slug
	page.Description = in.Description
	page.LogoURL = in.LogoURL
	page.IsPrivate = in.IsPrivate
	// private pages must never be publicly listed.
	page.IsPublicIndexed = in.IsPublicIndexed && !in.IsPrivate
	page.DefaultRoleID = in.DefaultRoleID
	page.AutoJoinEmailDomains = in.AutoJoinEmailDomains
	page.PrimaryColor = in.PrimaryColor
	page.SecondaryColor = in.SecondaryColor
	page.HeaderStyle = in.HeaderStyle
	if in.PageLayout != nil {
		page.PageLayout = *in.PageLayout
	}
	page.NotificationFromAddress = in.NotificationFromAddress
}

// userHasRoleOnPrivatePage checks global admin status or an existing
// UserStatusPageRole for pageID - deliberately WITHOUT the auto-join side effect from
// GetPublicDashboardHandler (DefaultRoleID would automatically create a new
// UserStatusPageRole there when no role exists). For a pure
// metadata read endpoint like GetStatusPageBySlugHandler, calling it
// should not trigger a role assignment as a side effect.
func (h *Handler) userHasRoleOnPrivatePage(c *gin.Context, pageID uint) bool {
	userIDVal, exists := c.Get("userID")
	if !exists {
		return false
	}
	userID, ok := userIDVal.(uint)
	if !ok {
		return false
	}

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		return false
	}
	if middleware.EffectiveGlobalAdmin(c, user) {
		return true
	}
	if !middleware.SSOAllowsPage(c, pageID) {
		return false
	}

	var userRole models.UserStatusPageRole
	return h.DB.Where(whereUserIDAndStatusPageID, userID, pageID).First(&userRole).Error == nil
}

// userHasPermissionOnPrivatePage checks whether the logged-in user has, on pageID,
// a role with exactly the given permission (or is Global Admin)
// - unlike userHasRoleOnPrivatePage, "any
// role" is not enough. After auto-join (StatusPage.DefaultRoleID) that would be true for EVERY
// logged-in user, regardless of what the assigned role
// actually allows (special case of auto-join, found by an independent review).
// Reusable for
// future cases that need the same permission check instead of a role check;
// mirrors the same permission-string logic as middleware.RequirePermission,
// just as a callable function instead of middleware (the roleHasPermission there
// is unexported and not usable across packages).
func (h *Handler) userHasPermissionOnPrivatePage(c *gin.Context, pageID uint, permission string) bool {
	userIDVal, exists := c.Get("userID")
	if !exists {
		return false
	}
	userID, ok := userIDVal.(uint)
	if !ok {
		return false
	}

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		return false
	}
	if middleware.EffectiveGlobalAdmin(c, user) {
		return true
	}
	if !middleware.SSOAllowsPage(c, pageID) {
		return false
	}

	var userRole models.UserStatusPageRole
	if err := h.DB.Preload("Role").Where(whereUserIDAndStatusPageID, userID, pageID).First(&userRole).Error; err != nil {
		return false
	}
	return permissions.Has(userRole.Role.Permissions, permission)
}

// pageIsAccessible is the shared access check for all page_id-/
// global-ID endpoints that return the same data as GetPublicDashboardHandler
// (components, incidents, notices, translations): public pages
// are always readable, private pages via checkPrivatePageAccess (Global
// Admin, existing role, or auto-join via DefaultRoleID). Auto-join
// previously applied only to the dashboard route - deep links and direct
// API calls to the same data should trigger it just the same, otherwise
// a user would be treated differently depending on the entry point (dashboard vs. deep link).
// behandelt.
func (h *Handler) pageIsAccessible(c *gin.Context, page models.StatusPage) bool {
	if !page.IsPrivate {
		return true
	}
	_, _, ok := h.checkPrivatePageAccess(c, page)
	return ok
}

// respondNotVisible answers access to something the caller must not
// see exactly like access to something that does not exist: 404 with the
// text and code the route uses for a missing target. If a
// private page answered with 401 or 403, it would be possible to tell anonymously which
// slugs and IDs belong to private pages.
func respondNotVisible(c *gin.Context, message, code string) {
	c.JSON(http.StatusNotFound, gin.H{"error": message, "code": code})
}

// respondPageNotVisible: the response for "page missing" and "page private, no
// permission" of the routes that address a page by slug or page_id.
func respondPageNotVisible(c *gin.Context) {
	respondNotVisible(c, "Status page not found", "page_not_found")
}

// requirePageVisible checks read access to a page (like pageIsAccessible,
// including auto-join) and on rejection answers with the route's response for
// a missing target. false: the response has been written.
func (h *Handler) requirePageVisible(c *gin.Context, page models.StatusPage, notFoundMessage, notFoundCode string) bool {
	if h.pageIsAccessible(c, page) {
		return true
	}
	respondNotVisible(c, notFoundMessage, notFoundCode)
	return false
}

// POST /api/pages — create a new status page
func (h *Handler) CreateStatusPageHandler(c *gin.Context) {
	var in statusPageWritableFields
	if err := c.ShouldBindJSON(&in); err != nil {
		respondInvalidInput(c)
		return
	}
	page := models.StatusPage{PageLayout: "standard"}
	applyStatusPageWritableFields(&page, in)
	// a name is required (the frontend checked it, the backend otherwise created
	// pages without names).
	page.Name = strings.TrimSpace(page.Name)
	if page.Name == "" {
		abortInvalidInput(c, codeNameRequired, "Name darf nicht leer sein")
		return
	}
	if !requirePageNameLength(c, page.Name) {
		return
	}

	// an empty slug field is generated automatically from the name
	// (the page is then immediately reachable as <slug>.<base domain>, DNS
	// wildcard + TLS already run via Caddy). A manually entered slug
	// is by contrast strictly validated and clearly rejected on collision instead of
	// showing the user only the generic 500 of the DB uniqueIndex.
	slug := strings.ToLower(strings.TrimSpace(page.Slug))
	if slug == "" {
		page.Slug = h.uniqueSlug(slugify(page.Name))
	} else if !validSlugRegex.MatchString(slug) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Slug darf nur Kleinbuchstaben, Ziffern und einzelne Bindestriche enthalten", "code": "slug_invalid"})
		return
	} else if isReservedSlug(slug) {
		abortInvalidInput(c, codeSlugReserved, slugReservedMessage)
		return
	} else {
		var existing models.StatusPage
		if err := h.DB.Where(slugWhereClause, slug).First(&existing).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": slugTakenMessage, "code": "slug_taken"})
			return
		}
		page.Slug = slug
	}

	if status, msg, ok := h.validateAutoJoinSettings(&page); !ok {
		c.JSON(status, gin.H{"error": msg, "code": codeForStatus(status)})
		return
	}
	if msg, ok := validateBranding(&page); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "invalid_request"})
		return
	}
	if msg, ok := validateNotificationFromAddress(&page); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "invalid_request"})
		return
	}
	if err := h.DB.Create(&page).Error; err != nil {
		// Race between pre-check and insert -> 409 as above.
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": slugTakenMessage, "code": "slug_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Erstellen", "code": "server_error"})
		return
	}
	// GORM replaces the zero-value false with default:true. For private
	// pages, "not listed" therefore has to be set explicitly after creation.
	if page.IsPrivate {
		if err := h.DB.Model(&page).Update("is_public_indexed", false).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Erstellen", "code": "server_error"})
			return
		}
	}
	// every new status page gets the same de+en/de default
	// language configuration as the backfill for existing pages (see
	// database.backfillStatusPageLanguages), so that new and existing
	// pages do not differ. An error here must not prevent the creation of the page
	// itself - isValidNotificationLanguage falls back to
	// the same en/de convention anyway as long as no StatusPageLanguage row
	// exists (see handlers/notification_template_admin.go).
	if err := database.SeedDefaultStatusPageLanguages(h.DB, page.ID); err != nil {
		slog.Error("Failed to seed default languages for new status page", "status_page_id", page.ID, "error", err)
	}
	h.RecordAudit(c, "statuspage.created", "config", "StatusPage", strconv.FormatUint(uint64(page.ID), 10), page.Name, "")
	c.JSON(http.StatusCreated, page)
}

// PUT /api/admin/pages/:page_id — update a status page
func (h *Handler) UpdateStatusPageHandler(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("page_id"))
	var page models.StatusPage
	if err := h.DB.First(&page, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Nicht gefunden", "code": "page_not_found"})
		return
	}
	var in statusPageWritableFields
	if err := c.ShouldBindJSON(&in); err != nil {
		respondInvalidInput(c)
		return
	}
	originalSlug := page.Slug
	applyStatusPageWritableFields(&page, in)
	// Defensive: the DTO has no ID field anyway, so page.ID cannot be overwritten by the
	// bind above - still hard-reset to the
	// URL value, so that this invariant does not silently disappear with future
	// changes to statusPageWritableFields.
	page.ID = uint(id)
	// Name stays mandatory (see CreateStatusPageHandler).
	page.Name = strings.TrimSpace(page.Name)
	if page.Name == "" {
		abortInvalidInput(c, codeNameRequired, "Name darf nicht leer sein")
		return
	}
	if !requirePageNameLength(c, page.Name) {
		return
	}

	// same collision check as on create, in case the slug
	// (and thus the subdomain) is changed later.
	slug := strings.ToLower(strings.TrimSpace(page.Slug))
	if !validSlugRegex.MatchString(slug) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Slug darf nur Kleinbuchstaben, Ziffern und einzelne Bindestriche enthalten", "code": "slug_invalid"})
		return
	}
	var existing models.StatusPage
	if err := h.DB.Where("slug = ? AND id != ?", slug, id).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": slugTakenMessage, "code": "slug_taken"})
		return
	}
	// block reserved slugs only when changing the slug; an
	// existing page (including the system page with "system") may keep its slug
	// and otherwise continue to be saved.
	if slug != originalSlug && isReservedSlug(slug) {
		abortInvalidInput(c, codeSlugReserved, slugReservedMessage)
		return
	}
	// The slug of the system page is fixed (public URL, dogfooding).
	if page.IsSystem && slug != database.SystemStatusPageSlug {
		c.JSON(http.StatusConflict, gin.H{"error": "Der Slug der Systemstatusseite kann nicht geändert werden", "code": "system_page_slug_locked"})
		return
	}
	page.Slug = slug

	if status, msg, ok := h.validateAutoJoinSettings(&page); !ok {
		c.JSON(status, gin.H{"error": msg, "code": codeForStatus(status)})
		return
	}
	if msg, ok := validateBranding(&page); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "invalid_request"})
		return
	}
	if msg, ok := validateNotificationFromAddress(&page); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "invalid_request"})
		return
	}
	if err := h.DB.Save(&page).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": slugTakenMessage, "code": "slug_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Aktualisieren", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "statuspage.updated", "config", "StatusPage", strconv.FormatUint(uint64(page.ID), 10), page.Name, "")
	c.JSON(http.StatusOK, page)
}

// DELETE /api/admin/pages/:page_id — delete status page
func (h *Handler) DeleteStatusPageHandler(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("page_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_id", "code": "invalid_id"})
		return
	}
	var page models.StatusPage
	if err := h.DB.First(&page, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Nicht gefunden", "code": "page_not_found"})
		return
	}
	// The system page represents the state of this
	// installation itself and is expected on every start by
	// database.SeedSystemStatusPage. Deleting is therefore blocked rather than
	// silently allowed: re-creating it on the next start would have produced an empty page
	// with a new ID, and the old one's components, subscriptions and roles would be lost.
	if page.IsSystem || page.Slug == database.SystemStatusPageSlug {
		c.JSON(http.StatusConflict, gin.H{"error": "Die Systemstatusseite kann nicht gelöscht werden", "code": "system_page_protected"})
		return
	}
	if err := h.DB.Delete(&models.StatusPage{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Löschen", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "statuspage.deleted", "config", "StatusPage", strconv.Itoa(id), page.Name, "")
	c.JSON(http.StatusOK, gin.H{"message": "Gelöscht"})
}

// GET /api/admin/pages/:page_id/users — list users of a page
// Only the fields needed for administration (ID, email, role name),
// never the SSO identifier, admin flag or permission list of the accounts.
func (h *Handler) GetStatusPageUsersHandler(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("page_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_id", "code": "invalid_id"})
		return
	}
	var entries []models.UserStatusPageRole
	h.DB.Preload("User").Preload("Role").
		Where(whereStatusPageIDEquals, id).Find(&entries)
	out := make([]pageUserDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, newPageUserDTO(e))
	}
	c.JSON(http.StatusOK, out)
}

// POST /api/admin/pages/:page_id/users — add a user to a page (or change role)
//
// Non-Global-Admins invite by email (page_invitation.go),
// the membership only comes into being with the account holder's consent.
//
// user_id and role_id are checked against the database.
// A Global Admin selects the account by user_id (or email). All others
// (e.g. Page Owner) must not inject an arbitrary account by ID: they
// change the role of existing members of this page or add an account
// by email; the response there is the same for known and unknown addresses
// (no account lookup). The assigned role must never have more permissions
// than that of the assigner on this page.
func (h *Handler) AddStatusPageUserHandler(c *gin.Context) {
	pageID, err := strconv.Atoi(c.Param("page_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_id", "code": "invalid_id"})
		return
	}
	var input struct {
		UserID uint   `json:"user_id"`
		Email  string `json:"email"`
		RoleID uint   `json:"role_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		respondInvalidInput(c)
		return
	}

	// Form errors (without role_id) are no role oracle: 400 instead of 403 (regression,
	// noticed in the router tests TestRoutePermissions/TestRouter_SSOSession).
	if input.RoleID == 0 {
		respondInvalidInput(c)
		return
	}
	var page models.StatusPage
	if err := h.DB.First(&page, pageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Nicht gefunden", "code": "page_not_found"})
		return
	}
	isGA := h.isEffectiveGlobalAdmin(c)

	// Role-related checks run for non-Global-Admins BEFORE the account
	// lookup and respond the same for missing, Global Admin and uncovered
	// roles: neither the address nor existing role IDs can be
	// guessed this way (review).
	var role models.Role
	roleErr := h.DB.First(&role, input.RoleID).Error
	var actorPerms string
	if !isGA {
		actorPerms = h.actorPagePermissions(c, uint(pageID))
		globalOnly, goErr := h.globalOnlyRoleIDs(h.DB, []uint{role.ID})
		if goErr != nil {
			// O2: block on DB error of the role check (fail closed).
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
			return
		}
		if roleErr != nil || globalOnly[role.ID] || !roleCoveredByActor(actorPerms, role) {
			abortRoleExceedsOwn(c)
			return
		}
	} else if roleErr != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found", "code": "role_not_found"})
		return
	}

	// A user can only ever have one role per status page. Without this check,
	// reassigning (e.g. upgrade from Viewer to Editor) would create a second
	// row; RequirePermission however always reads the oldest row via First(),
	// so the newly assigned role would never take effect.
	target, found, status := h.resolvePageUserTarget(c, isGA, uint(pageID), input.UserID, input.Email)
	if status != 0 {
		return
	}
	byEmail := input.UserID == 0 && !isGA

	// Same queries for known and unknown addresses (ID 0 finds nothing).
	var existing models.UserStatusPageRole
	err = h.DB.Preload("Role").Where(whereUserIDAndStatusPageID, target.ID, pageID).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
		return
	}
	isMember := err == nil
	if !isGA && !isMember {
		// A non-Global-Admin no longer attaches an account directly.
		// Every address that is not yet a member (with, without or with a non-
		// attachable account) gets an invitation and the same response; only a
		// found account (found) receives the email. Via user_id (a just
		// removed member) there is no way around the invitation.
		if byEmail {
			mailTo := models.User{}
			if found {
				mailTo = target
			}
			h.invitePageUser(c, page, role, input.Email, mailTo, actorPerms)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Add accounts by email address", "code": "user_email_required"})
		return
	}

	if isGA {
		// "Global Admin" is not a page role (see pageRolesAssignable).
		if !h.pageRolesAssignable(target.ID, []PageRoleInput{{StatusPageID: uint(pageID), RoleID: input.RoleID}}) {
			abortGlobalAdminRoleNotAssignable(c)
			return
		}
	} else if isMember && !roleCoveredByActor(actorPerms, existing.Role) {
		// Member with a stronger (or deleted) role: on the email path
		// neutral and without change, otherwise 403 (the member is visible).
		if byEmail {
			c.JSON(http.StatusOK, gin.H{"message": pageUserEmailAcceptedMsg})
			return
		}
		abortRoleExceedsOwn(c)
		return
	}

	if isMember {
		h.updatePageUserRole(c, existing, role, byEmail)
		return
	}

	// From here on only Global Admins (all others invite above).
	entry := models.UserStatusPageRole{
		UserID:       target.ID,
		StatusPageID: uint(pageID),
		RoleID:       input.RoleID,
	}
	// Lock the user row before the role is created; a concurrently
	// deleted account no longer gets a role (404 instead of 201).
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockLiveUser(tx, target.ID); err != nil {
			return err
		}
		return tx.Create(&entry).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found", "code": "user_not_found"})
			return
		}
		if isUniqueViolation(err) {
			// A concurrent call was faster (unique index
			// user_id + status_page_id). Adding is idempotent: the now
			// existing member gets the requested role, instead of a 500.
			h.assignAfterRace(c, role, uint(pageID), target.ID)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "statuspage.user_role_assigned", "config", "StatusPage", strconv.Itoa(pageID), "", fmt.Sprintf("user_id=%d role_id=%d", target.ID, input.RoleID))
	respondPageUserAssigned(c, http.StatusCreated, entry, false)
}

// DELETE /api/admin/pages/:page_id/users/:uid — remove a user from a page
func (h *Handler) RemoveStatusPageUserHandler(c *gin.Context) {
	pageID, err := strconv.Atoi(c.Param("page_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_id", "code": "invalid_id"})
		return
	}
	userID, err := strconv.Atoi(c.Param("uid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid uid", "code": "invalid_id"})
		return
	}
	// Every DB error except "not found" aborts (fail closed), the
	// permission check follows in mayRemovePageUser.
	details := fmt.Sprintf("user_id=%d", userID)
	var current models.UserStatusPageRole
	curErr := h.DB.Preload("Role").Where(whereUserIDAndStatusPageID, userID, pageID).First(&current).Error
	if curErr != nil && !errors.Is(curErr, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Entfernen", "code": "server_error"})
		return
	}
	if !h.mayRemovePageUser(c, curErr, current, uint(pageID)) {
		return
	}
	if curErr == nil {
		details += fmt.Sprintf(" role_id=%d", current.RoleID)
	}
	lockCheck := curErr == nil && h.lockOutApplies(c, uint(userID), current.Role, nil)
	// Review: between the check and the deletion, the
	// membership may already be gone (TOCTOU). A non-Global-Admin must then
	// not delete any links anymore: roll back and 404 as without a membership.
	requireMember := !h.isEffectiveGlobalAdmin(c)
	// Role and page-SSO link of this account end together.
	// The "last owner" check runs in the same transaction (under
	// Postgres with FOR UPDATE on the page's membership rows).
	var ssoRemoved int64
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if lockCheck {
			if err := ensureOtherPageOwner(tx, uint(pageID), uint(userID)); err != nil {
				return err
			}
		}
		if err := deletePageMembership(tx, uint(userID), uint(pageID), requireMember); err != nil {
			return err
		}
		var err error
		ssoRemoved, err = deleteSSOIdentitiesOnPage(tx, uint(userID), uint(pageID))
		return err
	}); err != nil {
		if errors.Is(err, errLastPageOwner) {
			respondLastPageOwner(c)
			return
		}
		if errors.Is(err, errNotPageMember) {
			respondNotPageMember(c)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Entfernen", "code": "server_error"})
		return
	}
	// The audit entry names the links deleted along with it.
	details += fmt.Sprintf(" sso_identities_removed=%d", ssoRemoved)
	h.RecordAudit(c, "statuspage.user_role_removed", "config", "StatusPage", strconv.Itoa(pageID), "", details)
	c.JSON(http.StatusOK, gin.H{"message": "Entfernt"})
}

// mayRemovePageUser checks the permissions for RemoveStatusPageUserHandler and
// responds itself on rejection. A member with more permissions than
// the assigner remains untouched. Without an active membership
// of the target there is no role against which the caller's permissions could be
// checked; removal would then only have deleted arbitrary page-SSO links
// of foreign accounts. Only a Global Admin may do that (cleaning up orphaned
// links), everyone else gets 404 as for an unknown account -
// explicitly including accounts that have pages:delete via a global role but
// are not Global Admin: only a Global Admin cleans up. Unknown account,
// existing non-member and only soft-deleted membership return
// the same status and body (no account enumeration).
func (h *Handler) mayRemovePageUser(c *gin.Context, curErr error, current models.UserStatusPageRole, pageID uint) bool {
	if h.isEffectiveGlobalAdmin(c) {
		return true
	}
	if curErr != nil {
		respondNotPageMember(c)
		return false
	}
	if !roleCoveredByActor(h.actorPagePermissions(c, pageID), current.Role) {
		abortRoleExceedsOwn(c)
		return false
	}
	return true
}

// errNotPageMember: the membership was already gone at deletion time.
var errNotPageMember = errors.New("not a page member")

// deletePageMembership (soft-)deletes the account's membership on the
// page. With requireMember, "nothing deleted" counts as errNotPageMember, so that
// the caller rolls back the transaction (TOCTOU).
func deletePageMembership(tx *gorm.DB, userID, pageID uint, requireMember bool) error {
	res := tx.Where("status_page_id = ? AND user_id = ?", pageID, userID).Delete(&models.UserStatusPageRole{})
	if res.Error != nil {
		return res.Error
	}
	if requireMember && res.RowsAffected == 0 {
		return errNotPageMember
	}
	return nil
}

func respondNotPageMember(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "Benutzer ist kein Mitglied dieser Seite", "code": "user_not_found"})
}

// updatePageUserRole sets the role of an existing member
// (the last owner downgrading themselves -> 409 last_page_owner).
func (h *Handler) updatePageUserRole(c *gin.Context, existing models.UserStatusPageRole, role models.Role, byEmail bool) {
	lockCheck := h.lockOutApplies(c, existing.UserID, existing.Role, &role)
	existing.RoleID = role.ID
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		// Lock the user row first. Otherwise Save() re-creates via upsert the role row removed by the
		// account deletion.
		if err := lockLiveUser(tx, existing.UserID); err != nil {
			return err
		}
		if lockCheck {
			if err := ensureOtherPageOwner(tx, existing.StatusPageID, existing.UserID); err != nil {
				return err
			}
		}
		// Targeted update instead of Save(). With 0 affected rows, Save()
		// issues an upsert and thereby brings back a concurrently removed
		// (soft-deleted) membership. Under Postgres the UPDATE waits
		// for the removal's lock and then re-evaluates deleted_at IS NULL;
		// 0 rows means "member is gone" (404 user_not_found, fail closed).
		// Lock order unchanged: user, then role rows; the removal takes
		// no user lock, so no cycle arises.
		res := tx.Model(&models.UserStatusPageRole{}).
			Where("id = ? AND deleted_at IS NULL", existing.ID).
			Update("role_id", role.ID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found", "code": "user_not_found"})
			return
		}
		if errors.Is(err, errLastPageOwner) {
			respondLastPageOwner(c)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Aktualisieren", "code": "server_error"})
		return
	}
	pageID := strconv.Itoa(int(existing.StatusPageID))
	h.RecordAudit(c, "statuspage.user_role_assigned", "config", "StatusPage", pageID, "", fmt.Sprintf("user_id=%d role_id=%d", existing.UserID, role.ID))
	respondPageUserAssigned(c, http.StatusOK, existing, byEmail)
}

// assignAfterRace handles the unique violation on creation:
// read the existing row, update the role. Since the change, only
// Global Admins create members directly, hence no permission check of its own.
func (h *Handler) assignAfterRace(c *gin.Context, role models.Role, pageID, userID uint) {
	var existing models.UserStatusPageRole
	if err := h.DB.Preload("Role").Where(whereUserIDAndStatusPageID, userID, pageID).First(&existing).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
		return
	}
	h.updatePageUserRole(c, existing, role, false)
}
