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

// hexColorRegex validiert Hex-Farbwerte wie "#007bff" (Branding).
// Leerer String ist erlaubt - er bedeutet "keine eigene Farbe gesetzt", die
// öffentliche Seite fällt dann auf die globalen CSS-Defaults zurück.
var hexColorRegex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// validHeaderStyles sind die vordefinierten Layout-Varianten für den
// Statusseiten-Header. Leerer String ist NICHT erlaubt, da das
// Feld einen "not null default 'classic'"-DB-Default hat.
var validHeaderStyles = map[string]bool{
	"classic":  true,
	"centered": true,
	"minimal":  true,
}

// validPageLayouts sind die wählbaren Seiten-Layouts. Anders als
// bei HeaderStyle ist auch hier der leere String ungültig: Fehlt das Feld im
// Body, bleibt der bisherige Wert bzw. der Default "standard" erhalten (siehe
// applyStatusPageWritableFields), nur ein explizit gesendeter Wert wird geprüft.
var validPageLayouts = map[string]bool{
	"standard": true,
	"liste":    true,
}

// validateBranding prüft die neuen Branding-Felder einer StatusPage
// und liefert bei Bedarf eine deutschsprachige Fehlermeldung, konsistent
// zum restlichen Fehler-Stil dieses Handlers.
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

// maxLogoURLLength begrenzt die gespeicherte Logo-Adresse.
const maxLogoURLLength = 2048

// validLogoURL: Das Logo landet als <img src> im Browser der Besucher. Erlaubt
// sind eine leere Angabe, eine https-URL mit Host oder ein absoluter Pfad auf
// dem eigenen Host. Andere Schemata (javascript:, data:, http:), protokoll-
// relative Adressen und Steuerzeichen werden abgelehnt.
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

// validateNotificationFromAddress prüft das optionale NotificationFromAddress-
// Feld. Leer ist erlaubt (Fallback auf einen Slug-Default, siehe
// resolveNotificationFromAddress). Ist es gesetzt, muss es sich wie
// der bestehende SMTP_FROM-Wert per mail.ParseAddress
// parsen lassen ("Anzeigename <adresse>" oder reine Adresse) und darf keine
// CR/LF enthalten - dieselbe Prüfung, die sendAlertEmail auf smtpCfg.From
// anwendet, hier nur vorgezogen, damit ein Admin sofort eine verständliche
// Fehlermeldung statt eines stillschweigend verworfenen Versands sieht.
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

// resolveNotificationFromAddress liefert den From-Header einer Mail für eine
// Statusseite. Die Adresse (und damit auch der SMTP-Envelope, siehe
// envelopeFrom) gehört immer dem Betreiber: sie stammt aus operatorFrom
// (SMTP_FROM). Die Statusseite steuert nur den Anzeigenamen bei. Eine frei
// wählbare Absenderadresse ließe den Server als Relais mit gefälschtem
// Absender missbrauchen. Die Adresse der Seite (NotificationFromAddress) wirkt
// nur als Reply-To, siehe resolveNotificationReplyTo.
func resolveNotificationFromAddress(operatorFrom string, page models.StatusPage) string {
	op, err := mail.ParseAddress(operatorFrom)
	if err != nil {
		// Nicht parsebare Betreiberkonfiguration: unverändert lassen, der
		// SMTP-Server lehnt sie wie bisher ab.
		return operatorFrom
	}
	name := sanitizeDisplayName(page.Name)
	if name == "" {
		return op.String()
	}
	return (&mail.Address{Name: name, Address: op.Address}).String()
}

// sanitizeDisplayName entfernt Steuerzeichen (u. a. CR/LF) aus einem
// Anzeigenamen und kürzt ihn.
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

// resolveNotificationReplyTo liefert die Antwortadresse der Statusseite für
// den Reply-To-Header ("" ohne gültige Angabe).
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

// frontendURLHost liefert den Hostnamen aus FRONTEND_URL (z.B.
// "example.com"), mit "localhost" als Fallback, falls FRONTEND_URL nicht
// gesetzt oder nicht parsebar ist (z.B. im lokalen Dev-Setup, wo FRONTEND_URL
// auf localhost zeigt).
func frontendURLHost() string {
	if u, err := url.Parse(getFrontendURL()); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "localhost"
}

// Wiederholte Literale (go:S1192).
const (
	// slugWhereClause ist die GORM-Where-Bedingung für einen Slug-Lookup, an
	// mehreren Stellen wiederverwendet, statt den Literal-String zu duplizieren.
	slugWhereClause            = "slug = ?"
	whereUserIDAndStatusPageID = "user_id = ? AND status_page_id = ?"
	whereStatusPageIDEquals    = "status_page_id = ?"
)

