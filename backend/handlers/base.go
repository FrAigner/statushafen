package handlers

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/middleware"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	DB *gorm.DB
	// ExternalStatus: Abfrage-Dienst für externe Statusseiten,
	// von "Jetzt abrufen" genutzt und in main.go gestartet. Tests ersetzen ihn
	// durch einen Dienst mit Fake-Provider.
	ExternalStatus *externalstatus.Service

	// Now ersetzt die Uhr der Verfügbarkeitsberechnung, nil = time.Now.
	Now        func() time.Time
	availCache availabilityCache
	// lastStatusPrune: letzte Bereinigung der Statushistorie;
	// nur der Scheduler-Goroutine zugänglich.
	lastStatusPrune time.Time
	// translationsCache: 60-s-Cache der gebündelten Statusseiten-Übersetzungen.
	translationsCache publicTranslationsCache

	// dispatchWG zählt die Goroutinen von DispatchNotifications (WaitNotifications).
	dispatchWG sync.WaitGroup

	// loginThrottle zählt Fehlversuche je Konto (siehe LoginHandler).
	loginThrottle *middleware.AccountThrottle

	// HTTP-Clients der Übersetzungsdienste. nil = Produktionswert
	// (translationClient). Tests setzen sie auf Clients gegen httptest-Server.
	// SafeClient prüft Ziele gegen SSRF (frei eingebbare Adresse),
	// TrustedClient ruft die Adresse aus der Betreiber-Umgebung (LIBRETRANSLATE_URL,
	// z. B. ein Compose-Dienst) ohne IP-Sperre auf. DeepLURL überschreibt den
	// festen DeepL-Host (nur Tests).
	TranslationSafeClient    *http.Client
	TranslationTrustedClient *http.Client
	TranslationDeepLURL      string

	// OAuthClient ist der HTTP-Client für Token-Tausch und Benutzerdaten bei
	// Google und GitHub. nil = Standardclient mit Zeitgrenze (oauthHTTPClient);
	// Tests setzen einen Client mit Attrappen-Anbieter, es geht kein Verkehr ins Netz.
	OAuthClient *http.Client

	// oauthStates merkt eingelöste State-Werte des OAuth-Logins, siehe oauth_state.go.
	oauthStates oauthStateUse

	// incidentMails zählt laufende Vorfall-Benachrichtigungen, siehe WaitIncidentMails.
	incidentMails sync.WaitGroup
	// outageMails zählt laufende Ausfall-Mails, siehe WaitOutageMails.
	outageMails sync.WaitGroup
	// mailQ begrenzt die Bestätigungsmails im Hintergrund, siehe mail_queue.go.
	mailQ mailQueue
}

func NewHandler(db *gorm.DB) *Handler {
	h := &Handler{DB: db, loginThrottle: middleware.NewLoginThrottle()}
	// Der Dienst meldet Quellen-Ausfälle an den Handler, der
	// die Mails über deliverMail verschickt.
	h.ExternalStatus = externalstatus.NewService(db, externalstatus.Options{Outage: h, Incidents: h, Retention: externalstatus.RetentionFromEnv(os.Getenv)})
	return h
}

func getEmail(c *gin.Context) string {
	em, exists := c.Get("email")
	if !exists || em == nil {
		return ""
	}
	str, ok := em.(string)
	if !ok {
		return ""
	}
	return str
}

// getUserID liefert die ID des angemeldeten Users, oder 0 wenn nicht angemeldet
// (z.B. anonymes Abonnieren über OptionalAuthMiddleware).
func getUserID(c *gin.Context) uint {
	uid, exists := c.Get("userID")
	if !exists || uid == nil {
		return 0
	}
	id, ok := uid.(uint)
	if !ok {
		return 0
	}
	return id
}

// parseTenantID liest und validiert :page_id aus der URL und antwortet bei
// Fehlern selbst mit dem passenden JSON-Error (ok=false signalisiert dem
// Aufrufer, sofort zurückzukehren). Ersatz für die zuvor in
// Create-/Update-/DeleteComponentHandler (und analog bei Incidents)
// dreifach duplizierte page_id-Parsing-Boilerplate.
func parseTenantID(c *gin.Context) (uint, bool) {
	tenantIDStr := c.Param("page_id")
	if tenantIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page_id parameter missing", "code": "parameter_missing"})
		return 0, false
	}
	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_id parameter", "code": "invalid_id"})
		return 0, false
	}
	return uint(tenantID), true
}
