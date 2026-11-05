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
	// HideHelpTooltips: SSO-Logins setzen den User direkt aus
	// dem Token, ohne /api/me zu laden. Ohne den Claim wären die Tooltips
	// dort nach jedem SSO-Login wieder an.
	HideHelpTooltips bool `json:"hide_help_tooltips,omitempty"`
	// SSOPageID: Seite, deren IdP die Sitzung ausgestellt hat
	// (Seiten-SSO per OIDC oder SAML). Die Sitzung gilt nur für diese Seite
	// und nie mit Global-Admin-Rechten. Fehlt bei Passwort- und
	// globalen SSO-Logins (Google/GitHub).
	SSOPageID uint `json:"sso_page_id,omitempty"`
	// TokenVersion: Stand von users.token_version bei der Ausstellung. Weicht
	// er vom Konto ab, wurde das Token widerrufen (siehe sessionStillValid).
	TokenVersion uint `json:"tv"`
	jwt.RegisteredClaims
}

// tokenTTL ist die Access-Token-Laufzeit für alle Login-Wege (Passwort + SSO).
const tokenTTL = 2 * time.Hour

// AllowedCountries wurde aus dem Claim entfernt (Country → Tags
// Schritt 4/4). Tag-basierte Zugriffsprüfungen lesen die Tags eines Users
// jetzt frisch aus der DB (siehe handlers/tag_access.go, userTagsAndAdmin) statt
// aus einem JWT-Claim - so wirkt eine Tag-Änderung sofort, ohne dass der User
// sich neu einloggen muss.
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

// TokenOption passt die Claims eines Login-Tokens bei der Ausstellung an.
type TokenOption func(*Claims)

// WithTokenVersion trägt die aktuelle users.token_version des Kontos ein.
// Ohne die Option gilt Version 0 (Konto, dessen Version nie erhöht wurde).
func WithTokenVersion(v uint) TokenOption {
	return func(c *Claims) { c.TokenVersion = v }
}

// GenerateTokenFull ist die zentrale Token-Factory für SSO-Logins, inkl. Name und
// SSO-Provider als Zusatz-Claims (ersetzt die frühere Inline-Signierung in auth_sso.go).
func GenerateTokenFull(u models.User, permissions, name, sso string) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, fullClaims(u, permissions, name, sso)).SignedString(getJWTSecret())
}