// validSlugRegex erlaubt nur Kleinbuchstaben, Ziffern und einzelne Bindestriche
// als Trenner (kein führender/nachgestellter/doppelter Bindestrich) - dieselbe
// Zeichenmenge, die slugify() aus einem Statusseiten-Namen erzeugt, damit
// manuell eingegebene und automatisch generierte Slugs demselben Format folgen.
var validSlugRegex = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// slugSanitizeRegex ersetzt jede Folge nicht erlaubter Zeichen durch genau
// einen Bindestrich.
var slugSanitizeRegex = regexp.MustCompile(`[^a-z0-9]+`)

// slugify wandelt einen Statusseiten-Namen in einen URL-sicheren
// Vorschlag für den Slug um, unter dem die Seite dann automatisch als
// <slug>.<Basisdomain> erreichbar ist (siehe PublicRoot.vue, isOwnBaseDomain).
// Deutsche Umlaute werden transliteriert statt einfach entfernt, damit z.B.
// "Müller GmbH" nicht zu "mller-gmbh" wird.
func slugify(name string) string {
	s := strings.ToLower(name)
	umlautReplacer := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")
	s = umlautReplacer.Replace(s)
	s = slugSanitizeRegex.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// slugTakenMessage ist die 409-Meldung bei Slug-Kollision (Anlegen und Ändern).
const slugTakenMessage = "Slug wird bereits von einer anderen Statusseite verwendet"

// slugReservedMessage: 400-Meldung für reservierte Slugs.
const slugReservedMessage = "Dieser Slug ist reserviert und kann nicht verwendet werden"

// uniqueSlug hängt bei einer Kollision mit einem bestehenden Slug "-2", "-3", …
// an, bis ein freier Slug gefunden ist. Wird nur für automatisch generierte
// Slugs verwendet (leeres Slug-Feld im Formular) - ein manuell eingegebener
// Slug soll bei Kollision stattdessen einen klaren 409-Fehler auslösen, siehe
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

// Das Verzeichnis ist anonym lesbar und wuchs mit der Zahl der Seiten und der
// Länge von Name und Beschreibung ohne Grenze. Jetzt liefert ein Aufruf höchstens
// maxPublicDirectoryLimit Seiten (limit/offset zum Blättern); Name und
// Beschreibung erscheinen gekürzt, die Seite selbst zeigt den vollen Text.
const (
	maxPublicDirectoryLimit  = 200
	maxPublicDirectoryOffset = 100000
	publicDirectoryNameMax   = 120
	publicDirectoryDescMax   = 500
)

// truncateRunes kürzt s auf höchstens max Zeichen; eine Kürzung endet mit „…“.
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
	// substr schon in der Abfrage, damit eine riesige Beschreibung nicht erst
	// geladen und dann gekürzt wird; ein Zeichen mehr als erlaubt zeigt die Kürzung.
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

// publicStatusPage ist die öffentliche Darstellung einer Statusseite
// Die öffentlichen Endpunkte (Verzeichnis,
// Dashboard, Slug-Lookup) serialisierten bisher das komplette Model inkl.
// interner Mandanten-Konfiguration (AutoJoinEmailDomains, DefaultRoleID,
// NotificationFromAddress, CustomDomain/-Status, IsPrivate, ...). Das DTO
// enthält genau die Felder, die das öffentliche Frontend liest
// (LandingPage.vue: ID/Name/Slug/Description, StatusPage.vue zusätzlich
// Logo, Farben, Header-Stil und Layout). JSON-Namen bleiben wie beim Model,
// damit das Frontend unverändert weiterläuft. Admin-Endpunkte liefern
// weiterhin das volle Model.
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

// autoAssignDefaultRole legt bei fehlender UserStatusPageRole
// automatisch eine mit page.DefaultRoleID an - mandantenübergreifendes
// Self-Service-Onboarding: JEDER eingeloggte Account erhält beim ersten
// Aufruf einer privaten Seite mit gesetztem DefaultRoleID automatisch Zugriff.
// Das gilt nur noch für Konten, deren E-Mail-Domain in
// AutoJoinEmailDomains der Seite steht (sonst könnte sich jeder per
// Selbstregistrierung Zugriff verschaffen). Ist DefaultRoleID oder die
// Allowlist leer, gilt "nur explizit eingeladene Nutzer" und der Zugriff
// wird verweigert.
func (h *Handler) autoAssignDefaultRole(c *gin.Context, user models.User, page models.StatusPage) (int, string, bool) {
	userID := user.ID
	if page.DefaultRoleID == nil || !emailMatchesAutoJoin(user.Email, page.AutoJoinEmailDomains) {
		return http.StatusForbidden, "Access denied", false
	}

	// Die Rolle wird beim Beitritt erneut geprüft.
	// Sie kann nach dem Speichern der Seite um Schreib- oder Abonnentenrechte
	// erweitert oder gelöscht worden sein; Bestandsseiten wurden nie geprüft.
	var defaultRole models.Role
	if err := h.DB.First(&defaultRole, *page.DefaultRoleID).Error; err != nil || !roleSafeAsDefault(defaultRole.Permissions) {
		return http.StatusForbidden, "Access denied", false
	}

	// ein Soft-Delete durch RemoveStatusPageUserHandler (gezielte,
	// korrekt auditierte Admin-Entscheidung) ist für die gescopte Query oben in
	// checkPrivatePageAccess nicht von "hatte nie eine Rolle" unterscheidbar.
	// Ohne diesen Unscoped()-Check würde ein einfacher Seitenaufruf die
	// Entfernung stillschweigend rückgängig machen.
	var removed models.UserStatusPageRole
	if err := h.DB.Unscoped().Where(whereUserIDAndStatusPageID, userID, page.ID).First(&removed).Error; err == nil {
		return http.StatusForbidden, "Access denied", false
	}

	newRole := models.UserStatusPageRole{
		UserID:       userID,
		StatusPageID: page.ID,
		RoleID:       *page.DefaultRoleID,
	}
	if err := h.DB.Create(&newRole).Error; err != nil {
		// Die SPA fragt beim ersten Aufruf parallel an; die Anfrage, die
		// am Unique-Index verliert, hat trotzdem Zugriff (die Mitgliedschaft existiert).
		if isUniqueViolation(err) {
			return 0, "", true
		}
		return http.StatusInternalServerError, "Fehler bei der Rollenzuweisung", false
	}
	// Bisher komplett ohne Audit-Trail, obwohl Rollenvergabe - auch die
	// automatische - genauso protokolliert gehört wie die manuelle
	// (AddStatusPageUserHandler: "statuspage.user_role_assigned").
	h.RecordAudit(c, "statuspage.user_role_auto_assigned", "config", "StatusPage",
		strconv.FormatUint(uint64(page.ID), 10), "", "")
	return 0, "", true
}

// checkPrivatePageAccess prüft, ob der eingeloggte User Zugriff auf eine
// private Statusseite hat (Global Admin, bestehende Rolle oder Auto-Join per
// DefaultRoleID). Ausgelagert aus GetPublicDashboardHandler, damit dessen
// kognitive Komplexität die SonarCloud-Grenze (go:S3776) nicht überschreitet.
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
	// weder Lesezugriff noch Auto-Join außerhalb der SSO-Seite.
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

// authorizePublicPageRead prüft den Lesezugriff auf die öffentliche Sicht einer
// Statusseite und schreibt bei Ablehnung selbst die Fehlerantwort (false).
// ein Subscriber-Token, das zu genau dieser privaten Statusseite
// gehört, gewährt Lesezugriff ohne User-Login - z.B. für einen Abonnenten
// ohne eigenes Konto, der dem Link in einer Benachrichtigungsmail folgt.
// Sonst gilt bei privaten Seiten die Rollenprüfung. Gemeinsam genutzt von
// Dashboard und Verfügbarkeit, damit beide dieselbe Regel haben.
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

// writePublicDashboard lädt Components/Incidents/Notices einer bereits als
// zugriffsberechtigt geprüften StatusPage und schreibt die Dashboard-Antwort.
// Ausgelagert aus GetPublicDashboardHandler, damit sowohl der reguläre
// Session-Pfad als auch der Subscriber-Token-Pfad denselben
// Antwortaufbau verwenden.
func (h *Handler) writePublicDashboard(c *gin.Context, page models.StatusPage) {
	// Fetch components
	all := []models.Component{}
	h.DB.Where(whereStatusPageIDEquals, page.ID).Order("sort_order asc").Find(&all)
	// unsichtbare Komponenten (samt Nachfahren) erscheinen weder im
	// Baum noch in den Komponenten der Vorfälle; id-Menge vor dem Filtern merken.
	hidden := hiddenComponentIDs(all)
	all = withoutHiddenComponents(all)
	// der auf der öffentlichen Statusseite angezeigte Status
	// einer Komponente wird hier - nur für diese Antwort, nicht in der DB -
	// mit offenen zugeordneten Vorfällen überlagert, damit ein offener
	// Vorfall nicht weiterhin "operational" vorgaukelt. Der admin-seitige
	// GetComponentsHandler bleibt bewusst unverändert und liefert den
	// manuell gesetzten Status, damit das Admin-Formular beim Speichern
	// keinen abgeleiteten Status versehentlich als neuen manuellen Status
	// persistiert (siehe effectiveComponentStatus in component.go).
	openComponentIDs := h.componentIDsWithOpenIncidents(page.ID)
	// effective_status/external_sources; Status behält die
	// bisherige Bedeutung (gespeichert + offene Vorfälle), Teil D zeigt
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

	// Vorfälle: alle offenen (höchstens 200) plus die neuesten behobenen
	// (incident_limit, Standard 50, höchstens 200); ältere lädt die Oberfläche
	// seitenweise über GET /api/pages/:page_id/incidents nach.
	incidents, more, ok := h.dashboardIncidents(c, page.ID)
	if !ok {
		return
	}
	stripHiddenIncidentComponents(incidents, hidden)

	// Fetch active General Notices: IsActive muss true sein, und
	// falls StartAt/EndAt gesetzt sind, muss "jetzt" innerhalb liegen. Neueste
	// zuerst, analog zu Incidents.
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
		// incidents_more: es gibt ältere behobene Vorfälle als die gelieferten.
		"incidents_more": more,
		"notices":        notices,
		// gespiegelte Meldungen externer Quellen (Teil D).
		"external_incidents": h.publicExternalIncidents(page.ID),
	})
}

