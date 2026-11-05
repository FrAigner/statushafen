package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
)

func randomToken(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// ssoCodeStore hält kurzlebige Einmal-Codes, gegen die das Frontend das fertige JWT
// eintauscht (S5 — Token wandert nicht mehr per Redirect-Query durch Browser-History/
// Referrer/Logs). Bewusst simpel gehalten (in-memory, Prozess-lokal), analog zum
// Visitor-Store in middleware.NewRateLimiter.
type ssoCodeEntry struct {
	token   string
	expires time.Time
}

var (
	ssoCodeMu    sync.Mutex
	ssoCodeStore = map[string]ssoCodeEntry{}
)

func storeSSOCode(token string) string {
	code := randomToken(32)
	ssoCodeMu.Lock()
	ssoCodeStore[code] = ssoCodeEntry{token: token, expires: time.Now().Add(ssoCodeExpiry())}
	ssoCodeMu.Unlock()
	return code
}

func consumeSSOCode(code string) (string, bool) {
	ssoCodeMu.Lock()
	defer ssoCodeMu.Unlock()
	entry, ok := ssoCodeStore[code]
	delete(ssoCodeStore, code) // Einmalgebrauch, auch wenn abgelaufen
	if !ok || time.Now().After(entry.expires) {
		return "", false
	}
	return entry.token, true
}

// StartSSOCleanupWorker startet den periodischen ssoCodeStore-Aufräum-Loop
// im Hintergrund und liefert die zugehörige CancelFunc zurück,
// die der Aufrufer per defer beim Application-Shutdown auslösen muss -
// vorher lief die Goroutine unkontrolliert aus init() los: sie startete
// auch in jedem Testprozess, der dieses Package importiert
// (Goroutine-Leak/Race-Risiko bei -race), und konnte nie
// sauber beendet werden. Erzeugt den Context selbst (statt ihn entgegen-
// zunehmen), damit main() nur eine einzige Zeile braucht
// (defer StartSSOCleanupWorker(...)()) - main() selbst lässt sich nicht
// sinnvoll unit-testen und würde sonst die SonarCloud-New-Code-Coverage
// unnötig belasten.
func StartSSOCleanupWorker(parent context.Context) context.CancelFunc {
	ctx, cancel := context.WithCancel(parent)
	go cleanupSSOCodeStore(ctx)
	return cancel
}

// cleanupSSOCodeStore entfernt abgelaufene, nie eingelöste SSO-Codes
// regelmäßig, analog zu rateLimiter.cleanupStaleVisitors
// (middleware/security.go). Ohne das wächst ssoCodeStore unbegrenzt, wenn
// ein Nutzer den Browser vor dem finalen Redirect schließt und
// consumeSSOCode dadurch nie aufgerufen wird. Beendet sich, sobald ctx
// gecancelt wird (Graceful Shutdown).
func cleanupSSOCodeStore(ctx context.Context) {
	ticker := time.NewTicker(ssoCleanupInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepExpiredSSOCodes()
		}
	}
}

// sweepExpiredSSOCodes führt einen einzelnen Aufräumdurchlauf aus - aus der
// Sleep-Schleife ausgelagert, damit Tests ihn ohne Wartezeit direkt
// aufrufen können.
func sweepExpiredSSOCodes() {
	ssoCodeMu.Lock()
	defer ssoCodeMu.Unlock()
	now := time.Now()
	for code, entry := range ssoCodeStore {
		if now.After(entry.expires) {
			delete(ssoCodeStore, code)
		}
	}
}

func getBaseURL() string {
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return baseURL
}

func getFrontendURL() string {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	return frontendURL
}

// isBootstrapAdminEmail prüft, ob die E-Mail in SSO_BOOTSTRAP_ADMIN_EMAILS
// (kommagetrennt, case-insensitive) hinterlegt ist.
func isBootstrapAdminEmail(email string) bool {
	list := os.Getenv("SSO_BOOTSTRAP_ADMIN_EMAILS")
	if list == "" {
		return false
	}
	for _, e := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(e), email) {
			return true
		}
	}
	return false
}