// GeneratePageSSOToken stellt das Token nach einem Seiten-SSO-
// Login aus: wie GenerateTokenFull, zusätzlich mit sso_page_id. Ein Seiten-
// Admin kontrolliert den IdP seiner Seite und kann damit Assertions bzw.
// ID-Tokens für jeden dort verknüpften Nutzer fälschen. Die Rechte werden je
// Request frisch aus der DB gelesen; ohne Seitenbezug bekäme das Token sofort
// jede Rolle, die der Nutzer später anderswo erhält (auch Global Admin).
func GeneratePageSSOToken(u models.User, name, protocol string, pageID uint) (string, error) {
	claims := fullClaims(u, u.Role.Permissions, name, protocol)
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

// ssoPageIDKey: Kontext-Schlüssel für Claims.SSOPageID.
const ssoPageIDKey = "ssoPageID"

// SSOPageID liefert die Seite, auf die eine Seiten-SSO-Sitzung beschränkt
// ist, sonst 0.
func SSOPageID(c *gin.Context) uint {
	return c.GetUint(ssoPageIDKey)
}

// EffectiveGlobalAdmin: Global-Admin-Rechte der Anfrage. Eine Seiten-SSO-
// Sitzung hat sie nie, auch wenn der Nutzer inzwischen Global Admin ist
// .
func EffectiveGlobalAdmin(c *gin.Context, u models.User) bool {
	return u.IsGlobalAdmin && SSOPageID(c) == 0
}

// SSOAllowsPage: false, wenn die Anfrage aus einer Seiten-SSO-Sitzung einer
// anderen Seite stammt.
func SSOAllowsPage(c *gin.Context, pageID uint) bool {
	scope := SSOPageID(c)
	return scope == 0 || scope == pageID
}

// setUserContext legt die Claims eines gültigen Login-Tokens im Kontext ab
// (gemeinsam für AuthMiddleware und OptionalAuthMiddleware).
func setUserContext(c *gin.Context, claims *Claims) {
	c.Set("userID", claims.UserID)
	c.Set("email", claims.Email)
	c.Set("role", claims.Role)
	c.Set("permissions", claims.Permissions)
	c.Set(ssoPageIDKey, claims.SSOPageID)
}

// subscriberAudience trennt Subscriber-Tokens von Login-Tokens:
// beide teilen sich das JWT_SECRET, ein Subscriber-Token darf aber nie als
// Sitzung durchgehen (zusätzlich zur Prüfung der Audience verlangt der
// Login-Pfad ein exp von höchstens zwei Stunden).
const subscriberAudience = "subscriber"

// subscriberTokenTTL: Gültigkeit eines Subscriber-Tokens. 400 Tage sind gut
// ein Jahr plus Puffer: Jede Benachrichtigung bringt einen frischen Link, und
// der Abmelde-Link einer selten genutzten Seite hält sich länger als ein
// Jahreszyklus. Ein weitergegebenes oder geleaktes Exemplar bleibt nicht ewig
// brauchbar.
const subscriberTokenTTL = 400 * 24 * time.Hour

// subscriberTokenNow ist die Uhr der Subscriber-Token-Prüfung (Tests setzen sie).
var subscriberTokenNow = time.Now

// SetSubscriberTokenClockForTests ersetzt die Uhr und gibt über cleanup die
// Rücksetzung weiter (analog zu den übrigen *ForTests-Helfern).
func SetSubscriberTokenClockForTests(cleanup func(func()), now func() time.Time) {
	prev := subscriberTokenNow
	subscriberTokenNow = now
	cleanup(func() { subscriberTokenNow = prev })
}

// SubscriberClaims: langlebiger, nicht-einmaliger Token für zwei
// Self-Service-Aktionen ohne Login - Abo abbestellen und (nur bei privaten
// Seiten) die Statusseite ansehen. Bewusst ein eigener Claims-Typ statt
// Claims: ein Subscriber hat kein User-Konto (keine Role/Permissions/
// IsGlobalAdmin), und der Token muss anders als der Login-Token über viele
// Wochen aus einer Benachrichtigungsmail nutzbar bleiben (subscriberTokenTTL),
// nicht nur für eine einzelne Session gelten.
type SubscriberClaims struct {
	SubscriberID uint `json:"subscriber_id"`
	StatusPageID uint `json:"status_page_id"`
	jwt.RegisteredClaims
}

// GenerateSubscriberToken signiert einen SubscriberClaims-Token mit demselben
// Secret wie die User-Login-Tokens. Er läuft nach subscriberTokenTTL ab (exp);
// iat hält den Ausstellungszeitpunkt fest. Widerruf erfolgt weiterhin
// implizit durch Löschen des Subscriber-Datensatzes (siehe
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

// ParseSubscriberToken validiert Signatur und Algorithmus (jwt.WithValidMethods pinnt
// HS256, statt jeden vom Token selbst behaupteten
// Algorithmus zu akzeptieren) und Ablauf eines SubscriberClaims-Tokens. exp und
// die Subscriber-Audience sind Pflicht; ein Token ohne eines von beiden gilt nie.
func ParseSubscriberToken(tokenStr string) (*SubscriberClaims, error) {
	claims := &SubscriberClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return getJWTSecret(), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithTimeFunc(subscriberTokenNow))
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid or expired subscriber token")
	}
	// Die Audience ist Pflicht. Die ID-Prüfung weist zusätzlich Login-Tokens ab,
	// die kein subscriber_id/status_page_id tragen und sonst mit Nullwerten
	// durchgingen.
	if !slices.Contains(claims.Audience, subscriberAudience) {
		return nil, fmt.Errorf("invalid subscriber token audience")
	}
	if claims.SubscriberID == 0 || claims.StatusPageID == 0 {
		return nil, fmt.Errorf("invalid subscriber token claims")
	}
	return claims, nil
}

