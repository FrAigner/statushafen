package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

func getJWTSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		logging.Fatal("JWT_SECRET environment variable is required but not set")
	}
	return []byte(secret)
}

type Claims struct {
	UserID            uint   `json:"user_id"`
	Email             string `json:"email"`
	Role              string `json:"role"`
	Permissions       string `json:"permissions"`
	IsGlobalAdmin     bool   `json:"is_global_admin"`
	Name              string `json:"name,omitempty"`
	SSOProvider       string `json:"sso,omitempty"`
	PreferredLanguage string `json:"preferred_language,omitempty"`
	// HideHelpTooltips: SSO logins set the user directly from
	// the token, without loading /api/me. Without the claim the tooltips would be
	// on again there after every SSO login.
	HideHelpTooltips bool `json:"hide_help_tooltips,omitempty"`
	// SSOPageID: page whose IdP issued the session
	// (page SSO via OIDC). The session applies only to this page
	// and never with global admin rights. Absent for password and
	// global SSO logins.
	SSOPageID uint `json:"sso_page_id,omitempty"`
	// CustomDomain: the verified custom domain the session was handed to
	// (always with SSOPageID of that domain's page). Whoever controls the
	// domain's DNS can read the session there, so it never reaches the
	// account itself (RejectCustomDomainSession).
	CustomDomain string `json:"custom_domain,omitempty"`
	// TokenVersion: state of users.token_version at issuance. If it
	// differs from the account, the token was revoked (see sessionStillValid).
	TokenVersion uint `json:"tv"`
	jwt.RegisteredClaims
}

// tokenTTL is the access token lifetime for all login paths (password + SSO).
const tokenTTL = 2 * time.Hour

// AllowedCountries was removed from the claim (Country → Tags
// step 4/4). Tag-based access checks now read a user's tags
// fresh from the DB (see handlers/tag_access.go, userTagsAndAdmin) instead of
// from a JWT claim - so a tag change takes effect immediately, without the user
// having to log in again.
func GenerateToken(userID uint, email string, role string, permissions string, isGlobalAdmin bool, opts ...TokenOption) (string, error) {
	claims := &Claims{
		UserID:        userID,
		Email:         email,
		Role:          role,
		Permissions:   permissions,
		IsGlobalAdmin: isGlobalAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
		},
	}

	for _, opt := range opts {
		opt(claims)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJWTSecret())
}

// TokenOption adjusts the claims of a login token at issuance.
type TokenOption func(*Claims)

// WithTokenVersion records the account's current users.token_version.
// Without the option, version 0 applies (account whose version was never raised).
func WithTokenVersion(v uint) TokenOption {
	return func(c *Claims) { c.TokenVersion = v }
}

// WithSSOPageID limits the session to one page (sso_page_id, see
// Claims.SSOPageID); 0 leaves it unlimited. The caller drops global admin
// rights itself (GenerateToken's isGlobalAdmin).
func WithSSOPageID(pageID uint) TokenOption {
	return func(c *Claims) { c.SSOPageID = pageID }
}

// WithCustomDomain marks the session as handed to a custom domain (see
// Claims.CustomDomain); "" leaves it unmarked.
func WithCustomDomain(host string) TokenOption {
	return func(c *Claims) { c.CustomDomain = host }
}

// GenerateTokenFull is the central token factory for SSO logins, incl. name and
// SSO provider as additional claims (replaces the former inline signing in auth_sso.go).
func GenerateTokenFull(u models.User, permissions, name, sso string) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, fullClaims(u, permissions, name, sso)).SignedString(getJWTSecret())
}

// GeneratePageSSOToken issues the token after a page SSO
// login: like GenerateTokenFull, additionally with sso_page_id. A page
// admin controls their page's IdP and can thereby forge assertions or
// ID tokens for any user linked there. Permissions are read fresh from the DB on each
// request; without page scope the token would immediately get
// any role the user later receives elsewhere (including Global Admin).
func GeneratePageSSOToken(u models.User, name, protocol string, pageID uint, opts ...TokenOption) (string, error) {
	claims := fullClaims(u, u.Role.Permissions, name, protocol)
	for _, opt := range opts {
		opt(claims)
	}
	claims.SSOPageID = pageID
	claims.IsGlobalAdmin = false
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(getJWTSecret())
}