// dashboardIncidents lädt die Vorfälle der öffentlichen Sicht: alle offenen und
// die neuesten behobenen, beide begrenzt. more meldet weitere behobene.
// Bei einem ungültigen incident_limit (400) oder einem Datenbankfehler (500)
// ist die Antwort geschrieben (ok=false).
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
	// Fehler brechen ab (500), statt eine scheinbar leere Statusseite zu zeigen.
	if err := base().Where("status NOT IN ?", models.ResolvedIncidentStatuses).Limit(maxIncidentListLimit).Find(&incidents).Error; err != nil {
		slog.Error("Dashboard: offene Vorfälle nicht lesbar", "page", pageID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch incidents", "code": "server_error"})
		return nil, false, false
	}
	resolved := []models.Incident{}
	// Eine Zeile mehr als verlangt zeigt, ob es noch ältere gibt.
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

// publicLanguage ist eine für die Statusseite aktivierte Sprache:
// Code, Eigenbezeichnung aus dem Sprachkatalog und Standardmarkierung.
type publicLanguage struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	// IsActive: im Sprachkatalog aktiv. Der Sprachumschalter zeigt
	// weiter alle Seitensprachen, die Abo-Auswahlen nur aktive.
	IsActive bool `json:"is_active"`
}

// publicPageLanguages liefert die Sprachen der Seite (nach Code sortiert) für
// den öffentlichen Sprachumschalter. Fehler ergeben eine leere Liste: der
// Umschalter fällt dann auf die Oberflächensprachen zurück, das Dashboard
// bleibt nutzbar.
func (h *Handler) publicPageLanguages(pageID uint) []publicLanguage {
	rows := []models.StatusPageLanguage{}
	h.DB.Preload("Language").Where(whereStatusPageIDEquals, pageID).Order("language_code asc").Find(&rows)
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

// GET /api/pages/slug/:slug — einzelne Statusseite per Slug
//
// dieser Endpunkt läuft öffentlich (keine AuthMiddleware, main.go
// hängt aber OptionalAuthMiddleware ein) und lieferte private Seiten bisher
// ungeprüft aus - wer den Slug kennt/errät, bekam Name/Beschreibung/
// CustomDomain auch ohne Berechtigung. Bei page.IsPrivate ohne gültige
// Berechtigung wird jetzt konsequent 404 zurückgegeben (wie bei einem nicht
// existierenden Slug), damit ein Aufrufer nicht unterscheiden kann, ob die
// Seite gar nicht existiert oder nur privat ist.
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

	// öffentlicher Endpunkt, daher nur das DTO.
	c.JSON(http.StatusOK, toPublicStatusPage(page))
}