// ---- OAuth2 Konfigurationen ----
func googleOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  getBaseURL() + "/api/auth/google/callback",
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

func githubOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		RedirectURL:  getBaseURL() + "/api/auth/github/callback",
		Scopes:       []string{"user:email"},
		Endpoint:     github.Endpoint,
	}
}

// OAuthBeginHandler leitet den User zum Provider weiter
// GET /api/auth/:provider
func (h *Handler) OAuthBeginHandler(c *gin.Context) {
	provider := c.Param("provider")
	var config *oauth2.Config

	switch provider {
	case "google":
		config = googleOAuthConfig()
	case "github":
		config = githubOAuthConfig()
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unbekannter Provider: " + provider, "code": "provider_unknown"})
		return
	}

	// Ohne ClientID (Env-Var nicht gesetzt/leer) baut AuthCodeURL trotzdem eine
	// URL mit client_id= - der Redirect landet dann auf einer verwirrenden
	// 404-Seite des Providers statt einer erklärenden Fehlermeldung (Vorfall
	// 2026-07-28: GITHUB_CLIENT_ID/GOOGLE_CLIENT_ID in .env.prod vergessen).
	if config.ClientID == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": provider + "-SSO ist nicht konfiguriert", "code": "sso_not_configured"})
		return
	}

	// Zufälliger State gegen Login-CSRF und PKCE-Verifier (S256) in einem
	// signierten, kurzlebigen HttpOnly-Cookie (siehe oauth_state.go). Der Callback
	// prüft Signatur, Anbieter, Alter und State, bevor er den Anbieter ruft.
	state := randomToken(32)
	verifier := oauth2.GenerateVerifier()
	c.SetSameSite(http.SameSiteLaxMode) // der Anbieter leitet per Navigation zurück
	c.SetCookie(oauthStateCookie, signOAuthState(provider, state, verifier, h.oauthNow()),
		int(oauthStateLifetime.Seconds()), "/", "", true, true)
	url := config.AuthCodeURL(state, oauth2.AccessTypeOnline, oauth2.S256ChallengeOption(verifier))
	c.Redirect(http.StatusTemporaryRedirect, url)
}

// oauthTimeout begrenzt jeden Aufruf beim Anbieter (Token-Tausch, Benutzerdaten).
const oauthTimeout = 15 * time.Second

var defaultOAuthClient = &http.Client{Timeout: oauthTimeout}

// oauthContext trägt den HTTP-Client für die Aufrufe beim Anbieter. Ohne
// Angabe nutzt oauth2 den Standardclient von net/http, der keine Zeitgrenze
// hat; ein hängender Anbieter hielte dann die Anfrage beliebig lange offen.
func (h *Handler) oauthContext(c *gin.Context) context.Context {
	client := h.OAuthClient
	if client == nil {
		client = defaultOAuthClient
	}
	return context.WithValue(c.Request.Context(), oauth2.HTTPClient, client)
}

// oauthConfigForProvider liefert die OAuth2-Konfiguration für den benannten
// Provider oder nil, wenn er unbekannt ist.
func oauthConfigForProvider(provider string) *oauth2.Config {
	switch provider {
	case "google":
		return googleOAuthConfig()
	case "github":
		return githubOAuthConfig()
	default:
		return nil
	}
}