func fullClaims(u models.User, permissions, name, sso string) *Claims {
	return &Claims{
		UserID:            u.ID,
		Email:             u.Email,
		Role:              u.Role.Name,
		Permissions:       permissions,
		IsGlobalAdmin:     u.IsGlobalAdmin,
		Name:              name,
		SSOProvider:       sso,
		PreferredLanguage: u.PreferredLanguage,
		HideHelpTooltips:  u.HideHelpTooltips,
		TokenVersion:      u.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
		},
	}
}

// ssoPageIDKey: context key for Claims.SSOPageID.
const ssoPageIDKey = "ssoPageID"

// SSOPageID returns the page to which a page SSO session is restricted,
// otherwise 0.
func SSOPageID(c *gin.Context) uint {
	return c.GetUint(ssoPageIDKey)
}

// EffectiveGlobalAdmin: global admin rights of the request. A page SSO
// session never has them, even if the user has since become Global Admin
// .
func EffectiveGlobalAdmin(c *gin.Context, u models.User) bool {
	return u.IsGlobalAdmin && SSOPageID(c) == 0
}

// SSOAllowsPage: false if the request comes from a page SSO session of
// another page.
func SSOAllowsPage(c *gin.Context, pageID uint) bool {
	scope := SSOPageID(c)
	return scope == 0 || scope == pageID
}

// setUserContext stores the claims of a valid login token in the context
// (shared by AuthMiddleware and OptionalAuthMiddleware).
func setUserContext(c *gin.Context, claims *Claims) {
	c.Set("userID", claims.UserID)
	c.Set("email", claims.Email)
	c.Set("role", claims.Role)
	c.Set("permissions", claims.Permissions)
	c.Set(ssoPageIDKey, claims.SSOPageID)
	c.Set(customDomainKey, claims.CustomDomain)
}

// customDomainKey: context key for Claims.CustomDomain.
const customDomainKey = "customDomain"

// CustomDomainSession reports whether the request's session was handed to a
// custom domain.
func CustomDomainSession(c *gin.Context) bool {
	return c.GetString(customDomainKey) != ""
}

// RejectCustomDomainSession answers 403 custom_domain_session for a session
// handed to a custom domain. It guards the routes that act on the account
// itself (delete, export, link and unlink sign-ins): the tenant who controls
// the domain's DNS can read such a session. The same account signs in on the
// main address for these.
func RejectCustomDomainSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		if CustomDomainSession(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "Auf einer eigenen Domain nicht möglich. Melde dich dafür unter der Hauptadresse an.",
				"code":  "custom_domain_session",
			})
			return
		}
		c.Next()
	}
}

// subscriberAudience separates subscriber tokens from login tokens:
// both share JWT_SECRET, but a subscriber token must never pass as a
// session (in addition to the audience check, the login path
// requires an exp of at most two hours).
const subscriberAudience = "subscriber"

// subscriberTokenTTL: validity of a subscriber token. 400 days is a good
// year plus buffer: every notification brings a fresh link, and
// the unsubscribe link of a rarely used page lasts longer than a
// yearly cycle. A forwarded or leaked copy does not remain
// usable forever.
const subscriberTokenTTL = 400 * 24 * time.Hour

// subscriberTokenNow is the clock of the subscriber token check (tests set it).
var subscriberTokenNow = time.Now

// SetSubscriberTokenClockForTests replaces the clock and passes the
// reset on via cleanup (analogous to the other *ForTests helpers).
func SetSubscriberTokenClockForTests(cleanup func(func()), now func() time.Time) {
	prev := subscriberTokenNow
	subscriberTokenNow = now
	cleanup(func() { subscriberTokenNow = prev })
}

