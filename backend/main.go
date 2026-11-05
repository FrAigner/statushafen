package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/metrics"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// trustedProxiesFromEnv liest TRUSTED_PROXIES (kommagetrennte IPs/CIDRs) aus
// der Umgebung (Review-Befund B3). gin.Default() vertraut ohne explizites
// SetTrustedProxies() per Default 0.0.0.0/0, also JEDEM Absender - ein
// Angreifer konnte dadurch mit einer frei gewählten X-Forwarded-For-Kopfzeile
// pro Request einen neuen Rate-Limiter-Bucket erzwingen (Login-/Subscribe-
// Brute-Force-Schutz umgangen) und beliebige Audit-Log-IPs vortäuschen.
// Ohne gesetzten Wert werden GAR KEINE Proxys vertraut - ClientIP() fällt
// dann auf die tatsächliche TCP-Peer-Adresse zurück statt X-Forwarded-For
// ungeprüft zu übernehmen. Läuft die App hinter einem bekannten Reverse-Proxy
// (z.B. Render-Edge) mit fester Absenderadresse, TRUSTED_PROXIES auf dessen
// IP(s)/CIDR(s) setzen, damit ClientIP() wieder die echte Besucher-IP liefert.
func trustedProxiesFromEnv() []string {
	raw := os.Getenv("TRUSTED_PROXIES")
	if raw == "" {
		return nil
	}
	var proxies []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	return proxies
}

// Kleinste erlaubte Präfixlängen vertrauter Proxy-Netze. Ein Netz davor (bis hin
// zu 0.0.0.0/0 und ::/0) lässt praktisch jeden Absender Weiterleitungs-Header
// setzen; Reverse-Proxys stehen in kleinen, bekannten Netzen.
const (
	minTrustedProxyPrefixV4 = 8
	minTrustedProxyPrefixV6 = 16
)

// overlyWideTrustedProxies liefert die Einträge von TRUSTED_PROXIES, deren Netz
// weiter ist als erlaubt. Ungültige Einträge meldet SetTrustedProxies selbst.
func overlyWideTrustedProxies(entries []string) []string {
	var wide []string
	for _, e := range entries {
		e = strings.TrimSpace(e)
		prefix, err := netip.ParsePrefix(e)
		if err != nil {
			continue // einzelne Adresse oder ungültig
		}
		bits, addr := prefix.Bits(), prefix.Addr()
		// ::ffff:a.b.c.d/n ist ein IPv4-Netz mit n-96 Bit.
		if addr.Is4In6() {
			bits -= 96
			addr = addr.Unmap()
		}
		if (addr.Is4() && bits < minTrustedProxyPrefixV4) || (addr.Is6() && bits < minTrustedProxyPrefixV6) {
			wide = append(wide, e)
		}
	}
	return wide
}

// checkTrustedProxyWidth bricht den Start ab, wenn TRUSTED_PROXIES zu weite
// Netze enthält. In der Entwicklung (APP_ENV-Allowlist) bleibt es bei einer
// deutlichen Warnung, damit lokale Docker-Netze nicht stören.
func checkTrustedProxyWidth(entries []string) {
	wide := overlyWideTrustedProxies(entries)
	if len(wide) == 0 {
		return
	}
	if database.IsDevelopmentAppEnv() {
		slog.Warn("TRUSTED_PROXIES vertraut zu weiten Netzen: jeder Absender darf X-Forwarded-For setzen, IP-Limits sind wirkungslos",
			"entries", wide)
		return
	}
	logging.Fatal("TRUSTED_PROXIES vertraut zu weiten Netzen", "entries", wide,
		"hint", "nur die Adressen des eigenen Reverse-Proxys eintragen (IPv4 mindestens /8, IPv6 mindestens /16)")
}

// Removed custom matchOrigin; using gin-contrib/cors library
func CORSMiddleware() gin.HandlerFunc {
	corsOrigins := os.Getenv("CORS_ORIGINS")
	if corsOrigins == "" {
		corsOrigins = "http://localhost:5173,http://*.localhost:5173"
	}
	origins := strings.Split(corsOrigins, ",")
	for _, o := range origins {
		if strings.TrimSpace(o) == "*" {
			// Wildcard-Origin + AllowCredentials ist eine unsichere Kombination (S9) –
			// lieber beim Start hart abbrechen als sie in Prod schleifen zu lassen.
			logging.Fatal("CORS_ORIGINS darf kein '*' enthalten, solange AllowCredentials aktiv ist")
		}
	}
	cfg := cors.Config{
		AllowOrigins: origins,
		// Nötig, damit "http://*.localhost:5173" tatsächlich als Subdomain-Wildcard
		// gematcht wird – ohne dieses Flag behandelt gin-contrib/cors den Eintrag als
		// literalen String, der nie zu einer echten Mandanten-Subdomain passt.
		AllowWildcard: true,
		AllowMethods:  []string{"POST", "OPTIONS", "GET", "PUT", "DELETE"},
		AllowHeaders: []string{
			"Content-Type", "Content-Length", "Accept-Encoding",
			"X-CSRF-Token", "Authorization", "X-Auth-Token", "accept", "origin",
			"Cache-Control", "X-Requested-With",
		},
		AllowCredentials: true,
	}
	withCORS := cors.New(cfg)
	return func(c *gin.Context) {
		// Den SAML-ACS ruft der Browser per Formular-POST von der
		// Origin des IdP auf; gin-contrib/cors bräche das mit 403 ab. Er läuft
		// ohne CORS-Header, eine fremde Seite kann die Antwort also nicht lesen.
		if c.Request.Method == http.MethodPost && handlers.IsSAMLACSPath(c.Request.URL.Path) {
			return
		}
		withCORS(c)
	}
}

// initLogging lädt optional eine .env-Datei (nur zur lokalen Entwicklung, in
// Prod kommen die Variablen aus der echten Umgebung) und richtet danach den
// globalen Logger ein (JSON in Prod bzw. mit LOG_FORMAT=json,
// sonst Text; Level per LOG_LEVEL) - in dieser Reihenfolge, damit
// LOG_FORMAT/LOG_LEVEL aus der .env greifen.
func initLogging() {
	envErr := godotenv.Load()
	logging.Init(os.Stdout, os.Getenv)
	if envErr != nil {
		slog.Warn("No .env file found, relying on environment variables")
	}
}