// validateOAuthState prüft den State der Rückkehr gegen das signierte Cookie und
// liefert den PKCE-Verifier. Das Cookie wird in jedem Fall gelöscht. Die Prüfung
// läuft vor jedem Aufruf beim Anbieter: Cookie und State müssen vom Server
// stammen (Signatur, Anbieter, höchstens oauthStateLifetime alt), der State der
// Anfrage muss dem im Cookie entsprechen, und jeder State gilt nur einmal.
func (h *Handler) validateOAuthState(c *gin.Context, provider string) (verifier string, ok bool) {
	cookie, _ := c.Cookie(oauthStateCookie)
	stateParam := c.Query("state")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthStateCookie, "", -1, "/", "", true, true)
	now := h.oauthNow()
	state, verifier, valid := verifyOAuthState(provider, cookie, now)
	if !valid || subtle.ConstantTimeCompare([]byte(state), []byte(stateParam)) != 1 {
		return "", false
	}
	if !h.oauthStates.consume(state, now) {
		return "", false
	}
	return verifier, true
}

// isEmailDomainAllowed prüft die Domain einer E-Mail gegen SSO_ALLOWED_DOMAINS
// (S8). Leere Env-Var bedeutet "keine Einschränkung".
func isEmailDomainAllowed(email string) bool {
	allowed := os.Getenv("SSO_ALLOWED_DOMAINS")
	if allowed == "" {
		return true
	}
	domain := ""
	if at := strings.LastIndex(email, "@"); at != -1 {
		domain = strings.ToLower(email[at+1:])
	}
	for _, d := range strings.Split(allowed, ",") {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

// provisionSSOUser legt einen neuen User für einen erstmaligen SSO-Login an.
// Kein automatischer globaler Tag (ersetzt das frühere
// AllowedCountries: "global") - fail-closed wie bei Components/Incidents
// : ein Admin muss Tags nach dem ersten Login bewusst zuweisen, außer
// der User ist Bootstrap-Admin (umgeht die Tag-Prüfung ohnehin komplett über
// IsGlobalAdmin).
func (h *Handler) provisionSSOUser(email, provider, ssoSubject string, isBootstrapAdmin bool) (models.User, error) {
	roleName := "Viewer"
	if isBootstrapAdmin {
		roleName = "Global Admin"
	}
	var role models.Role
	h.DB.Where("name = ?", roleName).First(&role)

	user := models.User{
		Email:         email,
		PasswordHash:  "", // Kein Passwort bei SSO
		RoleID:        role.ID,
		SSOProvider:   provider,
		SSOSubject:    ssoSubject,
		IsGlobalAdmin: isBootstrapAdmin,
	}
	if err := h.DB.Create(&user).Error; err != nil {
		return user, err
	}
	h.DB.Preload("Role").First(&user, user.ID)
	return user, nil
}

// updateSSOUser aktualisiert die SSO-Felder eines bestehenden Users bei
// erneutem Login und hebt ihn ggf. auf Bootstrap-Admin an.
func (h *Handler) updateSSOUser(user models.User, provider, ssoSubject string, isBootstrapAdmin bool) models.User {
	updates := map[string]interface{}{
		"sso_provider": provider,
		"sso_subject":  ssoSubject,
	}
	if isBootstrapAdmin && !user.IsGlobalAdmin {
		updates["is_global_admin"] = true
		var globalAdminRole models.Role
		h.DB.Where("name = ?", "Global Admin").First(&globalAdminRole)
		if globalAdminRole.ID != 0 {
			updates["role_id"] = globalAdminRole.ID
		}
	}
	h.DB.Model(&user).Updates(updates)
	h.DB.Preload("Role").First(&user, user.ID)
	return user
}

// findOrCreateSSOUser sucht den User per E-Mail oder legt ihn neu an
// (inkl. Domain-Allowlist-Prüfung für neue User) und aktualisiert bei
// bestehenden Usern die SSO-Felder. Ausgelagert aus OAuthCallbackHandler,
// damit dessen kognitive Komplexität die SonarCloud-Grenze (go:S3776) nicht
// überschreitet.
func (h *Handler) findOrCreateSSOUser(email, provider, ssoSubject string) (models.User, int, string, bool) {
	var user models.User
	result := h.DB.Preload("Role").Where("email = ?", email).First(&user)

	// Fest hinterlegte Bootstrap-Admins (SSO_BOOTSTRAP_ADMIN_EMAILS, kommagetrennt)
	// erhalten bei jedem SSO-Login Global-Admin-Rechte. Löst das Henne-Ei-Problem,
	// dass es ohne SEED_DB in Produktion sonst keinen Weg zu einem ersten Admin gibt.
	isBootstrapAdmin := isBootstrapAdminEmail(email)

	if result.Error == nil {
		if !ssoLinkAllowed(user, provider, ssoSubject) {
			return user, http.StatusForbidden, "Für diese E-Mail besteht bereits ein Konto. Bitte mit dem bisherigen Zugang anmelden.", false
		}
		return h.updateSSOUser(user, provider, ssoSubject, isBootstrapAdmin), 0, "", true
	}

	// Neuer Benutzer: Domain gegen Allowlist prüfen, bevor er angelegt wird (S8).
	if !isEmailDomainAllowed(email) {
		return user, http.StatusForbidden, "E-Mail-Domain nicht für SSO freigegeben", false
	}
	user, err := h.provisionSSOUser(email, provider, ssoSubject, isBootstrapAdmin)
	if err != nil {
		return user, http.StatusInternalServerError, "Benutzer konnte nicht angelegt werden", false
	}
	return user, 0, "", true
}

// ssoLinkAllowed entscheidet, ob ein SSO-Login ein per E-Mail gefundenes,
// bestehendes Konto übernehmen darf. Ein bereits verknüpftes
// Konto nur mit genau demselben Provider und Subject. Ein noch nicht
// verknüpftes Konto nur, wenn es weder ein Passwort hat noch Global Admin ist
// (z. B. ein vom Admin vorab angelegtes SSO-Konto). Passwort- und Admin-Konten
// bleiben so vor einer Übernahme über eine fremde IdP-Identität geschützt.
func ssoLinkAllowed(user models.User, provider, ssoSubject string) bool {
	if user.SSOSubject != "" {
		return user.SSOProvider == provider && user.SSOSubject == ssoSubject
	}
	return user.PasswordHash == "" && !user.IsGlobalAdmin
}

// OAuthCallbackHandler verarbeitet den Callback vom Provider
// GET /api/auth/:provider/callback
func (h *Handler) OAuthCallbackHandler(c *gin.Context) {
	provider := c.Param("provider")
	code := c.Query("code")

	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Kein Authorization Code erhalten", "code": "auth_code_missing"})
		return
	}

	// Allowlist-Validierung: provider stammt aus c.Param und darf nur bekannte
	// Werte annehmen. Die Neuzuweisung aus einem String-Literal bricht die
	// Taint-Kette für statische Analyse (SonarCloud S3649).
	switch provider {
	case "google":
		provider = "google"
	case "github":
		provider = "github"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unbekannter Provider", "code": "provider_unknown"})
		return
	}
	config := oauthConfigForProvider(provider)

	// State vor jedem Aufruf beim Anbieter: ohne ihn löste jede anonyme Anfrage
	// mit selbst gewähltem State einen Token-Tausch aus.
	verifier, ok := h.validateOAuthState(c, provider)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid OAuth state", "code": "oauth_state_invalid"})
		return
	}

	// Token vom Provider holen (mit dem PKCE-Verifier aus dem Cookie)
	ctx := h.oauthContext(c)
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		slog.Error("SSO token exchange failed", "provider", provider, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token-Austausch fehlgeschlagen"})
		return
	}

	// Userinfo vom Provider laden
	httpClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
	email, name, ssoSubject, err := fetchUserInfo(provider, httpClient)
	if err != nil {
		slog.Error("SSO userinfo fetch failed", "provider", provider, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Benutzerdaten konnten nicht geladen werden"})
		return
	}

	user, status, msg, ok := h.findOrCreateSSOUser(email, provider, ssoSubject)
	if !ok {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	// Audit-Eintrag für den SSO-Login: RecordAudit liest Aktor-Infos
	// aus dem Gin-Context (email/userID/role), die auf dieser öffentlichen Route
	// vor dem JWT-Issuing noch nicht gesetzt sind - deshalb hier einmalig
	// nachtragen, damit der Audit-Eintrag den tatsächlich eingeloggten User
	// zeigt statt eines leeren Aktors.
	c.Set("email", user.Email)
	c.Set("userID", user.ID)
	c.Set("role", user.Role.Name)
	h.RecordAudit(c, "auth.sso_login", "auth", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email, "provider="+provider)

	// JWT über die zentrale Factory ausstellen (kein hartcodierter Secret-Fallback mehr, S3/S6).
	tokenString, err := middleware.GenerateTokenFull(user, user.Role.Permissions, name, provider)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "JWT konnte nicht erstellt werden"})
		return
	}

	// Statt das Token per Redirect-Query zu übergeben (History/Logs/Referer), einen
	// kurzlebigen Einmal-Code ausgeben, den das Frontend serverseitig eintauscht (S5).
	ssoCode := storeSSOCode(tokenString)

	c.Redirect(http.StatusTemporaryRedirect, getFrontendURL()+"/auth/sso-callback?code="+ssoCode)
}