// SubscriberClaims: long-lived, non-single-use token for two
// self-service actions without login - unsubscribing and (only for private
// pages) viewing the status page. Deliberately its own claims type instead of
// Claims: a subscriber has no user account (no role/permissions/
// IsGlobalAdmin), and unlike the login token the token must remain usable for many
// weeks from a notification email (subscriberTokenTTL),
// not just for a single session.
type SubscriberClaims struct {
	SubscriberID uint `json:"subscriber_id"`
	StatusPageID uint `json:"status_page_id"`
	jwt.RegisteredClaims
}

// GenerateSubscriberToken signs a SubscriberClaims token with the same
// secret as the user login tokens. It expires after subscriberTokenTTL (exp);
// iat records the issuance time. Revocation still happens
// implicitly by deleting the subscriber record (see
// UnsubscribeHandler/subscriberFromToken in handlers/subscriber_token.go).
func GenerateSubscriberToken(subscriberID, statusPageID uint) (string, error) {
	now := subscriberTokenNow()
	claims := &SubscriberClaims{
		SubscriberID: subscriberID,
		StatusPageID: statusPageID,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{subscriberAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(subscriberTokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(getJWTSecret())
}

// ParseSubscriberToken validates signature and algorithm (jwt.WithValidMethods pins
// HS256, instead of accepting whatever algorithm the
// token itself claims) and expiry of a SubscriberClaims token. exp and
// the subscriber audience are required; a token missing either is never valid.
func ParseSubscriberToken(tokenStr string) (*SubscriberClaims, error) {
	claims := &SubscriberClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return getJWTSecret(), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithTimeFunc(subscriberTokenNow))
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid or expired subscriber token")
	}
	// The audience is required. The ID check additionally rejects login tokens
	// that carry no subscriber_id/status_page_id and would otherwise
	// pass with zero values.
	if !slices.Contains(claims.Audience, subscriberAudience) {
		return nil, fmt.Errorf("invalid subscriber token audience")
	}
	if claims.SubscriberID == 0 || claims.StatusPageID == 0 {
		return nil, fmt.Errorf("invalid subscriber token claims")
	}
	return claims, nil
}

// parseLoginToken validates a login token for AuthMiddleware and
// OptionalAuthMiddleware. exp is required, and tokens with the subscriber audience
// are explicitly rejected (they also carry an exp, but a long one).
func parseLoginToken(tokenStr string) (*Claims, bool) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return getJWTSecret(), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || slices.Contains(claims.Audience, subscriberAudience) {
		return nil, false
	}
	return claims, true
}

// AuthTokenHeader is the header in which the frontend sends its login token.
// Caddy puts HTTP Basic Auth in front of all pages; Basic Auth
// occupies the Authorization header. If the app put its own
// "Bearer <JWT>" there, the browser would no longer send the Basic credentials
// and Caddy would answer 401. Hence a dedicated header, without a "Bearer" prefix.
const AuthTokenHeader = "X-Auth-Token"

// loginTokenFromRequest reads the login token of a request:
// first from X-Auth-Token, otherwise – for API clients and existing users – from
// "Authorization: Bearer <token>". A set X-Auth-Token takes precedence,
// even if invalid: a fallback to Authorization would otherwise allow
// different identities depending on the header combination.
// If no token is present (also for "Authorization: Basic …", which Caddy
// passes through), errMsg is set; AuthMiddleware returns it as 401,
// OptionalAuthMiddleware ignores it and lets the request through anonymously.
func loginTokenFromRequest(c *gin.Context) (token string, errMsg string) {
	if t := strings.TrimSpace(c.GetHeader(AuthTokenHeader)); t != "" {
		return t, ""
	}
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return "", "Authentication token required"
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", "Authorization header format must be Bearer <token>"
	}
	return parts[1], ""
}

// sessionState is the result of comparing a login token with the account.
type sessionState int