// statusPageWritableFields ist das Bind-Ziel für Create/Update.
// Bewusst ein eigenes DTO statt direkt in models.StatusPage zu binden: die
// Model-Struct bettet gorm.Model ein, dessen ID-Feld kein json:"-" trägt -
// ein Request-Body mit z.B. {"ID": 7, ...} würde sonst page.ID überschreiben
// und Save() damit auf eine andere (fremde) Statusseite umlenken, unabhängig
// vom :page_id in der URL. Das DTO enthält nur die tatsächlich vom Client
// änderbaren Felder, WebhookTokenHash (json:"-") bleibt zusätzlich ausgeschlossen.
// CustomDomain/CustomDomainStatus sind aus demselben Grund NICHT hier
// enthalten: sie laufen ausschließlich über die dedizierten
// Endpunkte in custom_domain.go, damit ein Domain-Wechsel immer zuverlässig
// den Verifikationsstatus auf "pending" zurücksetzt, statt über diesen
// generischen Bind versehentlich mit einer stehengebliebenen "verified"/
// "active"-Markierung kombiniert werden zu können.
type statusPageWritableFields struct {
	Name            string `json:"Name"`
	Slug            string `json:"Slug"`
	Description     string `json:"Description"`
	LogoURL         string `json:"LogoURL"`
	IsPublicIndexed bool   `json:"IsPublicIndexed"`
	IsPrivate       bool   `json:"IsPrivate"`
	DefaultRoleID   *uint  `json:"DefaultRoleID"`
	// AutoJoinEmailDomains: siehe models.StatusPage.
	AutoJoinEmailDomains string `json:"AutoJoinEmailDomains"`
	PrimaryColor         string `json:"PrimaryColor"`
	SecondaryColor       string `json:"SecondaryColor"`
	HeaderStyle          string `json:"HeaderStyle"`
	// PageLayout: Zeiger, damit "Feld fehlt" (Wert behalten) von
	// einem explizit gesendeten leeren Wert (400) unterscheidbar bleibt.
	PageLayout *string `json:"PageLayout"`
	// NotificationFromAddress: siehe validateNotificationFromAddress.
	NotificationFromAddress string `json:"NotificationFromAddress"`
}