// parseLoginToken validiert ein Login-Token für AuthMiddleware und
// OptionalAuthMiddleware. exp ist Pflicht, und Tokens mit Subscriber-Audience
// werden ausdrücklich abgewiesen (auch sie tragen ein exp, aber ein langes).
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

// AuthTokenHeader ist der Header, in dem das Frontend sein Login-Token
// schickt. Caddy legt HTTP Basic Auth vor alle Seiten; Basic Auth
// belegt den Authorization-Header. Setzte die App dort ihr eigenes
// "Bearer <JWT>", schickte der Browser die Basic-Zugangsdaten nicht mehr mit
// und Caddy antwortete 401. Deshalb ein eigener Header, ohne "Bearer"-Präfix.
const AuthTokenHeader = "X-Auth-Token"

// loginTokenFromRequest liest das Login-Token einer Anfrage:
// zuerst aus X-Auth-Token, sonst – für API-Clients und Bestandsnutzer – aus
// "Authorization: Bearer <token>". Ein gesetzter X-Auth-Token hat Vorrang,
// auch wenn er ungültig ist: Ein Fallback auf Authorization würde sonst je
// nach Header-Kombination unterschiedliche Identitäten zulassen.
// Ist kein Token vorhanden (auch bei "Authorization: Basic …", das Caddy
// durchreicht), ist errMsg gesetzt; AuthMiddleware gibt ihn als 401 zurück,
// OptionalAuthMiddleware ignoriert ihn und lässt die Anfrage anonym durch.
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

// sessionState ist das Ergebnis des Abgleichs eines Login-Tokens mit dem Konto.
type sessionState int

const (
	sessionValid   sessionState = iota
	sessionRevoked              // Konto fehlt/gelöscht oder Token-Version veraltet
	sessionError                // Datenbankfehler: nichts bestätigen (fail closed)
)

// checkSession gleicht ein formal gültiges Login-Token mit dem Konto ab:
// Das Konto muss existieren (Soft Delete zählt als gelöscht) und die im Token
// eingetragene Version muss dem Stand in users.token_version entsprechen.
// Ohne diesen Abgleich lebte ein Token nach Passwortwechsel oder Löschung
// bis zum Ablauf weiter, auch auf Routen ohne eigene Datenbankprüfung.
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

		// jwt.WithValidMethods (in parseLoginToken) pinnt den erwarteten
		// Signaturalgorithmus: ohne diese Option akzeptiert die
		// Bibliothek jeden Algorithmus, den der Token selbst im Header angibt
		// (inkl. z.B. "none").
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

// IsGlobalTag identifiziert den reservierten globalen Tag aus der additiven
// Migration: Name "global" case-insensitive UND StatusPageID ==
// nil. Beide Bedingungen zusammen sind nötig, damit ein seitenbezogener Tag,
// der zufällig ebenfalls "global" heißt (von den partiellen Unique-Indizes
// ausdrücklich erlaubt), nicht fälschlich als Wildcard behandelt wird.
// Exportiert, da handlers/tag_access.go dieselbe Prüfung für die
// Listen-Filter-Scope-Logik braucht.
func IsGlobalTag(t models.Tag) bool {
	return t.StatusPageID == nil && strings.EqualFold(t.Name, "global")
}