const (
	sessionValid   sessionState = iota
	sessionRevoked              // account missing/deleted or token version outdated
	sessionError                // database error: confirm nothing (fail closed)
)

// checkSession compares a formally valid login token with the account:
// the account must exist (soft delete counts as deleted) and the version
// recorded in the token must match the state in users.token_version.
// Without this check a token would live on after a password change or deletion
// until expiry, even on routes without their own database check.
func checkSession(claims *Claims) sessionState {
	var user models.User
	err := database.DB.Select("id", "token_version").First(&user, claims.UserID).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return sessionRevoked
	case err != nil:
		slog.Error("Sitzungsprüfung fehlgeschlagen", "user_id", claims.UserID)
		return sessionError
	case user.TokenVersion != claims.TokenVersion:
		return sessionRevoked
	}
	return sessionValid
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr, errMsg := loginTokenFromRequest(c)
		if errMsg != "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": errMsg, "code": "unauthorized"})
			c.Abort()
			return
		}

		// jwt.WithValidMethods (in parseLoginToken) pins the expected
		// signing algorithm: without this option the
		// library accepts any algorithm the token itself states in its header
		// (incl. e.g. "none").
		claims, ok := parseLoginToken(tokenStr)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token", "code": "unauthorized"})
			c.Abort()
			return
		}

		switch checkSession(claims) {
		case sessionRevoked:
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token", "code": "unauthorized"})
			c.Abort()
			return
		case sessionError:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not verify session", "code": "server_error"})
			c.Abort()
			return
		}

		setUserContext(c, claims)

		c.Next()
	}
}

func OptionalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if tokenStr, errMsg := loginTokenFromRequest(c); errMsg == "" {
			if claims, ok := parseLoginToken(tokenStr); ok && checkSession(claims) == sessionValid {
				setUserContext(c, claims)
			}
		}

		c.Next()
	}
}

// IsGlobalTag identifies the reserved global tag from the additive
// migration: name "global" case-insensitive AND StatusPageID ==
// nil. Both conditions together are required so that a page-scoped tag
// that also happens to be called "global" (explicitly allowed by the partial
// unique indexes) is not wrongly treated as a wildcard.
// Exported because handlers/tag_access.go needs the same check for the
// list-filter scope logic.
func IsGlobalTag(t models.Tag) bool {
	return t.StatusPageID == nil && strings.EqualFold(t.Name, "global")
}

// UserHasAccessToAnyTag checks whether a user with the given tags has access to
// a resource with the given tags (multi-tag successor of
// UserHasAccessToCountry, additive -). Access exists as soon as
// at least one TagID from userTags matches a TagID from resourceTags
// (direct equivalent of the old "one shared country
// is enough" logic, not "all tags must match").
//
// Wildcard semantics: the reserved global tag acts as a wildcard on BOTH sides,
// exactly matching the old code. There, both
// `targetCountry == "global"` (resource side) and a
// `cTrimmed == "global"` entry in the loop over the user side
// yielded true independently of each other. The two paths look different
// in code (early return vs. loop hit), but the result is identical in both
// cases: access granted. The wildcard is thus effectively
// symmetric, not asymmetric - global on the resource grants any
// user access, and global on the user grants access to any resource.
//
// Fail-closed on empty lists: an empty userTags or resourceTags list
// returns false instead of being implicitly treated as a wildcard. After the
// additive migration from step 1/4 every resource and every
// user should carry at least one tag (the global one if in doubt) - an empty
// list therefore more likely indicates a data/loading error than a deliberate
// "no restriction", and for security-critical access code the
// safe default is to deny access when in doubt.
func UserHasAccessToAnyTag(userTags, resourceTags []models.Tag) bool {
	if len(userTags) == 0 || len(resourceTags) == 0 {
		return false
	}

	for _, rt := range resourceTags {
		if IsGlobalTag(rt) {
			return true
		}
	}
	for _, ut := range userTags {
		if IsGlobalTag(ut) {
			return true
		}
	}

	for _, ut := range userTags {
		for _, rt := range resourceTags {
			if ut.ID == rt.ID {
				return true
			}
		}
	}
	return false
}