func applyStatusPageWritableFields(page *models.StatusPage, in statusPageWritableFields) {
	page.Name = in.Name
	page.Slug = in.Slug
	page.Description = in.Description
	page.LogoURL = in.LogoURL
	page.IsPrivate = in.IsPrivate
	// private Seiten dürfen nie öffentlich gelistet sein.
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

// userHasRoleOnPrivatePage prüft Global-Admin-Status oder eine bestehende
// UserStatusPageRole für pageID - bewusst OHNE den Auto-Join-Seiteneffekt aus
// GetPublicDashboardHandler (DefaultRoleID würde dort bei fehlender Rolle
// automatisch eine neue UserStatusPageRole anlegen). Für einen reinen
// Metadaten-Read-Endpunkt wie GetStatusPageBySlugHandler soll das Aufrufen
// selbst keine Rollen-Zuweisung als Nebeneffekt auslösen.
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

// userHasPermissionOnPrivatePage prüft, ob der eingeloggte User auf pageID
// eine Rolle mit genau der übergebenen Permission hat (oder Global Admin
// ist) - anders als userHasRoleOnPrivatePage genügt nicht "irgendeine
// Rolle". Das wäre nach Auto-Join (StatusPage.DefaultRoleID) für JEDEN
// eingeloggten User wahr, unabhängig davon, was die zugewiesene Rolle
// tatsächlich darf (Sonderfall Auto-Join, per unabhängigem Review
// gefunden). Wiederverwendbar für
// künftige Fälle, die dieselbe Berechtigungs- statt Rollenprüfung brauchen;
// spiegelt dieselbe Permission-String-Logik wie middleware.RequirePermission,
// nur als aufrufbare Funktion statt Middleware (die dortige roleHasPermission
// ist unexported und nicht paketübergreifend nutzbar).
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

// pageIsAccessible ist der gemeinsame Zugriffs-Check für alle page_id-/
// globale-ID-Endpunkte, die dieselben Daten wie GetPublicDashboardHandler
// liefern (Components, Incidents, Notices, Translations): öffentliche Seiten
// sind immer lesbar, private Seiten über checkPrivatePageAccess (Global
// Admin, bestehende Rolle, oder Auto-Join per DefaultRoleID). Der Auto-Join
// galt bisher nur für die Dashboard-Route - Deep Links und direkte
// API-Aufrufe auf dieselben Daten sollen ihn genauso auslösen, sonst würde
// ein Nutzer je nach Einstiegspunkt (Dashboard vs. Deep Link) unterschiedlich
// behandelt.
func (h *Handler) pageIsAccessible(c *gin.Context, page models.StatusPage) bool {
	if !page.IsPrivate {
		return true
	}
	_, _, ok := h.checkPrivatePageAccess(c, page)
	return ok
}

// respondNotVisible beantwortet den Zugriff auf etwas, das der Aufrufer nicht
// sehen darf, genau wie den Zugriff auf etwas, das es nicht gibt: 404 mit dem
// Text und Code, den die Route für ein fehlendes Ziel verwendet. Würde eine
// private Seite mit 401 oder 403 antworten, ließe sich anonym erkennen, welche
// Slugs und IDs zu privaten Seiten gehören.
func respondNotVisible(c *gin.Context, message, code string) {
	c.JSON(http.StatusNotFound, gin.H{"error": message, "code": code})
}

// respondPageNotVisible: die Antwort für „Seite fehlt“ und „Seite privat, keine
// Berechtigung“ der Routen, die eine Seite über Slug oder page_id ansprechen.
func respondPageNotVisible(c *gin.Context) {
	respondNotVisible(c, "Status page not found", "page_not_found")
}

// requirePageVisible prüft den Lesezugriff auf eine Seite (wie pageIsAccessible,
// samt Auto-Join) und antwortet bei Ablehnung mit der Antwort der Route für
// ein fehlendes Ziel. false: die Antwort ist geschrieben.
func (h *Handler) requirePageVisible(c *gin.Context, page models.StatusPage, notFoundMessage, notFoundCode string) bool {
	if h.pageIsAccessible(c, page) {
		return true
	}
	respondNotVisible(c, notFoundMessage, notFoundCode)
	return false
}

// POST /api/pages — neue Statusseite anlegen
func (h *Handler) CreateStatusPageHandler(c *gin.Context) {
	var in statusPageWritableFields
	if err := c.ShouldBindJSON(&in); err != nil {
		respondInvalidInput(c)
		return
	}
	page := models.StatusPage{PageLayout: "standard"}
	applyStatusPageWritableFields(&page, in)
	// ein Name ist Pflicht (Frontend prüfte es, das Backend legte
	// sonst Seiten ohne Namen an).
	page.Name = strings.TrimSpace(page.Name)
	if page.Name == "" {
		abortInvalidInput(c, codeNameRequired, "Name darf nicht leer sein")
		return
	}
	if !requirePageNameLength(c, page.Name) {
		return
	}

	// ein leeres Slug-Feld wird automatisch aus dem Namen generiert
	// (die Seite ist dann sofort unter <slug>.<Basisdomain> erreichbar, DNS-
	// Wildcard + TLS laufen bereits über Caddy). Ein manuell eingegebener Slug
	// wird dagegen strikt validiert und bei Kollision klar zurückgewiesen statt
	// dem Nutzer nur den generischen 500er des DB-uniqueIndex zu zeigen.
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
		// Race zwischen Vorabprüfung und Insert -> 409 wie oben.
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": slugTakenMessage, "code": "slug_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Erstellen", "code": "server_error"})
		return
	}
	// GORM ersetzt das Nullwert-false durch default:true. Bei privaten
	// Seiten muss "nicht gelistet" deshalb nach dem Anlegen explizit gesetzt werden.
	if page.IsPrivate {
		if err := h.DB.Model(&page).Update("is_public_indexed", false).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Erstellen", "code": "server_error"})
			return
		}
	}
	// jede neue Statusseite bekommt dieselbe de+en/de-Default-
	// Sprachkonfiguration wie der Backfill für Bestandsseiten (siehe
	// database.backfillStatusPageLanguages), damit sich neue und bestehende
	// Seiten nicht unterscheiden. Ein Fehler hier darf das Anlegen der Seite
	// selbst nicht verhindern - isValidNotificationLanguage fällt ohnehin auf
	// dieselbe en/de-Konvention zurück, solange keine StatusPageLanguage-Zeile
	// existiert (siehe handlers/notification_template_admin.go).
	if err := database.SeedDefaultStatusPageLanguages(h.DB, page.ID); err != nil {
		slog.Error("Failed to seed default languages for new status page", "status_page_id", page.ID, "error", err)
	}
	h.RecordAudit(c, "statuspage.created", "config", "StatusPage", strconv.FormatUint(uint64(page.ID), 10), page.Name, "")
	c.JSON(http.StatusCreated, page)
}