// SSOExchangeHandler tauscht einen per SSO-Redirect ausgegebenen Einmal-Code gegen das
// eigentliche JWT ein. POST /api/auth/sso-exchange (öffentlich, rate-limitiert).
func (h *Handler) SSOExchangeHandler(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Code erforderlich", "code": "login_code_required"})
		return
	}

	token, ok := consumeSSOCode(req.Code)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Ungültiger oder abgelaufener Code", "code": "login_code_invalid"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token})
}

// fetchUserInfo holt E-Mail, Name und Subject-ID vom jeweiligen Provider
func fetchUserInfo(provider string, client *http.Client) (email, name, subject string, err error) {
	switch provider {
	case "google":
		resp, reqErr := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		if reqErr != nil {
			return "", "", "", reqErr
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", "", "", err
		}
		var info struct {
			ID            string `json:"id"`
			Email         string `json:"email"`
			Name          string `json:"name"`
			VerifiedEmail bool   `json:"verified_email"`
		}
		if err := json.Unmarshal(body, &info); err != nil {
			return "", "", "", err
		}
		// Wie beim GitHub-Pfad nur verifizierte Adressen. Ein
		// fehlendes Feld zählt als unverifiziert.
		if !info.VerifiedEmail {
			return "", "", "", fmt.Errorf("Google-E-Mail ist nicht verifiziert")
		}
		return info.Email, info.Name, info.ID, nil

	case "github":
		// Emails-Endpunkt bei GitHub
		resp, reqErr := client.Get("https://api.github.com/user/emails")
		if reqErr != nil {
			return "", "", "", reqErr
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", "", "", err
		}
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := json.Unmarshal(body, &emails); err != nil {
			return "", "", "", err
		}

		// Profil für Name und ID
		profileResp, err := client.Get("https://api.github.com/user")
		if err != nil {
			return "", "", "", err
		}
		defer func() { _ = profileResp.Body.Close() }()
		profileBody, err := io.ReadAll(profileResp.Body)
		if err != nil {
			return "", "", "", err
		}
		var profile struct {
			ID    int    `json:"id"`
			Login string `json:"login"`
			Name  string `json:"name"`
		}
		if err := json.Unmarshal(profileBody, &profile); err != nil {
			return "", "", "", err
		}

		for _, e := range emails {
			if e.Primary && e.Verified {
				return e.Email, profile.Name, fmt.Sprintf("%d", profile.ID), nil
			}
		}
		return "", "", "", fmt.Errorf("keine verifizierte primäre E-Mail bei GitHub gefunden")
	}
	return "", "", "", fmt.Errorf("unbekannter Provider")
}