// UserHasAccessToAnyTag prüft, ob ein User mit den gegebenen Tags Zugriff auf
// eine Ressource mit den gegebenen Tags hat (Mehrfach-Tag-Nachfolger von
// UserHasAccessToCountry, additiv -). Zugriff besteht, sobald
// mindestens eine TagID aus userTags mit einer TagID aus resourceTags
// übereinstimmt (direkte Entsprechung der alten "ein gemeinsames Land
// reicht"-Logik, nicht "alle Tags müssen übereinstimmen").
//
// Wildcard-Semantik: Der reservierte globale Tag wirkt auf BEIDEN Seiten als
// Wildcard, exakt entsprechend dem alten Code. Dort lieferte sowohl
// `targetCountry == "global"` (Ressourcen-Seite) als auch ein
// `cTrimmed == "global"`-Eintrag in der Schleife über die User-Seite
// unabhängig voneinander true. Die beiden Pfade sehen im Code unterschiedlich
// aus (früher Return vs. Schleifentreffer), aber das Ergebnis ist in beiden
// Fällen identisch: Zugriff gewährt. Die Wildcard ist also im Effekt
// symmetrisch, nicht asymmetrisch - global auf der Ressource gewährt jedem
// User Zugriff, und global beim User gewährt Zugriff auf jede Ressource.
//
// Fail-closed bei leeren Listen: Eine leere userTags- oder resourceTags-Liste
// liefert false statt implizit als Wildcard behandelt zu werden. Nach der
// additiven Migration aus Schritt 1/4 sollte jede Ressource und jeder
// User mindestens einen Tag tragen (im Zweifel den globalen) - eine leere
// Liste deutet daher eher auf einen Daten-/Ladefehler hin als auf bewusst
// "keine Einschränkung", und bei sicherheitskritischem Zugriffscode ist die
// sichere Grundeinstellung, im Zweifel den Zugriff zu verweigern.
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

// roleHasAnyPermission ist wahr, wenn die Rolle mindestens eine der Permissions
// hat. create, update und delete schließen read
// derselben Ressource ein, der Altwert "<ressource>:write" zählt als
// create+update+delete+read (permissions.Has); das Frontend (useAuth.hasPerm)
// spiegelt dieselben Regeln.
func roleHasAnyPermission(list string, required []string) bool {
	return permissions.HasAny(list, required)
}

// requirePermissionCheck führt die eigentliche Berechtigungsprüfung durch
// und gibt (httpStatus, errorMessage, ok) zurück. Ein leeres requiredPerm
// verlangt nur irgendeine Rolle auf der Seite (siehe RequirePageRole).
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
	// Eine Seiten-SSO-Sitzung gilt nur für die Seite ihres IdP,
	// auch wenn der Nutzer inzwischen auf anderen Seiten Rollen hat.
	if !SSOAllowsPage(c, uint(pageID)) {
		return http.StatusForbidden, "Access denied: SSO session is limited to its own Status Page", false
	}

	var userRole models.UserStatusPageRole
	if err := database.DB.Preload("Role").Where("user_id = ? AND status_page_id = ?", userID, pageID).First(&userRole).Error; err != nil {
		return http.StatusForbidden, "Access denied: No role assigned for this Status Page", false
	}
	if len(requiredPerms) > 0 && requiredPerms[0] != "" && !roleHasAnyPermission(userRole.Role.Permissions, requiredPerms) {
		return http.StatusForbidden, "Access denied: Missing required permission: " + strings.Join(requiredPerms, " or "), false
	}
	return 0, "", true
}

// RequirePageRole verlangt eine beliebige Rolle auf der Statusseite aus
// :page_id (oder Global Admin), ohne bestimmte Permission.
// Für Lesezugriffe auf seiten-interne Konfiguration, die jeder
// Admin-Bereich der Seite braucht (Sprachen, UI-Overrides) - vorher waren sie
// für jeden eingeloggten Nutzer auf jeder Seite lesbar.
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

// RequireAnyPermission verlangt mindestens eine der Permissions:
// z. B. die Tag-Liste einer Seite für Komponenten- UND Vorfallformulare, die ein
// Nutzer mit nur incidents:write sonst nicht laden dürfte.
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

// codeForStatus liefert einen groben Fehlercode nach HTTP-Status für Antworten,
// deren Text in requirePermissionCheck entsteht; das Frontend
// übersetzt ihn unter api.errors.<code>.
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
