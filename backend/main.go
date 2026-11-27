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

// trustedProxiesFromEnv reads TRUSTED_PROXIES (comma-separated IPs/CIDRs) from
// the environment (review finding B3). Without an explicit
// SetTrustedProxies(), gin.Default() trusts 0.0.0.0/0 by default, i.e. EVERY sender - an
// attacker could thereby force a new rate-limiter bucket per request with a freely chosen X-Forwarded-For
// header (bypassing the login/subscribe
// brute-force protection) and fake arbitrary audit-log IPs.
// Without a value set, NO proxies are trusted at all - ClientIP() then falls
// back to the actual TCP peer address instead of taking X-Forwarded-For
// unchecked. If the app runs behind a known reverse proxy
// (e.g. Render edge) with a fixed sender address, set TRUSTED_PROXIES to its
// IP(s)/CIDR(s) so that ClientIP() returns the real visitor IP again.
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

// Smallest allowed prefix lengths of trusted proxy networks. A network wider than this (up to
// 0.0.0.0/0 and ::/0) lets practically any sender set forwarding headers
// - reverse proxies sit in small, known networks.
const (
	minTrustedProxyPrefixV4 = 8
	minTrustedProxyPrefixV6 = 16
)

// overlyWideTrustedProxies returns the TRUSTED_PROXIES entries whose network
// is wider than allowed. Invalid entries are reported by SetTrustedProxies itself.
func overlyWideTrustedProxies(entries []string) []string {
	var wide []string
	for _, e := range entries {
		e = strings.TrimSpace(e)
		prefix, err := netip.ParsePrefix(e)
		if err != nil {
			continue // a single address or invalid
		}
		bits, addr := prefix.Bits(), prefix.Addr()
		// ::ffff:a.b.c.d/n is an IPv4 network with n-96 bits.
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

// checkTrustedProxyWidth aborts startup if TRUSTED_PROXIES contains networks
// that are too wide. In development (APP_ENV allowlist) it stays at a
// clear warning so that local Docker networks do not interfere.
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
			// A wildcard origin + AllowCredentials is an insecure combination (S9) -
			// better to abort hard at startup than let it slip through in prod.
			logging.Fatal("CORS_ORIGINS darf kein '*' enthalten, solange AllowCredentials aktiv ist")
		}
	}
	cfg := cors.Config{
		AllowOrigins: origins,
		// Needed so that "http://*.localhost:5173" is actually matched as a subdomain wildcard
		// - without this flag gin-contrib/cors treats the entry as a
		// literal string that never matches a real tenant subdomain.
		AllowWildcard: true,
		AllowMethods:  []string{"POST", "OPTIONS", "GET", "PUT", "DELETE"},
		AllowHeaders: []string{
			"Content-Type", "Content-Length", "Accept-Encoding",
			"X-CSRF-Token", "Authorization", "X-Auth-Token", "accept", "origin",
			"Cache-Control", "X-Requested-With",
		},
		AllowCredentials: true,
	}
	return cors.New(cfg)
}

// initLogging optionally loads a .env file (only for local development, in
// prod the variables come from the real environment) and then sets up the
// global logger (JSON in prod or with LOG_FORMAT=json,
// otherwise text; level via LOG_LEVEL) - in this order, so that
// LOG_FORMAT/LOG_LEVEL from the .env take effect.
func initLogging() {
	envErr := godotenv.Load()
	logging.Init(os.Stdout, os.Getenv)
	if envErr != nil {
		slog.Warn("No .env file found, relying on environment variables")
	}
}

// registerHealthcheck registers the /healthz endpoint, which in addition to
// pure process liveness also checks DB reachability.
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

// newRouter builds the complete gin.Engine including all middlewares and
// routes. Split out of main() to keep its cognitive complexity low
// (SonarCloud go:S3776) - the individual register* functions
// contain almost exclusively linear route registrations without their own
// Verzweigungslogik.
// defaultMaxBodyBytes limits request bodies; it covers every JSON input of
// the API.
const defaultMaxBodyBytes = 1 << 20

// bodyLimitOverrides: route templates with their own body limit (none at the
// moment).
var bodyLimitOverrides = map[string]int64{}