// PUT /api/admin/pages/:page_id — Statusseite aktualisieren
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
	// Defensive: das DTO hat ohnehin kein ID-Feld, page.ID kann also durch den
	// Bind oben gar nicht überschrieben werden - trotzdem hart auf den
	// URL-Wert zurückgesetzt, damit dieser Invariant auch bei künftigen
	// Änderungen an statusPageWritableFields nicht stillschweigend wegfällt.
	page.ID = uint(id)
	// Name bleibt Pflicht (siehe CreateStatusPageHandler).
	page.Name = strings.TrimSpace(page.Name)
	if page.Name == "" {
		abortInvalidInput(c, codeNameRequired, "Name darf nicht leer sein")
		return
	}
	if !requirePageNameLength(c, page.Name) {
		return
	}

	// derselbe Kollisions-Check wie beim Anlegen, falls der Slug
	// (und damit die Subdomain) nachträglich geändert wird.
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
	// reservierte Slugs nur beim Ändern des Slugs sperren; eine
	// bestehende Seite (auch die Systemseite mit "system") darf ihren Slug
	// behalten und sonst weiter gespeichert werden.
	if slug != originalSlug && isReservedSlug(slug) {
		abortInvalidInput(c, codeSlugReserved, slugReservedMessage)
		return
	}
	// Der Slug der Systemseite ist fest (öffentliche URL, Dogfooding).
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

// DELETE /api/admin/pages/:page_id — Statusseite löschen
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
	// Die Systemseite bildet den Zustand dieser
	// Installation selbst ab und wird bei jedem Start von
	// database.SeedSystemStatusPage erwartet. Löschen ist deshalb gesperrt statt
	// still erlaubt: Ein Neuanlegen beim nächsten Start hätte eine leere Seite
	// mit neuer ID ergeben, Komponenten, Abos und Rollen der alten wären verloren.
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

// GET /api/admin/pages/:page_id/users — Benutzer einer Seite auflisten
// Nur die für die Verwaltung nötigen Felder (ID, E-Mail, Rollenname),
// nie SSO-Kennung, Admin-Flag oder Rechte-Liste der Konten.
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