// registerHealthcheck registriert den /healthz-Endpunkt, der zusätzlich zur
// reinen Prozess-Liveness auch die DB-Erreichbarkeit prüft.
func registerHealthcheck(r *gin.Engine, h *handlers.Handler) {
	r.GET("/healthz", func(c *gin.Context) {
		sqlDB, err := h.DB.DB()
		if err != nil {
			slog.Error("Healthcheck failed: could not get sqlDB", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "database unavailable"})
			return
		}
		if err := sqlDB.Ping(); err != nil {
			slog.Error("Healthcheck failed: could not ping database", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "database ping failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}

// newRouter baut den kompletten gin.Engine inkl. aller Middlewares und
// Routen. Aufgeteilt aus main(), um dessen kognitive Komplexität gering zu
// halten (SonarCloud go:S3776) - die einzelnen register*-Funktionen
// enthalten fast ausschließlich lineare Routenregistrierungen ohne eigene
// Verzweigungslogik.
// Größengrenzen für Request-Bodys. Der Standard deckt alle JSON-Eingaben (die
// größte legitime ist eine SAML-IdP-Metadaten-Konfiguration mit 256 KiB). Die
// SAML-Antwort des IdP kommt formularkodiert und darf laut
// handlers.samlMaxResponseBytes bis 1 MiB groß sein; der ACS-Pfad bekommt
// deshalb Platz für die Formularkodierung.
const (
	defaultMaxBodyBytes = 1 << 20
	samlACSMaxBodyBytes = 2 << 20
)

// bodyLimitOverrides: Routen-Templates mit eigener Body-Grenze.
var bodyLimitOverrides = map[string]int64{
	"/api/auth/saml/:page_id/acs": samlACSMaxBodyBytes,
}

func newRouter(h *handlers.Handler) *gin.Engine {
	// gin.New() + eigener RequestLogger/Recovery statt
	// gin.Default() - eine strukturierte Access-Log-Zeile mit Request-ID statt
	// des Freitext-Loggers (der u.a. ?token=... im Klartext ausgab).
	r := gin.New()
	r.Use(middleware.RequestLogger(), middleware.Recovery())
	proxies := trustedProxiesFromEnv()
	checkTrustedProxyWidth(proxies)
	if err := r.SetTrustedProxies(proxies); err != nil {
		logging.Fatal("Ungültige TRUSTED_PROXIES-Konfiguration", "error", err)
	}
	r.Use(CORSMiddleware())
	r.Use(middleware.SecurityHeaders())
	// Globale Body-Grenze (413 request_too_large) vor allen Handlern.
	r.Use(middleware.BodyLimit(defaultMaxBodyBytes, bodyLimitOverrides))
	// NUL-Byte und ungültiges UTF-8 in Body, Query und Pfad: 400 statt eines
	// Datenbankfehlers (500) beim Schreiben.
	r.Use(middleware.RejectInvalidCharacters())
	// Golden Signals (Latency, Traffic, Errors) Instrumentierung – Schritt 1/4.
	// Rein In-Memory-Aggregation (backend/metrics), noch ohne
	// externe Exposition (kein /metrics-Endpoint, kein Dashboard); das folgt
	// in separaten Folgeschritten.
	r.Use(middleware.GoldenSignals(metrics.Default))

	// Prometheus-Export der Golden Signals (Schritt 2/4) –
	// Basic-Auth-geschützt, siehe handlers.RegisterMetricsEndpoint.
	// Der Grafana-Agent/Alloy-Scraper-Aufbau selbst (Render-Service-Topologie,
	// Remote-Write) ist eine offene Deployment-Frage und bewusst nicht Teil
	// dieser Änderung.
	if !handlers.RegisterMetricsEndpoint(r, metrics.Default) {
		slog.Warn(handlers.MetricsBasicAuthUserEnv + "/" + handlers.MetricsBasicAuthPasswordEnv + " nicht gesetzt – /metrics-Endpunkt bleibt deaktiviert")
	}

	registerHealthcheck(r, h)
	registerPublicRoutes(r, h)
	registerAuthRoutes(r, h)
	registerStaticFrontend(r)
	registerRoutesInMetrics(r, metrics.Default)

	return r
}

// registerRoutesInMetrics trägt alle registrierten Gin-Routen (Templates, keine
// IDs) mit 0 Requests in die Registry ein, damit die
// Monitoring-Tabelle auch ungenutzte Endpunkte zeigt. /metrics ist intern
// (Scrape-Endpunkt) und fehlt bewusst. Die Obergrenze ist die feste Zahl der
// Routen im Code; Requests auf unbekannte Pfade bündelt die Middleware weiter
// unter NOT_FOUND.
func registerRoutesInMetrics(r *gin.Engine, reg *metrics.Registry) {
	for _, ri := range r.Routes() {
		if ri.Path == "/metrics" {
			continue
		}
		reg.RegisterRoute(ri.Method, ri.Path)
	}
}

func registerPublicRoutes(r *gin.Engine, h *handlers.Handler) {
	// --- SSO (öffentlich) ---
	// OIDC je Statusseite. Statische Pfade gehen in gin vor
	// /api/auth/:provider.
	r.GET("/api/auth/oidc/login", middleware.NewRateLimiter(20, time.Minute), h.OIDCLoginHandler)
	// Einstieg über den Slug für Besucher einer privaten Seite (Seiten-ID und
	// SSO-Art nennt die Auskunft /api/public/pages/:slug/sso ihnen nicht).
	r.GET("/api/auth/sso/login", middleware.NewRateLimiter(20, time.Minute), h.SSOLoginBySlugHandler)
	r.GET("/api/auth/oidc/callback", middleware.NewRateLimiter(20, time.Minute), h.OIDCCallbackHandler)
	// SAML je Statusseite. Metadaten und ACS hängen an der
	// Seite im Pfad, weil der Admin sie beim IdP einträgt.
	r.GET("/api/auth/saml/login", middleware.NewRateLimiter(20, time.Minute), h.SAMLLoginHandler)
	r.GET("/api/auth/saml/:page_id/metadata", middleware.NewRateLimiter(60, time.Minute), h.SAMLMetadataHandler)
	r.POST("/api/auth/saml/:page_id/acs", middleware.NewRateLimiter(20, time.Minute), h.SAMLACSHandler)
	// Google und GitHub: wie die OIDC-Routen 20/min je IP. Der Callback löst
	// einen ausgehenden Aufruf beim Anbieter aus.
	r.GET("/api/auth/:provider", middleware.NewRateLimiter(20, time.Minute), h.OAuthBeginHandler)
	r.GET("/api/auth/:provider/callback", middleware.NewRateLimiter(20, time.Minute), h.OAuthCallbackHandler)
	r.POST("/api/auth/sso-exchange", middleware.NewRateLimiter(10, time.Minute), h.SSOExchangeHandler)

	// --- Öffentliche Routen ---
	// 20/Minute statt 5: schützt weiterhin wirksam vor Brute-Force, kollidiert aber
	// nicht mehr mit der eigenen e2e-Suite (mehrere Spec-Dateien loggen sich pro
	// Lauf über den Dev-Simulator ein, alle von derselben lokalen IP aus).
	r.POST("/api/login", middleware.NewRateLimiter(20, time.Minute), h.LoginHandler)
	r.POST("/api/subscribe", middleware.NewRateLimiter(10, time.Minute), middleware.OptionalAuthMiddleware(), h.SubscribeHandler)
	r.POST("/api/subscribe/test-webhook", middleware.AuthMiddleware(), middleware.NewRateLimiter(10, time.Minute), h.TestWebhookHandler)
	publicGroup := r.Group("/api/public")
	publicGroup.Use(middleware.OptionalAuthMiddleware())
	publicGroup.GET("/pages", middleware.NewRateLimiter(120, time.Minute), h.GetPublicPagesHandler)
	// Lese-Routen für Vorfälle: großzügig (120/min je IP und Route), damit ein
	// einzelner Client die Datenbank nicht mit Dashboard-Abrufen auslastet; ein
	// normaler Besucher kommt auf wenige Aufrufe pro Minute. Hinter einem
	// Reverse-Proxy muss TRUSTED_PROXIES gesetzt sein, sonst teilen sich alle
	// Besucher die Proxy-Adresse.
	publicGroup.GET("/pages/:slug/dashboard", middleware.NewRateLimiter(120, time.Minute), h.GetPublicDashboardHandler)
	// 90-Tage-Verfügbarkeit (Tagesaggregate) für die Statusseite.
	publicGroup.GET("/pages/:slug/availability", middleware.NewRateLimiter(60, time.Minute), h.GetPublicAvailabilityHandler)
	// gebündelte bestätigte Übersetzungen der Statusseite (lang=xx).
	publicGroup.GET("/pages/:slug/translations", middleware.NewRateLimiter(60, time.Minute), h.GetPublicTranslationsHandler)
	// bestätigte Übersetzungen der Updates eines Vorfalls.
	publicGroup.GET("/pages/:slug/incidents/:incident_id/update-translations", middleware.NewRateLimiter(60, time.Minute), h.GetPublicIncidentUpdateTranslationsHandler)
	publicGroup.GET("/ui-overrides", middleware.NewRateLimiter(120, time.Minute), h.GetPublicUiOverridesHandler)
	// OptionalAuthMiddleware: private Seiten benötigen eine Prüfung
	// gegen den eingeloggten User (Global Admin oder UserStatusPageRole),
	// anonyme Requests bleiben für öffentliche Seiten weiterhin erlaubt.
	r.GET("/api/pages/slug/:slug", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetStatusPageBySlugHandler)
	// Public read: components & incidents scoped by page
	// Sicherheitsprüfung: alle Routen hier lesen Daten einer
	// StatusPage über page_id bzw. eine globale Kind-ID und müssen daher
	// wissen, ob ein User eingeloggt ist (pageIsAccessible-Check in den
	// jeweiligen Handlern) - OptionalAuthMiddleware setzt userID bei
	// vorhandenem, validem Token, lässt anonyme Requests aber weiterhin durch.
	r.GET("/api/pages/:page_id/components", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetComponentsHandler)
	r.GET("/api/pages/:page_id/incidents", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetIncidentsHandler)
	r.GET("/api/pages/:page_id/incidents/:id", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetIncidentDetailHandler)
	// Stage 2 der Mehrsprachigkeit: Confirmed translations für public API
	r.GET("/api/incidents/:incident_id/translations", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetIncidentTranslationsHandler)
	// Stage 3 der Mehrsprachigkeit: Confirmed Component translations für public API
	r.GET("/api/components/:component_id/translations", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetComponentTranslationsHandler)
	// Stage 4 der Mehrsprachigkeit: Confirmed Notice translations für public API
	r.GET("/api/notices/:notice_id/translations", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetGeneralNoticeTranslationsHandler)
	// aktive General Notices (öffentlich, IsActive + StartAt/EndAt-Fenster gefiltert)
	r.GET("/api/pages/:page_id/notices", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetActiveNoticesHandler)

	// Eingehender Monitoring-Webhook: läuft bewusst NICHT über
	// AuthMiddleware/JWT, sondern authentifiziert sich selbst per
	// X-Webhook-Token-Header gegen den pro Statusseite hinterlegten Token
	// (siehe ComponentStatusWebhookHandler). Eigenes, eng gefasstes Rate-Limit
	// analog zu /api/login, damit dieser öffentlich erreichbare Endpunkt nicht
	// für Token-Brute-Force oder Statuswert-Spam missbraucht werden kann.
	// Ein gemeinsamer Zähler für beide Wege (ID und Kennung), sonst
	// wären je IP doppelt so viele Token-Versuche möglich.
	webhookLimit := middleware.NewRateLimiter(20, time.Minute)
	r.POST("/api/webhooks/components/:component_id/status", webhookLimit, h.ComponentStatusWebhookHandler)
	r.POST("/api/webhooks/component-slugs/:slug/status", webhookLimit, h.ComponentStatusWebhookBySlugHandler)

	// Custom-Domain-Auflösung: PublicRoot.vue ruft dies für Hosts
	// auf, die weder eine bekannte App-Basisdomain noch eine PaaS-Plattform-
	// domain sind, um den zugehörigen Statusseiten-Slug zu ermitteln. Eigenes
	// Rate-Limit, da bei jedem Seitenaufruf über eine Custom Domain getroffen.
	r.GET("/api/public/resolve-domain", middleware.NewRateLimiter(60, time.Minute), middleware.OptionalAuthMiddleware(), h.ResolvePublicDomainHandler)
	// meldet nur, ob das Seiten-SSO aktiv ist (Knopf „Mit SSO
	// anmelden“), Rate-Limit wie resolve-domain (Abruf bei jedem Seitenaufruf).
	r.GET("/api/public/pages/:slug/sso", middleware.NewRateLimiter(60, time.Minute), middleware.OptionalAuthMiddleware(), h.GetPublicOIDCHandler)

	// Subscriber-Self-Service: Abbestellen ohne Login, per
	// signiertem Token aus der Benachrichtigungsmail. Rate-Limit analog zu
	// anderen unauthentifizierten öffentlichen Endpunkten.
	// Löschen nur per POST (Frontend-Knopf bzw. RFC-8058-One-
	// Click), GET leitet nur noch auf die Frontend-Seite /unsubscribe weiter.
	r.GET("/api/public/subscribers/unsubscribe", middleware.NewRateLimiter(20, time.Minute), h.UnsubscribeRedirectHandler)
	r.POST("/api/public/subscribers/unsubscribe", middleware.NewRateLimiter(20, time.Minute), h.UnsubscribeHandler)
	// Double-Opt-in-Bestätigung per Token, Rate-Limit wie
	// /api/public/orders/confirm.
	r.POST("/api/public/subscribers/confirm", middleware.NewRateLimiter(20, time.Minute), h.ConfirmSubscriptionHandler)

	// Self-Service-Bestellung neuer Statusseiten über die Startseite
	// ("Eingangstor"-Homepage) - siehe handlers/page_order.go für den
	// dreistufigen Ablauf und den Missbrauchsschutz (Honeypot + Email-
	// Bestätigung + Cap auf offene Bestellungen je Email, zusätzlich zum
	// hier gesetzten IP-Rate-Limit). Enger als /api/subscribe, da jeder Hit
	// eine DB-Zeile UND einen Mailversand auslöst.
	// öffentlicher Flag-Endpunkt (ohne Auth), ob die Self-Service-
	// Bestellung aktiv ist - Landing-CTA und /order-Seite richten sich danach.
	r.GET("/api/public/orders/enabled", middleware.NewRateLimiter(60, time.Minute), h.GetPublicOrdersEnabledHandler)
	r.POST("/api/public/orders", middleware.NewRateLimiter(5, time.Minute), h.CreateOrderHandler)
	r.POST("/api/public/orders/confirm", middleware.NewRateLimiter(20, time.Minute), h.ConfirmOrderHandler)
	r.POST("/api/public/orders/set-password", middleware.NewRateLimiter(10, time.Minute), h.SetOrderPasswordHandler)
	// Vom Reverse-Proxy (z. B. Caddy On-Demand-TLS) als "ask"-Endpunkt aufgerufen
	// (siehe README, Abschnitt Betrieb) - läuft im internen Netzwerk. Das Limit
	// je IP (120/min, Burst 120, also 2 Anfragen pro Sekunde im Mittel) fängt
	// ab, wenn der Endpunkt doch erreichbar ist (leeres Token in der
	// Entwicklung, Fehlkonfiguration) und sonst als unbegrenztes Orakel für den
	// Status fremder Domains dienen würde. Caddy fragt nur für Hosts ohne
	// zwischengespeichertes Zertifikat (Erstausstellung, Erneuerung), nicht pro
	// TLS-Handshake; die Obergrenze liegt weit über dem realen Bedarf, auch wenn
	// alle Anfragen über dieselbe Proxy-IP kommen. Bei 429 versucht Caddy es
	// beim nächsten Handshake erneut.
	r.GET("/api/internal/custom-domain-check", middleware.NewRateLimiter(120, time.Minute), h.InternalCustomDomainCheckHandler)
}

// registerAuthRoutes registriert alle Routen, die AuthMiddleware() voraussetzen
// (JWT-geschützt) - ausgelagert aus newRouter, um dessen kognitive Komplexität
// gering zu halten (SonarCloud go:S3776).
func registerAuthRoutes(r *gin.Engine, h *handlers.Handler) {
	// --- Geschützte Routen ---
	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.GET("/me", h.MeHandler)
	authGroup.POST("/auth/oidc/link", h.OIDCLinkHandler)
	authGroup.POST("/auth/saml/link", h.SAMLLinkHandler)
	// eigene SSO-Verknüpfungen anzeigen und lösen (Profil).
	authGroup.GET("/me/sso", h.GetMySSOHandler)
	authGroup.DELETE("/me/sso/:id", h.DeleteMySSOHandler)
	authGroup.PATCH("/me", h.UpdateMeHandler)
	// DSGVO-Datenexport: eigene Daten als JSON-Download,
	// niemals Daten anderer Nutzer - siehe handlers/me_export.go.
	authGroup.GET("/me/export", h.MeExportHandler)
	// Konto löschen - siehe handlers/me_delete.go.
	authGroup.DELETE("/me", h.DeleteMeHandler)
	authGroup.GET("/me/pages", h.GetMyPagesHandler)
	authGroup.GET("/me/subscription", h.GetMySubscriptionHandler)
	authGroup.PUT("/me/subscription", h.UpdateMySubscriptionHandler)
	authGroup.DELETE("/me/subscription", h.DeleteMySubscriptionHandler)
	// eigene Einladung (Token aus der Mail) ansehen, annehmen oder
	// ablehnen. Nur angemeldet mit dem eingeladenen Konto; Token im Body, nie in
	// der URL. Begrenzt wie die übrigen Token-Endpunkte.
	invitationLimit := middleware.NewRateLimiter(20, time.Minute)
	authGroup.POST("/invitations/preview", invitationLimit, h.PreviewPageInvitationHandler)
	authGroup.POST("/invitations/accept", invitationLimit, h.AcceptPageInvitationHandler)
	authGroup.POST("/invitations/decline", invitationLimit, h.DeclinePageInvitationHandler)

	// Liefert ALLE Statusseiten ungefiltert (auch private/unlisted) – nur für
	// Global Admins (Seiten-/Nutzerverwaltungs-Screens). War vorher komplett
	// öffentlich erreichbar und leakte die Namen/Slugs aller Mandanten.
	guarded(authGroup, http.MethodGet, "/pages", platform("pages", permissions.Read), h.GetStatusPagesHandler)

	// StatusPage Management (Ressource pages)
	// Wer Nutzer einer Seite verwaltet, braucht pages:<aktion> auf genau dieser
	// Seite (oder Global Admin) – vorher war das komplett ungeschützt (jeder
	// eingeloggte Nutzer konnte sich beliebige Rollen auf beliebigen Seiten
	// zuweisen). Route-Param heißt bewusst :page_id, weil RequirePermission genau
	// diesen Namen erwartet. jede Route prüft die zur HTTP-Aktion
	// passende Berechtigung, siehe guarded() in route_permissions.go.
	guarded(authGroup, http.MethodGet, "/admin/pages/:page_id/users", need("pages", permissions.Read), h.GetStatusPageUsersHandler)
	guarded(authGroup, http.MethodPost, "/admin/pages/:page_id/users", need("pages", permissions.Create), middleware.NewRateLimiter(20, time.Minute), h.AddStatusPageUserHandler)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id/users/:uid", need("pages", permissions.Delete), h.RemoveStatusPageUserHandler)
	// offene Einladungen der Seite ansehen und zurückziehen.
	guarded(authGroup, http.MethodGet, "/admin/pages/:page_id/invitations", need("pages", permissions.Read), h.GetPageInvitationsHandler)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id/invitations/:id", need("pages", permissions.Delete), h.RevokePageInvitationHandler)

	// Neue Statusseiten anzulegen ist eine Plattform-Operation (es gibt noch keine
	// page_id, auf die sich pages:create scopen könnte) – dafür Global Admin nötig.
	guarded(authGroup, http.MethodPost, "/admin/pages", platform("pages", permissions.Create), h.CreateStatusPageHandler)
	guarded(authGroup, http.MethodPut, "/admin/pages/:page_id", need("pages", permissions.Update), h.UpdateStatusPageHandler)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id", need("pages", permissions.Delete), h.DeleteStatusPageHandler)
	// Monitoring-Webhook-Token pro Statusseite: Generieren liefert
	// den Klartext-Token einmalig zurück, Widerrufen invalidiert ihn wieder.
	// Beides ändert die Seite (pages:update).
	guarded(authGroup, http.MethodPost, "/admin/pages/:page_id/webhook-token", need("pages", permissions.Update), h.GenerateWebhookTokenHandler)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id/webhook-token", need("pages", permissions.Update), h.RevokeWebhookTokenHandler)
	// Custom-Domain-Verwaltung: eigene Endpunkte statt Teil des
	// generischen statusPageWritableFields-Binds, siehe Kommentar dort. Setzen,
	// Entfernen und Prüfen ändern die Seite (pages:update).
	guarded(authGroup, http.MethodPut, "/admin/pages/:page_id/custom-domain", need("pages", permissions.Update), h.SetCustomDomainHandler)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id/custom-domain", need("pages", permissions.Update), h.DeleteCustomDomainHandler)
	guarded(authGroup, http.MethodPost, "/admin/pages/:page_id/custom-domain/verify", need("pages", permissions.Update), handlers.CustomDomainVerifyLimit(), h.VerifyCustomDomainHandler)
	// SSO-Konfiguration pro Statusseite: OIDC + SAML (Ressource sso)
	guarded(authGroup, http.MethodGet, "/admin/pages/:page_id/sso", need("sso", permissions.Read), h.GetSSO)
	guarded(authGroup, http.MethodPut, "/admin/pages/:page_id/sso", need("sso", permissions.Update), h.UpdateSSO)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id/sso", need("sso", permissions.Delete), h.DeleteSSO)
	// Testanmeldung (test_login_enabled), Rate-Limit wie der
	// OIDC-Login, weil jeder Aufruf Discovery und einen Login-State auslöst.
	// Sonderaktion: schaltet die Testanmeldung der Konfiguration (sso:update).
	guarded(authGroup, http.MethodPost, "/admin/pages/:page_id/sso/test", need("sso", permissions.Update), middleware.NewRateLimiter(20, time.Minute), h.StartSSOTestHandler)
	guarded(authGroup, http.MethodPost, "/admin/pages/:page_id/sso/saml-test", need("sso", permissions.Update), middleware.NewRateLimiter(20, time.Minute), h.StartSAMLTestHandler)

	// Components – scoped under /pages/:page_id
	guarded(authGroup, http.MethodPost, "/pages/:page_id/components", need("components", permissions.Create), h.CreateComponentHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/components/:id", need("components", permissions.Update), h.UpdateComponentHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/components/:id", need("components", permissions.Delete), h.DeleteComponentHandler)
	// Archivieren/Wiederherstellen ist eine Änderung (components:update), kein Löschen.
	guarded(authGroup, http.MethodPost, "/pages/:page_id/components/:id/archive", need("components", permissions.Update), h.ArchiveComponentHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/components/:id/restore", need("components", permissions.Update), h.RestoreComponentHandler)

	// externe Statusseiten (Quellen, externe Komponenten, Kopplung) – scoped under /pages/:page_id
	registerExternalStatusRoutes(authGroup, h)

	// Incidents und Wartungen – scoped under /pages/:page_id. Wartungen teilen
	// sich die Routen: byKind prüft incidents oder maintenance je
	// nach Art des Datensatzes (Anlegen: Feld IsMaintenance im Body).
	// Alles, was Abonnenten benachrichtigt, hat ein Limit je Seite und Nutzer
	// (handlers/notify_rate_limit.go); main_notification_limit_test.go prüft,
	// dass keine Route mit Mailversand ohne Limit bleibt. Anlegen: 30 je Seite
	// und Stunde, Änderungen und Updates: 120.
	notifyCreate, notifyChange := handlers.NotificationCreateLimit(), handlers.NotificationChangeLimit()
	guarded(authGroup, http.MethodPost, "/pages/:page_id/incidents", byKind(permissions.Create, h.IncidentResourceFromBody()), notifyCreate, h.CreateIncidentHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/incidents/:id", byKind(permissions.Update, h.IncidentResourceByParam("id")), notifyChange, h.UpdateIncidentHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/incidents/:id", byKind(permissions.Delete, h.IncidentResourceByParam("id")), h.DeleteIncidentHandler)
	// Sonderaktion: ein Timeline-Eintrag legt einen Datensatz an (create).
	guarded(authGroup, http.MethodPost, "/pages/:page_id/incidents/:id/updates", byKind(permissions.Create, h.IncidentResourceByParam("id")), notifyChange, h.AddIncidentUpdateHandler)
	// Übersetzungen eines Updates (Entwürfe inklusive) lesen und je Sprache speichern.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/incidents/:id/updates/:update_id/translations", byKind(permissions.Read, h.IncidentResourceByParam("id")), h.GetIncidentUpdateTranslationsHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/incidents/:id/updates/:update_id/translations/:language_code", byKind(permissions.Update, h.IncidentResourceByParam("id")), h.SaveIncidentUpdateTranslationHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/incidents/:id/postmortem", byKind(permissions.Update, h.IncidentResourceByParam("id")), h.UpsertPostMortemHandler)

	// Stage 2 der Mehrsprachigkeit: Incident translations (draft/confirmed)
	// die Pfade tragen keine :page_id - die
	// *PageScope-Middlewares leiten sie aus der Ressource ab, damit
	// RequirePermission auch für Seiten-Admins (nicht nur Global Admins)
	// greift und fremde Seiten mit 403 abweist (handlers/translation_page_scope.go).
	// Übersetzungen folgen den Rechten der Ressource ihres Inhalts.
	guarded(authGroup, http.MethodPost, "/incidents/:incident_id/translations", byKind(permissions.Create, h.IncidentResourceByParam("incident_id")).after(h.IncidentPageScope()), h.IncidentTagGuard(), h.CreateIncidentTranslationHandler)
	// Admin-Liste inkl. Entwürfe (die öffentliche Route oben liefert nur confirmed).
	guarded(authGroup, http.MethodGet, "/incidents/:incident_id/translations/all", byKind(permissions.Read, h.IncidentResourceByParam("incident_id")).after(h.IncidentPageScope()), h.IncidentTagGuard(), h.GetAllIncidentTranslationsHandler)
	// Übersetzung einer Sprache aus dem Bearbeiten-Formular speichern (anlegen oder ändern, Entwurf oder fertig).
	// Übersetzungsvorschlag: liest/schreibt keinen Datensatz, braucht
	// daher nur update an einem der drei Texttypen; Aufrufe sind begrenzt.
	guarded(authGroup, http.MethodPost, "/pages/:page_id/translate", anyOf(permissions.Update, "incidents", "maintenance", "notices"), middleware.NewRateLimiter(30, time.Minute), h.TranslateTextHandler)
	guarded(authGroup, http.MethodPut, "/incidents/:incident_id/translations/:language_code", byKind(permissions.Update, h.IncidentResourceByParam("incident_id")).after(h.IncidentPageScope()), h.IncidentTagGuard(), h.SaveIncidentTranslationHandler)
	// Sonderaktion: Entwurf bestätigen ändert die Übersetzung (update).
	guarded(authGroup, http.MethodPatch, "/incident-translations/:translation_id/confirm", byKind(permissions.Update, h.IncidentResourceByTranslation()).after(h.IncidentTranslationPageScope()), h.IncidentTranslationTagGuard(), h.ConfirmIncidentTranslationHandler)
	guarded(authGroup, http.MethodDelete, "/incident-translations/:translation_id", byKind(permissions.Delete, h.IncidentResourceByTranslation()).after(h.IncidentTranslationPageScope()), h.IncidentTranslationTagGuard(), h.DeleteIncidentTranslationHandler)

	// Stage 3 der Mehrsprachigkeit: Component translations (draft/confirmed)
	guarded(authGroup, http.MethodPost, "/components/:component_id/translations", need("components", permissions.Create).after(h.ComponentPageScope()), h.ComponentTagGuard(), h.CreateComponentTranslationHandler)
	guarded(authGroup, http.MethodGet, "/components/:component_id/translations/all", need("components", permissions.Read).after(h.ComponentPageScope()), h.ComponentTagGuard(), h.GetAllComponentTranslationsHandler)
	guarded(authGroup, http.MethodPut, "/components/:component_id/translations/:language_code", need("components", permissions.Update).after(h.ComponentPageScope()), h.ComponentTagGuard(), h.SaveComponentTranslationHandler)
	guarded(authGroup, http.MethodPatch, "/component-translations/:translation_id/confirm", need("components", permissions.Update).after(h.ComponentTranslationPageScope()), h.ComponentTranslationTagGuard(), h.ConfirmComponentTranslationHandler)
	guarded(authGroup, http.MethodDelete, "/component-translations/:translation_id", need("components", permissions.Delete).after(h.ComponentTranslationPageScope()), h.ComponentTranslationTagGuard(), h.DeleteComponentTranslationHandler)

	// Stage 4 der Mehrsprachigkeit: Notice translations (draft/confirmed)
	guarded(authGroup, http.MethodPost, "/notices/:notice_id/translations", need("notices", permissions.Create).after(h.NoticePageScope()), h.CreateGeneralNoticeTranslationHandler)
	guarded(authGroup, http.MethodGet, "/notices/:notice_id/translations/all", need("notices", permissions.Read).after(h.NoticePageScope()), h.GetAllGeneralNoticeTranslationsHandler)
	// Übersetzung einer Sprache aus dem Bearbeiten-Formular speichern.
	guarded(authGroup, http.MethodPut, "/notices/:notice_id/translations/:language_code", need("notices", permissions.Update).after(h.NoticePageScope()), h.SaveGeneralNoticeTranslationHandler)
	guarded(authGroup, http.MethodPatch, "/notice-translations/:translation_id/confirm", need("notices", permissions.Update).after(h.NoticeTranslationPageScope()), h.ConfirmGeneralNoticeTranslationHandler)
	guarded(authGroup, http.MethodDelete, "/notice-translations/:translation_id", need("notices", permissions.Delete).after(h.NoticeTranslationPageScope()), h.DeleteGeneralNoticeTranslationHandler)

	// General Notices – scoped under /pages/:page_id
	guarded(authGroup, http.MethodGet, "/pages/:page_id/notices/all", need("notices", permissions.Read), h.GetNoticesHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/notices", need("notices", permissions.Create), notifyCreate, h.CreateNoticeHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/notices/:id", need("notices", permissions.Update), notifyChange, h.UpdateNoticeHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/notices/:id", need("notices", permissions.Delete), h.DeleteNoticeHandler)

	// Incident-Vorlagen – scoped under /pages/:page_id, eigene
	// Ressource incident_templates (vorher incidents:write auch fürs Lesen).
	guarded(authGroup, http.MethodGet, "/pages/:page_id/incident-templates", need("incident_templates", permissions.Read), h.GetIncidentTemplatesHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/incident-templates", need("incident_templates", permissions.Create), h.CreateIncidentTemplateHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/incident-templates/:id", need("incident_templates", permissions.Update), h.UpdateIncidentTemplateHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/incident-templates/:id", need("incident_templates", permissions.Delete), h.DeleteIncidentTemplateHandler)
	// Übersetzungen einer Vorlage (Sprachumschalter im Formular).
	guarded(authGroup, http.MethodGet, "/pages/:page_id/incident-templates/:id/translations", need("incident_templates", permissions.Read), h.GetIncidentTemplateTranslationsHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/incident-templates/:id/translations/:language_code", need("incident_templates", permissions.Update), h.SaveIncidentTemplateTranslationHandler)

	// Notification-Mail-Templates - seiten-scoped unter
	// /pages/:page_id/notification-templates, Ressource notification_templates
	// (vorher pages:write): Templates konfigurieren wie die Seite
	// Nachrichten versendet. Kind + Audience + Language stehen im Pfad, ID nicht -
	// das UI upsertet direkt auf diesem Quartett und muss keinen ID-Zustand
	// tracken (siehe Handler-Kommentar), das Speichern ist deshalb ein PUT
	// (update). Audience kam mit Stage 5 der Mehrsprachigkeit dazu.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/notification-templates", need("notification_templates", permissions.Read), h.GetNotificationTemplatesHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/notification-templates/:kind/:audience/:language", need("notification_templates", permissions.Update), h.PutNotificationTemplateHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/notification-templates/:kind/:audience/:language", need("notification_templates", permissions.Delete), h.DeleteNotificationTemplateHandler)
	// Sonderaktion: Vorschau rendert nur, verändert nichts (read).
	guarded(authGroup, http.MethodPost, "/pages/:page_id/notification-templates/:kind/:audience/:language/preview", need("notification_templates", permissions.Read), h.PreviewNotificationTemplateHandler)

	// Sprachkatalog (Stage 1 der Mehrsprachigkeit): Anlegen/Ändern
	// von Katalog-Einträgen ist global-admin-only (analog zu Tags/Roles).
	// Lesen ist dagegen bewusst für jeden eingeloggten User offen (anders als
	// beim Tag-Katalog) - ein Seiten-Admin ohne Global-Admin-Rechte muss den
	// Katalog trotzdem sehen können, um für seine Statusseite Sprachen über
	// PutPageLanguagesHandler zu aktivieren.
	authGroup.GET("/languages", h.GetLanguagesHandler)
	guarded(authGroup, http.MethodPost, "/languages", platform("languages", permissions.Create), h.CreateLanguageHandler)
	guarded(authGroup, http.MethodPut, "/languages/:id", platform("languages", permissions.Update), h.UpdateLanguageHandler)
	guarded(authGroup, http.MethodDelete, "/languages/:id", platform("languages", permissions.Delete), h.DeleteLanguageHandler)

	// Lesen nur mit Rolle auf der Seite - das
	// öffentliche Frontend nutzt den Endpunkt nicht (nur Admin-Ansichten
	// über useContentLanguages.js), vorher konnte jeder eingeloggte Nutzer
	// die Sprachkonfiguration fremder Seiten lesen. Jede Seitenrolle darf lesen,
	// weil die Übersetzungs-Panels der anderen Tabs die Sprachen brauchen.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/languages", pageRole("languages"), h.GetPageLanguagesHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/languages", need("languages", permissions.Update), h.PutPageLanguagesHandler)

	// UI-Übersetzungs-Overrides: globale Overrides
	// (Global Admin, wirken app-weit als neuer Standard) + pro-Seite
	// Overrides (Ressource ui_translations, nur eigene Seite, gelten nur im
	// Admin-Bereich dieser Seite - siehe frontend/src/layouts/AdminLayout.vue).
	// Bewusst unter /pages/:page_id/... genestet (nicht flach wie die Content-
	// Translation-Routen), damit RequirePermission tatsächlich pro Seite
	// scopen kann.
	guarded(authGroup, http.MethodGet, "/admin/ui-overrides", platform("ui_translations", permissions.Read), h.GetGlobalUiOverridesHandler)
	guarded(authGroup, http.MethodPut, "/admin/ui-overrides", platform("ui_translations", permissions.Update), h.PutGlobalUiOverrideHandler)
	guarded(authGroup, http.MethodDelete, "/admin/ui-overrides", platform("ui_translations", permissions.Delete), h.DeleteGlobalUiOverrideHandler)

	// Lesen nur mit Rolle auf der Seite (geladen
	// von AdminLayout.vue im Admin-Bereich der Seite); anonyme Besucher
	// bekommen die globalen Overrides über /api/public/ui-overrides.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/ui-overrides", pageRole("ui_translations"), h.GetPageUiOverridesHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/ui-overrides", need("ui_translations", permissions.Update), h.PutPageUiOverrideHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/ui-overrides", need("ui_translations", permissions.Delete), h.DeletePageUiOverrideHandler)

	// Subscribers – scoped under /pages/:page_id
	guarded(authGroup, http.MethodGet, "/pages/:page_id/subscribers", need("subscribers", permissions.Read), h.GetSubscribersHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/subscribers", need("subscribers", permissions.Create), middleware.NewRateLimiter(20, time.Minute), h.CreateSubscriberAdminHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/subscribers/:id", need("subscribers", permissions.Update), notifyChange, h.UpdateSubscriberHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/subscribers/:id", need("subscribers", permissions.Delete), h.DeleteSubscriberHandler)

	// Users & Roles verwalten Nutzer/Rechte global (kein page_id-Scope möglich) –
	// dafür ist RequirePermission strukturell ungeeignet (erwartet zwingend
	// c.Param("page_id")) und hätte bislang jeden Nicht-Global-Admin mit 400
	// abgewiesen, egal welche Permissions die Rolle trägt. need() wählt für
	// globale Ressourcen (permissions.ScopeGlobal) RequireGlobalAdmin.
	guarded(authGroup, http.MethodGet, "/users", need("users", permissions.Read), h.GetUsersHandler)
	guarded(authGroup, http.MethodPost, "/users", need("users", permissions.Create), h.CreateUserHandler)
	guarded(authGroup, http.MethodPut, "/users/:id", need("users", permissions.Update), h.UpdateUserHandler)
	guarded(authGroup, http.MethodDelete, "/users/:id", need("users", permissions.Delete), h.DeleteUserHandler)

	guarded(authGroup, http.MethodGet, "/roles", need("roles", permissions.Read), h.GetRolesHandler)
	guarded(authGroup, http.MethodPost, "/roles", need("roles", permissions.Create), h.CreateRoleHandler)
	guarded(authGroup, http.MethodPut, "/roles/:id", need("roles", permissions.Update), h.UpdateRoleHandler)
	guarded(authGroup, http.MethodDelete, "/roles/:id", need("roles", permissions.Delete), h.DeleteRoleHandler)

	// Tag-Katalog (Schritt 3/4): global admin-only, analog zu
	// Users & Roles - die Verwaltungsseite zeigt globale UND
	// statusseiten-gebundene Tags in einer Übersicht, ein page_id-scoped
	// RequirePermission wäre dafür strukturell ungeeignet. Die Zuweisung von
	// Tags zu Components/Incidents/Usern folgt erst.
	guarded(authGroup, http.MethodGet, "/tags", need("tags", permissions.Read), h.GetTagsHandler)
	// wählbare Tags (global + seiteneigen) mit Seitenrecht, damit
	// Seiten-Rollen das Tag-Feld in Komponenten-/Vorfallformularen nutzen können.
	// jede Ressource, deren Formulare ein Tag-Feld haben.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/tags", anyOf(permissions.Read, "components", "incidents", "maintenance", "external_sources"), h.GetPageTagsHandler)
	guarded(authGroup, http.MethodPost, "/tags", need("tags", permissions.Create), h.CreateTagHandler)
	guarded(authGroup, http.MethodPut, "/tags/:id", need("tags", permissions.Update), h.UpdateTagHandler)
	guarded(authGroup, http.MethodDelete, "/tags/:id", need("tags", permissions.Delete), h.DeleteTagHandler)

	// Globale Systemeinstellungen: nur Global Admins, analog zu
	// Users/Roles - es gibt keinen page_id-Scope, auf den RequirePermission
	// greifen könnte. Derzeit nur die Self-Service-Bestellung an/aus.
	guarded(authGroup, http.MethodGet, "/settings", need("system_settings", permissions.Read), h.GetSettingsHandler)
	guarded(authGroup, http.MethodPut, "/settings", need("system_settings", permissions.Update), h.UpdateSettingsHandler)
	// Automatische Übersetzung: Dienst und Zugang (Schlüssel nie in
	// der Antwort); der Verbindungstest ändert nichts, ruft aber den Dienst auf.
	guarded(authGroup, http.MethodGet, "/settings/translation", need("system_settings", permissions.Read), h.GetTranslationSettingsHandler)
	guarded(authGroup, http.MethodPut, "/settings/translation", need("system_settings", permissions.Update), h.UpdateTranslationSettingsHandler)
	guarded(authGroup, http.MethodPost, "/settings/translation/test", need("system_settings", permissions.Update), middleware.NewRateLimiter(10, time.Minute), h.TestTranslationSettingsHandler)

	// Mandanten-Übersicht + Audit-Log (nur Global-Admins)
	guarded(authGroup, http.MethodGet, "/admin/overview", need("overview", permissions.Read), h.GetOverviewHandler)
	// In-App Monitoring-Dashboard (Golden Signals Schritt 3/4) –
	// liest aus der in Schritt 1/4 instrumentierten metrics.Default-Registry.
	guarded(authGroup, http.MethodGet, "/admin/monitoring", need("monitoring", permissions.Read), h.GetMonitoringHandler)
	guarded(authGroup, http.MethodGet, "/admin/audit", need("audit", permissions.Read), h.GetAuditLogsHandler)
}

// registerExternalStatusRoutes: externe Statusseiten, Ressource
// external_sources (vorher components:read/write, siehe
// handlers/external_source.go).
func registerExternalStatusRoutes(authGroup *gin.RouterGroup, h *handlers.Handler) {
	const res = "external_sources"
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-sources", need(res, permissions.Read), h.GetExternalSourcesHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/external-sources", need(res, permissions.Create), h.CreateExternalSourceHandler)
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-sources/:id", need(res, permissions.Read), h.GetExternalSourceHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/external-sources/:id", need(res, permissions.Update), h.UpdateExternalSourceHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/external-sources/:id", need(res, permissions.Delete), h.DeleteExternalSourceHandler)
	// Sonderaktion „Jetzt abrufen": ändert den Zustand der Quelle (update).
	guarded(authGroup, http.MethodPost, "/pages/:page_id/external-sources/:id/fetch", need(res, permissions.Update), middleware.NewRateLimiter(20, time.Minute), h.FetchExternalSourceHandler)
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-sources/:id/components", need(res, permissions.Read), h.GetExternalComponentsHandler)
	// externe Bausteine samt eigener Komponente übernehmen und koppeln.
	// Sonderaktion „Übernehmen": legt eigene Komponenten an, braucht deshalb
	// external_sources:create UND components:create (sonst ließe sich die
	// Komponentenverwaltung über den Import umgehen).
	guarded(authGroup, http.MethodPost, "/pages/:page_id/external-sources/:id/import", allOf(permissions.Create, res, "components"), h.ImportExternalComponentsHandler)

	// aktive Quellen-Ausfälle für das Admin-Banner.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-outages", need(res, permissions.Read), h.GetExternalOutagesHandler)

	// Admin-Nachrichtencenter (Glocke). Seitennachrichten sind
	// Betriebsmeldungen für Bearbeiter: sie verlangen external_sources:update
	// (Quellen-Ausfälle) ODER subscribers:update (Webhook-Zustellung) - dasselbe
	// Wirkungsniveau wie vorher components:write/subscribers:write.
	// Auch Lesen und „als gelesen markieren“ brauchen es (Sonderfall: keine
	// read-Aktion), weil die Meldungen Bearbeitern gelten. Systemweite
	// Nachrichten sind Global Admins vorbehalten (Ressource monitoring); die
	// Aktion folgt dort der HTTP-Methode, hat aber keine Wirkung, da nur Global
	// Admins durchkommen. Die Handler prüfen die Rolle zusätzlich selbst.
	pageNotif := func() routeGuard { return anyOf(permissions.Update, "external_sources", "subscribers") }
	guarded(authGroup, http.MethodGet, "/pages/:page_id/admin-notifications", pageNotif(), h.ListAdminNotificationsHandler)
	guarded(authGroup, http.MethodGet, "/pages/:page_id/admin-notifications/unread-count", pageNotif(), h.AdminNotificationUnreadCountHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/admin-notifications/read-all", pageNotif(), h.MarkAllAdminNotificationsReadHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/admin-notifications/:id/read", pageNotif(), h.MarkAdminNotificationReadHandler)
	guarded(authGroup, http.MethodGet, "/admin-notifications", platform("monitoring", permissions.Read), h.ListAdminNotificationsHandler)
	guarded(authGroup, http.MethodGet, "/admin-notifications/unread-count", platform("monitoring", permissions.Read), h.AdminNotificationUnreadCountHandler)
	guarded(authGroup, http.MethodPost, "/admin-notifications/read-all", platform("monitoring", permissions.Create), h.MarkAllAdminNotificationsReadHandler)
	guarded(authGroup, http.MethodPost, "/admin-notifications/:id/read", platform("monitoring", permissions.Create), h.MarkAdminNotificationReadHandler)

	// B2: gespiegelte Vorfälle und Wartungen, die gekoppelte Komponenten betreffen.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-incidents", need(res, permissions.Read), h.GetExternalIncidentsHandler)
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-incidents/:id", need(res, permissions.Read), h.GetExternalIncidentHandler)

	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-links", need(res, permissions.Read), h.GetExternalLinksHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/external-links", need(res, permissions.Create), h.CreateExternalLinkHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/external-links/:id", need(res, permissions.Delete), h.DeleteExternalLinkHandler)
}

// staticRoot ist das Verzeichnis mit dem gebauten Vue-Frontend im Laufzeit-Image.
const staticRoot = "./static"

// registerStaticFrontend hängt das gebaute Vue-Frontend ein, falls vorhanden
// (lokale Backend-only-Entwicklung läuft weiterhin gegen den Vite-Devserver).
func registerStaticFrontend(r *gin.Engine) {
	registerStaticFrontendFrom(r, staticRoot)
}

// registerStaticFrontendFrom liefert das Frontend aus root aus. Reihenfolge:
// /assets, dann jede existierende Datei im Build, sonst index.html (SPA).
//
// Früher stand nur eine feste Liste (/favicon.svg, /icons.svg) im Router. Alles
// andere aus frontend/public/ fiel auf index.html zurück: Das Logo (/logo.png)
// und die selbst gehosteten Schriften (/fonts/*.woff2) kamen auf Prod als
// HTML an (2026-10-29), im Vite-Devserver fiel es nicht auf, weil er public/
// selbst ausliefert. Jetzt reicht es, eine Datei in public/ abzulegen.
func registerStaticFrontendFrom(r *gin.Engine, root string) {
	if _, err := os.Stat(filepath.Join(root, "index.html")); err != nil {
		return
	}
	r.Static("/assets", filepath.Join(root, "assets"))
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found", "code": "not_found"})
			return
		}
		if file, ok := staticFileFor(root, c.Request.URL.Path); ok {
			c.Header("Cache-Control", "public, max-age=86400")
			c.File(file)
			return
		}
		c.File(filepath.Join(root, "index.html"))
	})
}

// staticFileFor liefert den Pfad einer Datei unter root, wenn urlPath auf eine
// existierende reguläre Datei zeigt, sonst false. Abgelehnt werden die
// Startseite selbst, Verzeichnisse und Versteckte (.xyz an irgendeiner Stelle).
// ".." wird vorher durch path.Clean aufgelöst und kann nicht aus root führen.
func staticFileFor(root, urlPath string) (string, bool) {
	clean := path.Clean("/" + urlPath)
	if clean == "/" || clean == "/index.html" {
		return "", false
	}
	for _, seg := range strings.Split(clean, "/") {
		if strings.HasPrefix(seg, ".") {
			return "", false
		}
	}
	// Kein Ausbruch aus root möglich: path.Clean löst bei einem Pfad mit
	// führendem "/" jedes ".." auf (TestStaticFileFor, ../-Varianten in
	// TestStatic_GibtKeineGeheimnisseOderVerzeichnisseHeraus).
	full := filepath.Join(root, filepath.FromSlash(clean))
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	// os.Stat folgt Links: Ein Link im Build-Ordner darf nicht auf Dateien
	// außerhalb zeigen. Links innerhalb des Ordners bleiben erlaubt.
	if !withinRoot(root, full) {
		return "", false
	}
	return full, true
}

// withinRoot sagt, ob path nach Auflösung aller Links unter root liegt.
func withinRoot(root, path string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realRoot, realPath)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// serverTimeouts bündelt die Zeitgrenzen des HTTP-Servers.
type serverTimeouts struct {
	ReadHeader time.Duration // bis die Request-Header komplett sind
	Read       time.Duration // bis der gesamte Request inkl. Body gelesen ist
	Write      time.Duration // bis die Antwort geschrieben ist
	Idle       time.Duration // Keep-Alive-Verbindung ohne Anfrage
}

// defaultServerTimeouts: Die API hat keine Streaming-Antworten. ReadHeader
// bremst Slowloris; Read deckt Bodys bis wenige MiB (BodyLimit); Write liegt
// über den längsten Aufrufen externer Dienste in einem Handler (Übersetzung,
// Webhook-Test, je höchstens rund 15 s).
var defaultServerTimeouts = serverTimeouts{
	ReadHeader: 10 * time.Second,
	Read:       30 * time.Second,
	Write:      60 * time.Second,
	Idle:       120 * time.Second,
}

func newHTTPServer(addr string, h http.Handler, t serverTimeouts) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: t.ReadHeader,
		ReadTimeout:       t.Read,
		WriteTimeout:      t.Write,
		IdleTimeout:       t.Idle,
	}
}

// runServer startet den HTTP-Server im Hintergrund und blockiert, bis
// SIGINT/SIGTERM eintrifft, um dann einen Graceful-Shutdown durchzuführen.
func runServer(r *gin.Engine) {
	handlers.WarnIfCustomDomainCheckUnprotected()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := newHTTPServer(":"+port, r, defaultServerTimeouts)

	go func() {
		slog.Info("Starting backend server", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Failed to run server", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server exiting")
}

func main() {
	if cliRequested(os.Args[1:]) {
		exitProcess(runCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
		return
	}
	initLogging()
	// Vor allem anderen: ein schwaches Signatur-Geheimnis erlaubt gefälschte Tokens.
	middleware.RequireStrongJWTSecret()

	database.InitDB()
	h := handlers.NewHandler(database.DB)
	h.StartMaintenanceScheduler()
	defer handlers.StartSSOCleanupWorker(context.Background())() // s. Doku an StartSSOCleanupWorker
	// Abruf externer Statusseiten; Stopp beim Shutdown nach runServer.
	defer externalstatus.StartFromEnv(context.Background(), h.ExternalStatus, os.Getenv)()
	// Selbstüberwachung setzt den Komponentenstatus der Systemseite.
	defer h.StartSelfMonitorFromEnv(context.Background(), os.Getenv)()

	// Saturation-Signal (Golden Signals Schritt 4/4): DB-
	// Connection-Pool-Auslastung. Muss erst nach InitDB() passieren, da die
	// zugrunde liegende *sql.DB vorher nicht existiert; siehe
	// metrics.Registry.SaturationSnapshot / SetDBStatsProvider.
	if sqlDB, err := database.DB.DB(); err == nil {
		metrics.Default.SetDBStatsProvider(sqlDB.Stats)
	} else {
		slog.Warn("Konnte DBStatsProvider für Saturation-Metriken nicht registrieren", "error", err)
	}

	r := newRouter(h)
	runServer(r)
}