// roleHasAnyPermission is true if the role has at least one of the permissions.
// create, update and delete include read
// of the same resource, the legacy value "<resource>:write" counts as
// create+update+delete+read (permissions.Has); the frontend (useAuth.hasPerm)
// mirrors the same rules.
func roleHasAnyPermission(list string, required []string) bool {
	return permissions.HasAny(list, required)
}

// requirePermissionCheck performs the actual permission check
// and returns (httpStatus, errorMessage, ok). An empty requiredPerm
// only requires any role on the page (see RequirePageRole).
func requirePermissionCheck(c *gin.Context, requiredPerms ...string) (int, string, bool) {
	userIDVal, exists := c.Get("userID")
	if !exists {
		return http.StatusUnauthorized, "Unauthorized", false
	}
	userID, ok := userIDVal.(uint)
	if !ok {
		return http.StatusInternalServerError, "Invalid user session", false
	}

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return http.StatusUnauthorized, "User not found", false
	}
	if EffectiveGlobalAdmin(c, user) {
		return 0, "", true
	}

	pageIDStr := c.Param("page_id")
	if pageIDStr == "" {
		return http.StatusBadRequest, "Access denied: Missing page_id in URL", false
	}
	pageID, err := strconv.Atoi(pageIDStr)
	if err != nil {
		return http.StatusBadRequest, "Invalid page_id format", false
	}
	// A page SSO session applies only to the page of its IdP,
	// even if the user now has roles on other pages.
	if !SSOAllowsPage(c, uint(pageID)) {
		return http.StatusForbidden, "Access denied: SSO session is limited to its own Status Page", false
	}

	var userRole models.UserStatusPageRole
	if err := database.DB.Preload("Role").Where("user_id = ? AND status_page_id = ?", userID, pageID).First(&userRole).Error; err != nil {
		return http.StatusForbidden, "Access denied: No role assigned for this Status Page", false
	}
	// A status-page-only role (no admin right) never passes an admin route,
	// not even one that accepts any role on the page.
	if !permissions.GrantsAdminArea(userRole.Role.Permissions) {
		return http.StatusForbidden, "Access denied: Role grants no admin access for this Status Page", false
	}
	if len(requiredPerms) > 0 && requiredPerms[0] != "" && !roleHasAnyPermission(userRole.Role.Permissions, requiredPerms) {
		return http.StatusForbidden, "Access denied: Missing required permission: " + strings.Join(requiredPerms, " or "), false
	}
	return 0, "", true
}

// RequirePageRole requires any role on the status page from
// :page_id (or Global Admin), without a specific permission.
// The role must grant admin access (permissions.GrantsAdminArea): a
// status-page-only role reads no admin configuration.
// For read access to page-internal configuration that every
// admin area of the page needs (languages, UI overrides) - previously they were
// readable by every logged-in user on every page.
func RequirePageRole() gin.HandlerFunc {
	return RequirePermission("")
}

// RequirePermission checks if the user has the required permission either globally or for the specific Status Page
func RequirePermission(requiredPerm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, msg, ok := requirePermissionCheck(c, requiredPerm)
		if !ok {
			c.JSON(status, gin.H{"error": msg, "code": codeForStatus(status)})
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireAnyPermission requires at least one of the permissions:
// e.g. a page's tag list for component AND incident forms, which a
// user with only incidents:write would otherwise not be allowed to load.
func RequireAnyPermission(requiredPerms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, msg, ok := requirePermissionCheck(c, requiredPerms...)
		if !ok {
			c.JSON(status, gin.H{"error": msg, "code": codeForStatus(status)})
			c.Abort()
			return
		}
		c.Next()
	}
}

// codeForStatus returns a coarse error code by HTTP status for responses
// whose text is produced in requirePermissionCheck; the frontend
// translates it under api.errors.<code>.
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_id"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	}
	return "server_error"
}
