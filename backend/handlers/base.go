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
	// ExternalStatus: polling service for external status pages,
	// used by "Jetzt abrufen" and started in main.go. Tests replace it
	// with a service using a fake provider.
	ExternalStatus *externalstatus.Service

	// Now replaces the clock of the availability calculation, nil = time.Now.
	Now        func() time.Time
	availCache availabilityCache
	// lastStatusPrune: last cleanup of the status history;
	// accessible only to the scheduler goroutine.
	lastStatusPrune time.Time
	// translationsCache: 60 s cache of the bundled status page translations.
	translationsCache publicTranslationsCache

	// dispatchWG counts the goroutines of DispatchNotifications (WaitNotifications).
	dispatchWG sync.WaitGroup

	// loginThrottle counts failed attempts per account (see LoginHandler).
	loginThrottle *middleware.AccountThrottle

	// HTTP clients of the translation services. nil = production value
	// (translationClient). Tests set them to clients against httptest servers.
	// SafeClient checks targets against SSRF (freely enterable address),
	// TrustedClient calls the address from the operator environment (LIBRETRANSLATE_URL,
	// e.g. a Compose service) without an IP block. DeepLURL overrides the
	// fixed DeepL host (tests only).
	TranslationSafeClient    *http.Client
	TranslationTrustedClient *http.Client
	TranslationDeepLURL      string

	// incidentMails counts running incident notifications, see WaitIncidentMails.
	incidentMails sync.WaitGroup
	// outageMails counts running outage mails, see WaitOutageMails.
	outageMails sync.WaitGroup
	// mailQ limits the confirmation mails in the background, see mail_queue.go.
	mailQ mailQueue
}

func NewHandler(db *gorm.DB) *Handler {
	h := &Handler{DB: db, loginThrottle: middleware.NewLoginThrottle()}
	// The service reports source outages to the handler, which
	// sends the mails via deliverMail.
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

// getUserID returns the ID of the logged-in user, or 0 if not logged in
// (e.g. anonymous subscribing via OptionalAuthMiddleware).
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

// parseTenantID reads and validates :page_id from the URL and, on
// errors, replies itself with the appropriate JSON error (ok=false signals the
// caller to return immediately). Replacement for the previously, in
// Create-/Update-/DeleteComponentHandler (and analogously for incidents),
// triplicated page_id parsing boilerplate.
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