// POST /api/admin/pages/:page_id/users — Benutzer zu einer Seite hinzufügen (oder Rolle ändern)
//
// Nicht-Global-Admins laden per E-Mail ein (page_invitation.go),
// die Mitgliedschaft entsteht erst mit der Zustimmung des Kontoinhabers.
//
// user_id und role_id werden gegen die Datenbank geprüft.
// Ein Global Admin wählt das Konto per user_id (oder E-Mail). Alle anderen
// (z. B. Page Owner) dürfen kein beliebiges Konto per ID einschleusen: Sie
// ändern die Rolle bestehender Mitglieder dieser Seite oder fügen ein Konto
// per E-Mail hinzu; die Antwort ist dort für bekannte und unbekannte Adressen
// gleich (keine Kontenabfrage). Die vergebene Rolle darf nie mehr Rechte haben
// als die des Zuweisenden auf dieser Seite.
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

	// Formfehler (ohne role_id) sind kein Rollen-Orakel: 400 statt 403 (Regression,
	// aufgefallen an den Router-Tests TestRoutePermissions/TestRouter_SSOSession).
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

	// Rollenbezogene Prüfungen laufen für Nicht-Global-Admins VOR der Konto-
	// Suche und antworten für fehlende, Global-Admin- und nicht abgedeckte
	// Rollen gleich: Weder die Adresse noch vorhandene Rollen-IDs lassen sich
	// so erraten (Review).
	var role models.Role
	roleErr := h.DB.First(&role, input.RoleID).Error
	var actorPerms string
	if !isGA {
		actorPerms = h.actorPagePermissions(c, uint(pageID))
		globalOnly, goErr := h.globalOnlyRoleIDs(h.DB, []uint{role.ID})
		if goErr != nil {
			// O2: DB-Fehler der Rollenprüfung sperren (fail closed).
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

	// Ein User kann pro Statusseite immer nur eine Rolle haben. Ohne diesen Check
	// würde ein erneutes Zuweisen (z.B. Upgrade von Viewer auf Editor) eine zweite
	// Zeile anlegen; RequirePermission liest per First() aber immer die älteste
	// Zeile, sodass die neu vergebene Rolle nie greifen würde.
	target, found, status := h.resolvePageUserTarget(c, isGA, uint(pageID), input.UserID, input.Email)
	if status != 0 {
		return
	}
	byEmail := input.UserID == 0 && !isGA

	// Gleiche Abfragen für bekannte und unbekannte Adressen (ID 0 findet nichts).
	var existing models.UserStatusPageRole
	err = h.DB.Preload("Role").Where(whereUserIDAndStatusPageID, target.ID, pageID).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
		return
	}
	isMember := err == nil
	if !isGA && !isMember {
		// Ein Nicht-Global-Admin hängt kein Konto mehr direkt an.
		// Jede Adresse, die noch kein Mitglied ist (mit, ohne oder mit nicht
		// anhängbarem Konto), bekommt eine Einladung und dieselbe Antwort; nur ein
		// gefundenes Konto (found) erhält die Mail. Per user_id (ein gerade
		// entferntes Mitglied) gibt es keinen Weg an der Einladung vorbei.
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
		// "Global Admin" ist keine Seitenrolle (siehe pageRolesAssignable).
		if !h.pageRolesAssignable(target.ID, []PageRoleInput{{StatusPageID: uint(pageID), RoleID: input.RoleID}}) {
			abortGlobalAdminRoleNotAssignable(c)
			return
		}
	} else if isMember && !roleCoveredByActor(actorPerms, existing.Role) {
		// Mitglied mit stärkerer (oder gelöschter) Rolle: auf dem E-Mail-Weg
		// neutral und ohne Änderung, sonst 403 (das Mitglied ist sichtbar).
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

	// Ab hier nur noch Global Admins (alle anderen laden oben ein).
	entry := models.UserStatusPageRole{
		UserID:       target.ID,
		StatusPageID: uint(pageID),
		RoleID:       input.RoleID,
	}
	// Benutzerzeile sperren, bevor die Rolle entsteht; ein gleichzeitig
	// gelöschtes Konto bekommt keine Rolle mehr (404 statt 201).
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
			// Ein gleichzeitiger Aufruf war schneller (Unique-Index
			// user_id + status_page_id). Das Hinzufügen ist idempotent: das nun
			// vorhandene Mitglied bekommt die angeforderte Rolle, statt 500.
			h.assignAfterRace(c, role, uint(pageID), target.ID)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "statuspage.user_role_assigned", "config", "StatusPage", strconv.Itoa(pageID), "", fmt.Sprintf("user_id=%d role_id=%d", target.ID, input.RoleID))
	respondPageUserAssigned(c, http.StatusCreated, entry, false)
}