func newRouter(h *handlers.Handler) *gin.Engine {
	// gin.New() + own RequestLogger/Recovery instead of
	// gin.Default() - a structured access-log line with request ID instead of
	// the free-text logger (which, among other things, printed ?token=... in plain text).
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
	// NUL byte and invalid UTF-8 in body, query and path: 400 instead of a
	// database error (500) on write.
	r.Use(middleware.RejectInvalidCharacters())
	// Golden Signals (latency, traffic, errors) instrumentation - step 1/4.
	// Purely in-memory aggregation (backend/metrics), still without
	// external exposure (no /metrics endpoint, no dashboard); that follows
	// in separate follow-up steps.
	r.Use(middleware.GoldenSignals(metrics.Default))

	// Prometheus export of the golden signals (step 2/4) -
	// protected by basic auth, see handlers.RegisterMetricsEndpoint.
	// The Grafana Agent/Alloy scraper setup itself (Render service topology,
	// remote write) is an open deployment question and deliberately not part
	// of this change.
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

// registerRoutesInMetrics enters all registered Gin routes (templates, no
// IDs) with 0 requests into the registry so that the
// monitoring table also shows unused endpoints. /metrics is internal
// (scrape endpoint) and deliberately absent. The upper limit is the fixed number of
// routes in the code; requests to unknown paths are still bundled by the middleware
// under NOT_FOUND.
func registerRoutesInMetrics(r *gin.Engine, reg *metrics.Registry) {
	for _, ri := range r.Routes() {
		if ri.Path == "/metrics" {
			continue
		}
		reg.RegisterRoute(ri.Method, ri.Path)
	}
}

func registerPublicRoutes(r *gin.Engine, h *handlers.Handler) {
	// --- SSO (public) ---
	// OIDC per status page.
	r.GET("/api/auth/oidc/login", middleware.NewRateLimiter(20, time.Minute), h.OIDCLoginHandler)
	// Entry via the slug for visitors of a private page (the page ID and
	// SSO type are not disclosed to them by /api/public/pages/:slug/sso).
	r.GET("/api/auth/sso/login", middleware.NewRateLimiter(20, time.Minute), h.SSOLoginBySlugHandler)
	r.GET("/api/auth/oidc/callback", middleware.NewRateLimiter(20, time.Minute), h.OIDCCallbackHandler)
	// Global OIDC providers (OIDC_PROVIDERS), 20/min per IP like the page
	// OIDC routes; the callback triggers an outgoing call to the provider.
	r.GET("/api/auth/provider/:id/login", middleware.NewRateLimiter(20, time.Minute), h.ProviderLoginHandler)
	r.GET("/api/auth/provider/:id/callback", middleware.NewRateLimiter(20, time.Minute), h.ProviderCallbackHandler)
	r.POST("/api/auth/sso-exchange", middleware.NewRateLimiter(10, time.Minute), h.SSOExchangeHandler)
	// Sign-in methods for the login page (password switch, providers).
	r.GET("/api/public/auth-config", middleware.NewRateLimiter(60, time.Minute), h.GetAuthConfigHandler)

	// --- Public routes ---
	// 20/minute instead of 5: still protects effectively against brute force, but no longer collides
	// with our own e2e suite (several spec files log in per
	// run via the dev simulator, all from the same local IP).
	r.POST("/api/login", middleware.NewRateLimiter(20, time.Minute), h.LoginHandler)
	r.POST("/api/subscribe", middleware.NewRateLimiter(10, time.Minute), middleware.OptionalAuthMiddleware(), h.SubscribeHandler)
	publicGroup := r.Group("/api/public")
	publicGroup.Use(middleware.OptionalAuthMiddleware())
	publicGroup.GET("/pages", middleware.NewRateLimiter(120, time.Minute), h.GetPublicPagesHandler)
	// Read routes for incidents: generous (120/min per IP and route), so that a
	// single client cannot exhaust the database with dashboard requests; a
	// normal visitor makes a few requests per minute. Behind a
	// reverse proxy TRUSTED_PROXIES must be set, otherwise all
	// visitors share the proxy address.
	publicGroup.GET("/pages/:slug/dashboard", middleware.NewRateLimiter(120, time.Minute), h.GetPublicDashboardHandler)
	// 90-day availability (daily aggregates) for the status page.
	publicGroup.GET("/pages/:slug/availability", middleware.NewRateLimiter(60, time.Minute), h.GetPublicAvailabilityHandler)
	// bundled confirmed translations of the status page (lang=xx).
	publicGroup.GET("/pages/:slug/translations", middleware.NewRateLimiter(60, time.Minute), h.GetPublicTranslationsHandler)
	// confirmed translations of an incident's updates.
	publicGroup.GET("/pages/:slug/incidents/:incident_id/update-translations", middleware.NewRateLimiter(60, time.Minute), h.GetPublicIncidentUpdateTranslationsHandler)
	publicGroup.GET("/ui-overrides", middleware.NewRateLimiter(120, time.Minute), h.GetPublicUiOverridesHandler)
	// OptionalAuthMiddleware: private pages require a check
	// against the logged-in user (global admin or UserStatusPageRole),
	// anonymous requests are still allowed for public pages.
	r.GET("/api/pages/slug/:slug", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetStatusPageBySlugHandler)
	// Public read: components & incidents scoped by page
	// Security check: all routes here read data of a
	// StatusPage via page_id or a global kind ID and therefore must
	// know whether a user is logged in (pageIsAccessible check in the
	// respective handlers) - OptionalAuthMiddleware sets userID when a
	// valid token is present, but still lets anonymous requests through.
	r.GET("/api/pages/:page_id/components", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetComponentsHandler)
	r.GET("/api/pages/:page_id/incidents", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetIncidentsHandler)
	r.GET("/api/pages/:page_id/incidents/:id", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetIncidentDetailHandler)
	// Stage 2 of multilingual support: confirmed translations for public API
	r.GET("/api/incidents/:incident_id/translations", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetIncidentTranslationsHandler)
	// Stage 3 of multilingual support: confirmed component translations for public API
	r.GET("/api/components/:component_id/translations", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetComponentTranslationsHandler)
	// Stage 4 of multilingual support: confirmed notice translations for public API
	r.GET("/api/notices/:notice_id/translations", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetGeneralNoticeTranslationsHandler)
	// active general notices (public, filtered by IsActive + StartAt/EndAt window)
	r.GET("/api/pages/:page_id/notices", middleware.NewRateLimiter(120, time.Minute), middleware.OptionalAuthMiddleware(), h.GetActiveNoticesHandler)

	// Incoming monitoring webhook: deliberately NOT routed through
	// AuthMiddleware/JWT, but authenticates itself via the
	// X-Webhook-Token header against the token stored per status page
	// (see ComponentStatusWebhookHandler). Own, narrowly scoped rate limit
	// analogous to /api/login, so that this publicly reachable endpoint cannot be
	// abused for token brute force or status-value spam.
	// A shared counter for both routes (ID and identifier), otherwise
	// twice as many token attempts per IP would be possible.
	webhookLimit := middleware.NewRateLimiter(20, time.Minute)
	r.POST("/api/webhooks/components/:component_id/status", webhookLimit, h.ComponentStatusWebhookHandler)
	r.POST("/api/webhooks/component-slugs/:slug/status", webhookLimit, h.ComponentStatusWebhookBySlugHandler)

	// Custom-domain resolution: PublicRoot.vue calls this for hosts
	// that are neither a known app base domain nor a PaaS platform
	// domain, to determine the corresponding status page slug. Own
	// rate limit, since it is hit on every page view via a custom domain.
	r.GET("/api/public/resolve-domain", middleware.NewRateLimiter(60, time.Minute), middleware.OptionalAuthMiddleware(), h.ResolvePublicDomainHandler)
	// only reports whether page SSO is active (button "Sign in
	// with SSO"), rate limit like resolve-domain (queried on every page view).
	r.GET("/api/public/pages/:slug/sso", middleware.NewRateLimiter(60, time.Minute), middleware.OptionalAuthMiddleware(), h.GetPublicOIDCHandler)

	// Subscriber self-service: unsubscribe without login, via a
	// signed token from the notification email. Rate limit analogous to
	// other unauthenticated public endpoints.
	// Deletion only via POST (frontend button or RFC 8058 one-
	// click), GET only redirects to the frontend page /unsubscribe.
	r.GET("/api/public/subscribers/unsubscribe", middleware.NewRateLimiter(20, time.Minute), h.UnsubscribeRedirectHandler)
	r.POST("/api/public/subscribers/unsubscribe", middleware.NewRateLimiter(20, time.Minute), h.UnsubscribeHandler)
	// Double opt-in confirmation via token, rate limit like
	// /api/public/orders/confirm.
	r.POST("/api/public/subscribers/confirm", middleware.NewRateLimiter(20, time.Minute), h.ConfirmSubscriptionHandler)

	// Self-service ordering of new status pages via the home page
	// ("gateway" homepage) - see handlers/page_order.go for the
	// three-step flow and the abuse protection (honeypot + email
	// confirmation + cap on open orders per email, in addition to the
	// IP rate limit set here). Tighter than /api/subscribe, since every hit
	// triggers a DB row AND an email send.
	// public flag endpoint (no auth) on whether self-service
	// ordering is active - landing CTA and /order page depend on it.
	r.GET("/api/public/orders/enabled", middleware.NewRateLimiter(60, time.Minute), h.GetPublicOrdersEnabledHandler)
	// Public flag (no auth): whether automatic translation and the second content
	// language are switched on. Admin views of page admins hide translation UI by it.
	r.GET("/api/public/translation/enabled", middleware.NewRateLimiter(60, time.Minute), h.GetPublicTranslationEnabledHandler)
	r.POST("/api/public/orders", middleware.NewRateLimiter(5, time.Minute), h.CreateOrderHandler)
	r.POST("/api/public/orders/confirm", middleware.NewRateLimiter(20, time.Minute), h.ConfirmOrderHandler)
	r.POST("/api/public/orders/set-password", middleware.NewRateLimiter(10, time.Minute), h.SetOrderPasswordHandler)
	// Called by the reverse proxy (e.g. Caddy on-demand TLS) as the "ask" endpoint
	// (see README, operations section) - runs in the internal network. The limit
	// per IP (120/min, burst 120, i.e. 2 requests per second on average) catches
	// the case where the endpoint is reachable after all (empty token in
	// development, misconfiguration) and would otherwise serve as an unlimited oracle for the
	// status of foreign domains. Caddy only asks for hosts without a
	// cached certificate (initial issuance, renewal), not per
	// TLS handshake; the upper limit is far above the real need, even if
	// all requests come through the same proxy IP. On 429 Caddy retries
	// at the next handshake.
	r.GET("/api/internal/custom-domain-check", middleware.NewRateLimiter(120, time.Minute), h.InternalCustomDomainCheckHandler)
}

// registerAuthRoutes registers all routes that require AuthMiddleware()
// (JWT-protected) - split out of newRouter to keep its cognitive complexity
// low (SonarCloud go:S3776).
func registerAuthRoutes(r *gin.Engine, h *handlers.Handler) {
	// --- Protected routes ---
	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	// A session handed to a custom domain never acts on the account itself
	// (see middleware.RejectCustomDomainSession).
	noCustomDomain := middleware.RejectCustomDomainSession()
	authGroup.GET("/me", h.MeHandler)
	authGroup.POST("/auth/oidc/link", noCustomDomain, h.OIDCLinkHandler)
	authGroup.POST("/auth/provider/:id/link", noCustomDomain, h.ProviderLinkHandler)
	// show and unlink own SSO links (profile).
	authGroup.GET("/me/sso", h.GetMySSOHandler)
	authGroup.DELETE("/me/sso/:id", noCustomDomain, h.DeleteMySSOHandler)
	authGroup.PATCH("/me", h.UpdateMeHandler)
	// GDPR data export: own data as a JSON download,
	// never data of other users - see handlers/me_export.go.
	authGroup.GET("/me/export", noCustomDomain, h.MeExportHandler)
	// Delete account - see handlers/me_delete.go.
	authGroup.DELETE("/me", noCustomDomain, h.DeleteMeHandler)
	authGroup.GET("/me/pages", h.GetMyPagesHandler)
	authGroup.GET("/me/subscription", h.GetMySubscriptionHandler)
	authGroup.PUT("/me/subscription", h.UpdateMySubscriptionHandler)
	authGroup.DELETE("/me/subscription", h.DeleteMySubscriptionHandler)
	// view, accept or decline own invitation (token from the mail).
	// Only when logged in with the invited account; token in the body, never in
	// the URL. Limited like the other token endpoints.
	invitationLimit := middleware.NewRateLimiter(20, time.Minute)
	authGroup.POST("/invitations/preview", invitationLimit, h.PreviewPageInvitationHandler)
	authGroup.POST("/invitations/accept", invitationLimit, h.AcceptPageInvitationHandler)
	authGroup.POST("/invitations/decline", invitationLimit, h.DeclinePageInvitationHandler)

	// Returns ALL status pages unfiltered (including private/unlisted) - only for
	// Global Admins (page/user management screens). Previously it was completely
	// publicly reachable and leaked the names/slugs of all tenants.
	guarded(authGroup, http.MethodGet, "/pages", platform("pages", permissions.Read), h.GetStatusPagesHandler)

	// StatusPage Management (Ressource pages)
	// Whoever manages users of a page needs pages:<action> on exactly this
	// page (or global admin) - previously this was completely unprotected (any
	// logged-in user could assign themselves arbitrary roles on arbitrary pages).
	// The route param is deliberately named :page_id, because RequirePermission expects exactly
	// this name. Each route checks the permission matching the HTTP
	// method, see guarded() in route_permissions.go.
	guarded(authGroup, http.MethodGet, "/admin/pages/:page_id/users", need("pages", permissions.Read), h.GetStatusPageUsersHandler)
	guarded(authGroup, http.MethodPost, "/admin/pages/:page_id/users", need("pages", permissions.Create), middleware.NewRateLimiter(20, time.Minute), h.AddStatusPageUserHandler)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id/users/:uid", need("pages", permissions.Delete), h.RemoveStatusPageUserHandler)
	// view and withdraw open invitations of the page.
	guarded(authGroup, http.MethodGet, "/admin/pages/:page_id/invitations", need("pages", permissions.Read), h.GetPageInvitationsHandler)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id/invitations/:id", need("pages", permissions.Delete), h.RevokePageInvitationHandler)

	// Creating new status pages is a platform operation (there is no
	// page_id yet that pages:create could be scoped to) - requires global admin.
	guarded(authGroup, http.MethodPost, "/admin/pages", platform("pages", permissions.Create), h.CreateStatusPageHandler)
	guarded(authGroup, http.MethodPut, "/admin/pages/:page_id", need("pages", permissions.Update), h.UpdateStatusPageHandler)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id", need("pages", permissions.Delete), h.DeleteStatusPageHandler)
	// Monitoring webhook token per status page: generating returns
	// the plaintext token once, revoking invalidates it again.
	// Both change the page (pages:update).
	guarded(authGroup, http.MethodPost, "/admin/pages/:page_id/webhook-token", need("pages", permissions.Update), h.GenerateWebhookTokenHandler)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id/webhook-token", need("pages", permissions.Update), h.RevokeWebhookTokenHandler)
	// Custom-domain management: own endpoints instead of part of the
	// generic statusPageWritableFields bind, see the comment there. Setting,
	// Removing and verifying change the page (pages:update).
	guarded(authGroup, http.MethodGet, "/admin/pages/:page_id/custom-domain", need("pages", permissions.Read), h.GetCustomDomainHandler)
	guarded(authGroup, http.MethodPut, "/admin/pages/:page_id/custom-domain", need("pages", permissions.Update), h.SetCustomDomainHandler)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id/custom-domain", need("pages", permissions.Update), h.DeleteCustomDomainHandler)
	guarded(authGroup, http.MethodPost, "/admin/pages/:page_id/custom-domain/verify", need("pages", permissions.Update), handlers.CustomDomainVerifyLimit(), h.VerifyCustomDomainHandler)
	// SSO configuration per status page: OIDC (resource sso)
	guarded(authGroup, http.MethodGet, "/admin/pages/:page_id/sso", need("sso", permissions.Read), h.GetSSO)
	guarded(authGroup, http.MethodPut, "/admin/pages/:page_id/sso", need("sso", permissions.Update), h.UpdateSSO)
	guarded(authGroup, http.MethodDelete, "/admin/pages/:page_id/sso", need("sso", permissions.Delete), h.DeleteSSO)
	// Test login (test_login_enabled), rate limit like the
	// OIDC login, because every call triggers discovery and a login state.
	// Special action: toggles the configuration's test login (sso:update).
	guarded(authGroup, http.MethodPost, "/admin/pages/:page_id/sso/test", need("sso", permissions.Update), middleware.NewRateLimiter(20, time.Minute), h.StartSSOTestHandler)

	// Components – scoped under /pages/:page_id
	guarded(authGroup, http.MethodPost, "/pages/:page_id/components", need("components", permissions.Create), h.CreateComponentHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/components/:id", need("components", permissions.Update), h.UpdateComponentHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/components/:id", need("components", permissions.Delete), h.DeleteComponentHandler)
	// Archiving/restoring is a change (components:update), not a deletion.
	guarded(authGroup, http.MethodPost, "/pages/:page_id/components/:id/archive", need("components", permissions.Update), h.ArchiveComponentHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/components/:id/restore", need("components", permissions.Update), h.RestoreComponentHandler)

	// externe Statusseiten (Quellen, externe Komponenten, Kopplung) – scoped under /pages/:page_id
	registerExternalStatusRoutes(authGroup, h)

	// Incidents and maintenances - scoped under /pages/:page_id. Maintenances share
	// the routes: byKind checks incidents or maintenance depending
	// on the kind of the record (on create: field IsMaintenance in the body).
	// Everything that notifies subscribers has a limit per page and user
	// (handlers/notify_rate_limit.go); main_notification_limit_test.go checks
	// that no route with mail sending remains without a limit. Create: 30 per page
	// and hour, changes and updates: 120.
	notifyCreate, notifyChange := handlers.NotificationCreateLimit(), handlers.NotificationChangeLimit()
	guarded(authGroup, http.MethodPost, "/pages/:page_id/incidents", byKind(permissions.Create, h.IncidentResourceFromBody()), notifyCreate, h.CreateIncidentHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/incidents/:id", byKind(permissions.Update, h.IncidentResourceByParam("id")), notifyChange, h.UpdateIncidentHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/incidents/:id", byKind(permissions.Delete, h.IncidentResourceByParam("id")), h.DeleteIncidentHandler)
	// Special action: a timeline entry creates a record (create).
	guarded(authGroup, http.MethodPost, "/pages/:page_id/incidents/:id/updates", byKind(permissions.Create, h.IncidentResourceByParam("id")), notifyChange, h.AddIncidentUpdateHandler)
	// Read and save per language the translations of an update (drafts included).
	guarded(authGroup, http.MethodGet, "/pages/:page_id/incidents/:id/updates/:update_id/translations", byKind(permissions.Read, h.IncidentResourceByParam("id")), h.GetIncidentUpdateTranslationsHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/incidents/:id/updates/:update_id/translations/:language_code", byKind(permissions.Update, h.IncidentResourceByParam("id")), h.RequireTranslationEnabled(), h.SaveIncidentUpdateTranslationHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/incidents/:id/postmortem", byKind(permissions.Update, h.IncidentResourceByParam("id")), h.UpsertPostMortemHandler)

	// Stage 2 of multilingual support: incident translations (draft/confirmed)
	// the paths carry no :page_id - the
	// *PageScope middlewares derive it from the resource, so that
	// RequirePermission also applies to page admins (not only global admins)
	// and rejects foreign pages with 403 (handlers/translation_page_scope.go).
	// Translations follow the permissions of the resource of their content.
	guarded(authGroup, http.MethodPost, "/incidents/:incident_id/translations", byKind(permissions.Create, h.IncidentResourceByParam("incident_id")).after(h.IncidentPageScope()), h.IncidentTagGuard(), h.RequireTranslationEnabled(), h.CreateIncidentTranslationHandler)
	// Admin list including drafts (the public route above returns only confirmed).
	guarded(authGroup, http.MethodGet, "/incidents/:incident_id/translations/all", byKind(permissions.Read, h.IncidentResourceByParam("incident_id")).after(h.IncidentPageScope()), h.IncidentTagGuard(), h.GetAllIncidentTranslationsHandler)
	// Save the translation of one language from the edit form (create or change, draft or final).
	// Translation suggestion: reads/writes no record, therefore needs
	// only update on one of the three text types; calls are limited.
	guarded(authGroup, http.MethodPost, "/pages/:page_id/translate", anyOf(permissions.Update, "incidents", "maintenance", "notices"), middleware.NewRateLimiter(30, time.Minute), h.RequireTranslationEnabled(), h.TranslateTextHandler)
	guarded(authGroup, http.MethodPut, "/incidents/:incident_id/translations/:language_code", byKind(permissions.Update, h.IncidentResourceByParam("incident_id")).after(h.IncidentPageScope()), h.IncidentTagGuard(), h.RequireTranslationEnabled(), h.SaveIncidentTranslationHandler)
	// Special action: confirming a draft changes the translation (update).
	guarded(authGroup, http.MethodPatch, "/incident-translations/:translation_id/confirm", byKind(permissions.Update, h.IncidentResourceByTranslation()).after(h.IncidentTranslationPageScope()), h.IncidentTranslationTagGuard(), h.RequireTranslationEnabled(), h.ConfirmIncidentTranslationHandler)
	guarded(authGroup, http.MethodDelete, "/incident-translations/:translation_id", byKind(permissions.Delete, h.IncidentResourceByTranslation()).after(h.IncidentTranslationPageScope()), h.IncidentTranslationTagGuard(), h.RequireTranslationEnabled(), h.DeleteIncidentTranslationHandler)

	// Stage 3 of multilingual support: component translations (draft/confirmed)
	guarded(authGroup, http.MethodPost, "/components/:component_id/translations", need("components", permissions.Create).after(h.ComponentPageScope()), h.ComponentTagGuard(), h.RequireTranslationEnabled(), h.CreateComponentTranslationHandler)
	guarded(authGroup, http.MethodGet, "/components/:component_id/translations/all", need("components", permissions.Read).after(h.ComponentPageScope()), h.ComponentTagGuard(), h.GetAllComponentTranslationsHandler)
	guarded(authGroup, http.MethodPut, "/components/:component_id/translations/:language_code", need("components", permissions.Update).after(h.ComponentPageScope()), h.ComponentTagGuard(), h.RequireTranslationEnabled(), h.SaveComponentTranslationHandler)
	guarded(authGroup, http.MethodPatch, "/component-translations/:translation_id/confirm", need("components", permissions.Update).after(h.ComponentTranslationPageScope()), h.ComponentTranslationTagGuard(), h.RequireTranslationEnabled(), h.ConfirmComponentTranslationHandler)
	guarded(authGroup, http.MethodDelete, "/component-translations/:translation_id", need("components", permissions.Delete).after(h.ComponentTranslationPageScope()), h.ComponentTranslationTagGuard(), h.RequireTranslationEnabled(), h.DeleteComponentTranslationHandler)

	// Stage 4 of multilingual support: notice translations (draft/confirmed)
	guarded(authGroup, http.MethodPost, "/notices/:notice_id/translations", need("notices", permissions.Create).after(h.NoticePageScope()), h.RequireTranslationEnabled(), h.CreateGeneralNoticeTranslationHandler)
	guarded(authGroup, http.MethodGet, "/notices/:notice_id/translations/all", need("notices", permissions.Read).after(h.NoticePageScope()), h.GetAllGeneralNoticeTranslationsHandler)
	// Save the translation of one language from the edit form.
	guarded(authGroup, http.MethodPut, "/notices/:notice_id/translations/:language_code", need("notices", permissions.Update).after(h.NoticePageScope()), h.RequireTranslationEnabled(), h.SaveGeneralNoticeTranslationHandler)
	guarded(authGroup, http.MethodPatch, "/notice-translations/:translation_id/confirm", need("notices", permissions.Update).after(h.NoticeTranslationPageScope()), h.RequireTranslationEnabled(), h.ConfirmGeneralNoticeTranslationHandler)
	guarded(authGroup, http.MethodDelete, "/notice-translations/:translation_id", need("notices", permissions.Delete).after(h.NoticeTranslationPageScope()), h.RequireTranslationEnabled(), h.DeleteGeneralNoticeTranslationHandler)

	// General Notices – scoped under /pages/:page_id
	guarded(authGroup, http.MethodGet, "/pages/:page_id/notices/all", need("notices", permissions.Read), h.GetNoticesHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/notices", need("notices", permissions.Create), notifyCreate, h.CreateNoticeHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/notices/:id", need("notices", permissions.Update), notifyChange, h.UpdateNoticeHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/notices/:id", need("notices", permissions.Delete), h.DeleteNoticeHandler)

	// Incident-Vorlagen – scoped under /pages/:page_id, eigene
	// Resource incident_templates (previously incidents:write also for reading).
	guarded(authGroup, http.MethodGet, "/pages/:page_id/incident-templates", need("incident_templates", permissions.Read), h.GetIncidentTemplatesHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/incident-templates", need("incident_templates", permissions.Create), h.CreateIncidentTemplateHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/incident-templates/:id", need("incident_templates", permissions.Update), h.UpdateIncidentTemplateHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/incident-templates/:id", need("incident_templates", permissions.Delete), h.DeleteIncidentTemplateHandler)
	// Translations of a template (language switcher in the form).
	guarded(authGroup, http.MethodGet, "/pages/:page_id/incident-templates/:id/translations", need("incident_templates", permissions.Read), h.GetIncidentTemplateTranslationsHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/incident-templates/:id/translations/:language_code", need("incident_templates", permissions.Update), h.RequireTranslationEnabled(), h.SaveIncidentTemplateTranslationHandler)

	// Notification mail templates - page-scoped under
	// /pages/:page_id/notification-templates, Ressource notification_templates
	// (previously pages:write): configure templates for how the page
	// sends messages. Kind + audience + language are in the path, the ID is not -
	// the UI upserts directly on this quartet and does not have to track any ID state
	// (see handler comment), so saving is therefore a PUT
	// (update). Audience was added with stage 5 of multilingual support.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/notification-templates", need("notification_templates", permissions.Read), h.GetNotificationTemplatesHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/notification-templates/:kind/:audience/:language", need("notification_templates", permissions.Update), h.PutNotificationTemplateHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/notification-templates/:kind/:audience/:language", need("notification_templates", permissions.Delete), h.DeleteNotificationTemplateHandler)
	// Special action: preview only renders, changes nothing (read).
	guarded(authGroup, http.MethodPost, "/pages/:page_id/notification-templates/:kind/:audience/:language/preview", need("notification_templates", permissions.Read), h.PreviewNotificationTemplateHandler)

	// Language catalog (stage 1 of multilingual support): creating/changing
	// catalog entries is global-admin-only (analogous to tags/roles).
	// Reading, by contrast, is deliberately open to every logged-in user (unlike
	// the tag catalog) - a page admin without global admin rights must
	// still be able to see the catalog in order to enable languages for their status page via
	// PutPageLanguagesHandler.
	authGroup.GET("/languages", h.GetLanguagesHandler)
	guarded(authGroup, http.MethodPost, "/languages", platform("languages", permissions.Create), h.CreateLanguageHandler)
	guarded(authGroup, http.MethodPut, "/languages/:id", platform("languages", permissions.Update), h.UpdateLanguageHandler)
	guarded(authGroup, http.MethodDelete, "/languages/:id", platform("languages", permissions.Delete), h.DeleteLanguageHandler)

	// Reading only with a role on the page - the
	// public frontend does not use the endpoint (only admin views
	// via useContentLanguages.js); previously any logged-in user could
	// read the language configuration of foreign pages. Every page role may read,
	// because the translation panels of the other tabs need the languages.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/languages", pageRole("languages"), h.GetPageLanguagesHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/languages", need("languages", permissions.Update), h.PutPageLanguagesHandler)

	// UI translation overrides: global overrides
	// (global admin, apply app-wide as the new default) + per-page
	// overrides (resource ui_translations, own page only, apply only in the
	// admin area of that page - see frontend/src/layouts/AdminLayout.vue).
	// Deliberately nested under /pages/:page_id/... (not flat like the content
	// translation routes), so that RequirePermission can actually
	// scope per page.
	guarded(authGroup, http.MethodGet, "/admin/ui-overrides", platform("ui_translations", permissions.Read), h.GetGlobalUiOverridesHandler)
	guarded(authGroup, http.MethodPut, "/admin/ui-overrides", platform("ui_translations", permissions.Update), h.PutGlobalUiOverrideHandler)
	guarded(authGroup, http.MethodDelete, "/admin/ui-overrides", platform("ui_translations", permissions.Delete), h.DeleteGlobalUiOverrideHandler)

	// Reading only with a role on the page (loaded
	// by AdminLayout.vue in the page's admin area); anonymous visitors
	// get the global overrides via /api/public/ui-overrides.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/ui-overrides", pageRole("ui_translations"), h.GetPageUiOverridesHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/ui-overrides", need("ui_translations", permissions.Update), h.PutPageUiOverrideHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/ui-overrides", need("ui_translations", permissions.Delete), h.DeletePageUiOverrideHandler)

	// Subscribers – scoped under /pages/:page_id
	guarded(authGroup, http.MethodGet, "/pages/:page_id/subscribers", need("subscribers", permissions.Read), h.GetSubscribersHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/subscribers", need("subscribers", permissions.Create), middleware.NewRateLimiter(20, time.Minute), h.CreateSubscriberAdminHandler)
	// Test message from the subscriber form (create or edit), before saving.
	guarded(authGroup, http.MethodPost, "/pages/:page_id/subscribers/test", anyAction("subscribers", permissions.Create, permissions.Update), middleware.NewRateLimiter(5, time.Minute), h.SendSubscriberTestMessageHandler)
	// Readers may test a stored webhook (only the stored target, never a free URL).
	guarded(authGroup, http.MethodPost, "/pages/:page_id/subscribers/:id/test-webhook", need("subscribers", permissions.Read), middleware.NewRateLimiter(10, time.Minute), h.TestStoredSubscriberWebhookHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/subscribers/:id", need("subscribers", permissions.Update), notifyChange, h.UpdateSubscriberHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/subscribers/:id", need("subscribers", permissions.Delete), h.DeleteSubscriberHandler)

	// Users & roles manage users/permissions globally (no page_id scope possible) -
	// RequirePermission is structurally unsuitable for that (it requires
	// c.Param("page_id")) and would so far have rejected every non-global admin with 400,
	// regardless of which permissions the role carries. need() chooses for
	// global resources (permissions.ScopeGlobal) RequireGlobalAdmin.
	guarded(authGroup, http.MethodGet, "/users", need("users", permissions.Read), h.GetUsersHandler)
	guarded(authGroup, http.MethodPost, "/users", need("users", permissions.Create), h.CreateUserHandler)
	guarded(authGroup, http.MethodPut, "/users/:id", need("users", permissions.Update), h.UpdateUserHandler)
	guarded(authGroup, http.MethodDelete, "/users/:id", need("users", permissions.Delete), h.DeleteUserHandler)

	guarded(authGroup, http.MethodGet, "/roles", need("roles", permissions.Read), h.GetRolesHandler)
	guarded(authGroup, http.MethodPost, "/roles", need("roles", permissions.Create), h.CreateRoleHandler)
	guarded(authGroup, http.MethodPut, "/roles/:id", need("roles", permissions.Update), h.UpdateRoleHandler)
	guarded(authGroup, http.MethodDelete, "/roles/:id", need("roles", permissions.Delete), h.DeleteRoleHandler)

	// Tag catalog (step 3/4): global admin-only, analogous to
	// users & roles - the management page shows global AND
	// status-page-bound tags in one overview, a page_id-scoped
	// RequirePermission would be structurally unsuitable for that. Assigning
	// tags to components/incidents/users follows later.
	guarded(authGroup, http.MethodGet, "/tags", need("tags", permissions.Read), h.GetTagsHandler)
	// selectable tags (global + page-specific) with page permission, so that
	// page roles can use the tag field in component/incident forms.
	// any resource whose forms have a tag field suffices.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/tags", anyOf(permissions.Read, "components", "incidents", "maintenance", "external_sources"), h.GetPageTagsHandler)
	guarded(authGroup, http.MethodPost, "/tags", need("tags", permissions.Create), h.CreateTagHandler)
	guarded(authGroup, http.MethodPut, "/tags/:id", need("tags", permissions.Update), h.UpdateTagHandler)
	guarded(authGroup, http.MethodDelete, "/tags/:id", need("tags", permissions.Delete), h.DeleteTagHandler)

	// Global system settings: global admins only, analogous to
	// users/roles - there is no page_id scope that RequirePermission could
	// apply to. Currently only self-service ordering on/off.
	guarded(authGroup, http.MethodGet, "/settings", need("system_settings", permissions.Read), h.GetSettingsHandler)
	guarded(authGroup, http.MethodPut, "/settings", need("system_settings", permissions.Update), h.UpdateSettingsHandler)
	// Automatic translation: service and credentials (key never in
	// the response); the connection test changes nothing, but calls the service.
	guarded(authGroup, http.MethodGet, "/settings/translation", need("system_settings", permissions.Read), h.GetTranslationSettingsHandler)
	guarded(authGroup, http.MethodPut, "/settings/translation", need("system_settings", permissions.Update), h.UpdateTranslationSettingsHandler)
	guarded(authGroup, http.MethodPost, "/settings/translation/test", need("system_settings", permissions.Update), middleware.NewRateLimiter(10, time.Minute), h.TestTranslationSettingsHandler)

	// Tenant overview + audit log (global admins only)
	guarded(authGroup, http.MethodGet, "/admin/overview", need("overview", permissions.Read), h.GetOverviewHandler)
	// In-app monitoring dashboard (Golden Signals step 3/4) -
	// reads from the metrics.Default registry instrumented in step 1/4.
	guarded(authGroup, http.MethodGet, "/admin/monitoring", need("monitoring", permissions.Read), h.GetMonitoringHandler)
	guarded(authGroup, http.MethodGet, "/admin/audit", need("audit", permissions.Read), h.GetAuditLogsHandler)
}

// registerExternalStatusRoutes: external status pages, resource
// external_sources (previously components:read/write, see
// handlers/external_source.go).
func registerExternalStatusRoutes(authGroup *gin.RouterGroup, h *handlers.Handler) {
	const res = "external_sources"
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-sources", need(res, permissions.Read), h.GetExternalSourcesHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/external-sources", need(res, permissions.Create), h.CreateExternalSourceHandler)
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-sources/:id", need(res, permissions.Read), h.GetExternalSourceHandler)
	guarded(authGroup, http.MethodPut, "/pages/:page_id/external-sources/:id", need(res, permissions.Update), h.UpdateExternalSourceHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/external-sources/:id", need(res, permissions.Delete), h.DeleteExternalSourceHandler)
	// Special action "Fetch now": changes the state of the source (update).
	guarded(authGroup, http.MethodPost, "/pages/:page_id/external-sources/:id/fetch", need(res, permissions.Update), middleware.NewRateLimiter(20, time.Minute), h.FetchExternalSourceHandler)
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-sources/:id/components", need(res, permissions.Read), h.GetExternalComponentsHandler)
	// adopt external building blocks together with their own component and link them.
	// Special action "Adopt": creates own components, therefore needs
	// external_sources:create AND components:create (otherwise component
	// management could be bypassed via the import).
	guarded(authGroup, http.MethodPost, "/pages/:page_id/external-sources/:id/import", allOf(permissions.Create, res, "components"), h.ImportExternalComponentsHandler)

	// active source outages for the admin banner.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-outages", need(res, permissions.Read), h.GetExternalOutagesHandler)

	// Admin message center (bell). Page messages are
	// operational notices for editors: they require external_sources:update
	// (source outages) OR subscribers:update (webhook delivery) - the same
	// effective level as the earlier components:write/subscribers:write.
	// Reading and "mark as read" also need it (special case: no
	// read action), because the messages are meant for editors. System-wide
	// messages are reserved for global admins (resource monitoring); the
	// action there follows the HTTP method but has no effect, since only global
	// admins get through. The handlers also check the role themselves.
	pageNotif := func() routeGuard { return anyOf(permissions.Update, "external_sources", "subscribers") }
	guarded(authGroup, http.MethodGet, "/pages/:page_id/admin-notifications", pageNotif(), h.ListAdminNotificationsHandler)
	guarded(authGroup, http.MethodGet, "/pages/:page_id/admin-notifications/unread-count", pageNotif(), h.AdminNotificationUnreadCountHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/admin-notifications/read-all", pageNotif(), h.MarkAllAdminNotificationsReadHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/admin-notifications/:id/read", pageNotif(), h.MarkAdminNotificationReadHandler)
	guarded(authGroup, http.MethodGet, "/admin-notifications", platform("monitoring", permissions.Read), h.ListAdminNotificationsHandler)
	guarded(authGroup, http.MethodGet, "/admin-notifications/unread-count", platform("monitoring", permissions.Read), h.AdminNotificationUnreadCountHandler)
	guarded(authGroup, http.MethodPost, "/admin-notifications/read-all", platform("monitoring", permissions.Create), h.MarkAllAdminNotificationsReadHandler)
	guarded(authGroup, http.MethodPost, "/admin-notifications/:id/read", platform("monitoring", permissions.Create), h.MarkAdminNotificationReadHandler)

	// B2: mirrored incidents and maintenances that affect linked components.
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-incidents", need(res, permissions.Read), h.GetExternalIncidentsHandler)
	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-incidents/:id", need(res, permissions.Read), h.GetExternalIncidentHandler)

	guarded(authGroup, http.MethodGet, "/pages/:page_id/external-links", need(res, permissions.Read), h.GetExternalLinksHandler)
	guarded(authGroup, http.MethodPost, "/pages/:page_id/external-links", need(res, permissions.Create), h.CreateExternalLinkHandler)
	guarded(authGroup, http.MethodDelete, "/pages/:page_id/external-links/:id", need(res, permissions.Delete), h.DeleteExternalLinkHandler)
}

// staticRoot is the directory with the built Vue frontend in the runtime image.
const staticRoot = "./static"

// registerStaticFrontend mounts the built Vue frontend if present
// (local backend-only development still runs against the Vite dev server).
func registerStaticFrontend(r *gin.Engine) {
	registerStaticFrontendFrom(r, staticRoot)
}

// registerStaticFrontendFrom serves the frontend from root. Order:
// /assets, then every existing file in the build, otherwise index.html (SPA).
//
// Previously only a fixed list (/favicon.svg, /icons.svg) was in the router. Everything
// else from frontend/public/ fell back to index.html: the logo (/logo.png)
// and the self-hosted fonts (/fonts/*.woff2) arrived as HTML in prod
// (2026-10-29); it went unnoticed in the Vite dev server because it serves public/
// itself. Now it is enough to drop a file into public/.
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

// staticFileFor returns the path of a file under root if urlPath points to an
// existing regular file, otherwise false. Rejected are the home page itself,
// directories and hidden files (.xyz anywhere).
// ".." is resolved beforehand by path.Clean and cannot lead out of root.
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
	// No escape from root possible: for a path with a
	// leading "/", path.Clean resolves every ".." (TestStaticFileFor, ../ variants in
	// TestStatic_GibtKeineGeheimnisseOderVerzeichnisseHeraus).
	// After Clean no ".." can remain as a path element; refusing it explicitly is the
	// guard CodeQL (go/path-injection) recognises before the file system call below.
	// Only the requested part is checked, so the configured root may contain "..".
	if strings.Contains(clean, "..") {
		return "", false
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	// Resolve links and check the result first, only then touch the file: a link in
	// the build folder must not point to files outside. Links within the folder
	// remain allowed. EvalSymlinks fails for a missing file.
	real, err := filepath.EvalSymlinks(full)
	if err != nil || !withinRoot(root, real) {
		return "", false
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return real, true
}

// withinRoot reports whether path lies under root after resolving all links.
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

// serverTimeouts bundles the time limits of the HTTP server.
type serverTimeouts struct {
	ReadHeader time.Duration // until the request headers are complete
	Read       time.Duration // until the entire request including body is read
	Write      time.Duration // until the response is written
	Idle       time.Duration // keep-alive connection without a request
}

// defaultServerTimeouts: The API has no streaming responses. ReadHeader
// mitigates Slowloris; Read covers bodies up to a few MiB (BodyLimit); Write is
// above the longest calls to external services in a handler (translation,
// webhook test, each at most about 15 s).
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

// runServer starts the HTTP server in the background and blocks until
// SIGINT/SIGTERM arrives, then performs a graceful shutdown.
func runServer(r *gin.Engine) {
	handlers.WarnIfCustomDomainCheckUnprotected()
	handlers.WarnAuthConfig()
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
	// Before anything else: a weak signing secret allows forged tokens.
	middleware.RequireStrongJWTSecret()

	database.InitDB()
	h := handlers.NewHandler(database.DB)
	h.StartMaintenanceScheduler()
	defer handlers.StartSSOCleanupWorker(context.Background())() // see the doc on StartSSOCleanupWorker
	// Fetching external status pages; stopped on shutdown after runServer.
	defer externalstatus.StartFromEnv(context.Background(), h.ExternalStatus, os.Getenv)()
	// Self-monitoring sets the component status of the system page.
	defer h.StartSelfMonitorFromEnv(context.Background(), os.Getenv)()

	// Saturation signal (Golden Signals step 4/4): DB
	// Connection pool utilization. Must happen only after InitDB(), because the
	// underlying *sql.DB does not exist before; see
	// metrics.Registry.SaturationSnapshot / SetDBStatsProvider.
	if sqlDB, err := database.DB.DB(); err == nil {
		metrics.Default.SetDBStatsProvider(sqlDB.Stats)
	} else {
		slog.Warn("Konnte DBStatsProvider für Saturation-Metriken nicht registrieren", "error", err)
	}

	r := newRouter(h)
	runServer(r)
}