// DELETE /api/admin/pages/:page_id/users/:uid — Benutzer von einer Seite entfernen
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
	// Jeder DB-Fehler außer "nicht gefunden" bricht ab (fail closed), die
	// Rechteprüfung folgt in mayRemovePageUser.
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
	// Review: Zwischen Prüfung und Löschen kann die
	// Mitgliedschaft schon weg sein (TOCTOU). Ein Nicht-Global-Admin darf dann
	// keine Verknüpfungen mehr löschen: Rückrollen und 404 wie ohne Mitgliedschaft.
	requireMember := !h.isEffectiveGlobalAdmin(c)
	// Rolle und Seiten-SSO-Verknüpfung dieses Kontos enden gemeinsam.
	// Die Prüfung "letzter Owner" läuft in derselben Transaktion (unter
	// Postgres mit FOR UPDATE auf die Mitgliedszeilen der Seite).
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
	// Der Audit-Eintrag nennt die mitgelöschten Verknüpfungen.
	details += fmt.Sprintf(" sso_identities_removed=%d", ssoRemoved)
	h.RecordAudit(c, "statuspage.user_role_removed", "config", "StatusPage", strconv.Itoa(pageID), "", details)
	c.JSON(http.StatusOK, gin.H{"message": "Entfernt"})
}

// mayRemovePageUser prüft die Rechte für RemoveStatusPageUserHandler und
// antwortet bei Ablehnung selbst. Ein Mitglied mit mehr Rechten als
// der Zuweisende bleibt unberührt. Ohne aktive Mitgliedschaft
// des Ziels gibt es keine Rolle, gegen die sich die Rechte des Aufrufers prüfen
// ließen; das Entfernen hätte dann nur noch beliebige Seiten-SSO-Verknüpfungen
// fremder Konten gelöscht. Das darf nur ein Global Admin (Aufräumen verwaister
// Verknüpfungen), alle anderen bekommen 404 wie bei einem unbekannten Konto –
// ausdrücklich auch Konten, die pages:delete über eine globale Rolle haben, aber
// kein Global Admin sind: Nur ein Global Admin räumt auf. Unbekanntes Konto,
// existierendes Nicht-Mitglied und nur soft-gelöschte Mitgliedschaft liefern
// denselben Status und Body (keine Aufzählung von Konten).
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

// errNotPageMember: Die Mitgliedschaft war beim Löschen schon weg.
var errNotPageMember = errors.New("not a page member")

// deletePageMembership löscht (soft) die Mitgliedschaft des Kontos auf der
// Seite. Mit requireMember gilt "nichts gelöscht" als errNotPageMember, damit
// der Aufrufer die Transaktion zurückrollt (TOCTOU).
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

// updatePageUserRole setzt die Rolle eines bestehenden Mitglieds (
// Herabstufen des letzten Owners durch sich selbst -> 409 last_page_owner).
func (h *Handler) updatePageUserRole(c *gin.Context, existing models.UserStatusPageRole, role models.Role, byEmail bool) {
	lockCheck := h.lockOutApplies(c, existing.UserID, existing.Role, &role)
	existing.RoleID = role.ID
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		// Benutzerzeile zuerst sperren. Sonst legt Save() die von der
		// Kontolöschung entfernte Rollenzeile per Upsert wieder an.
		if err := lockLiveUser(tx, existing.UserID); err != nil {
			return err
		}
		if lockCheck {
			if err := ensureOtherPageOwner(tx, existing.StatusPageID, existing.UserID); err != nil {
				return err
			}
		}
		// Gezieltes Update statt Save(). Save() setzt bei 0 getroffenen
		// Zeilen einen Upsert ab und holt damit eine gleichzeitig entfernte
		// (soft-gelöschte) Mitgliedschaft zurück. Das UPDATE wartet unter Postgres
		// auf die Sperre des Entfernens und wertet danach deleted_at IS NULL neu
		// aus; 0 Zeilen heißt "Mitglied ist weg" (404 user_not_found, fail closed).
		// Lock-Ordnung unverändert: Benutzer, dann Rollenzeilen; das Entfernen nimmt
		// keine Benutzersperre, es entsteht kein Zyklus.
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

// assignAfterRace behandelt die Unique-Verletzung beim Anlegen:
// vorhandene Zeile lesen, Rolle aktualisieren. Seit der Umstellung legen nur noch
// Global Admins direkt Mitglieder an, deshalb ohne eigene Rechteprüfung.
func (h *Handler) assignAfterRace(c *gin.Context, role models.Role, pageID, userID uint) {
	var existing models.UserStatusPageRole
	if err := h.DB.Preload("Role").Where(whereUserIDAndStatusPageID, userID, pageID).First(&existing).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Hinzufügen", "code": "server_error"})
		return
	}
	h.updatePageUserRole(c, existing, role, false)
}
