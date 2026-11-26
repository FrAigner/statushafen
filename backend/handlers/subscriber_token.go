package handlers

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// subscriberFromToken validates the ?token= query parameter and loads the
// associated subscriber, scoped to the StatusPageID contained in the token.
// An invalid/tampered token or a token for a
// subscriber that has since been deleted yields ok=false, without distinguishing between "token
// wrong" and "subscriber no longer exists" - both
// lead to the same generic error, so that no enumeration leak about
// valid subscriber/status page IDs arises.
func (h *Handler) subscriberFromToken(token string) (models.Subscriber, bool) {
	var sub models.Subscriber
	if token == "" {
		return sub, false
	}
	claims, err := middleware.ParseSubscriberToken(token)
	if err != nil {
		return sub, false
	}
	if err := h.DB.Where("id = ? AND status_page_id = ?", claims.SubscriberID, claims.StatusPageID).First(&sub).Error; err != nil {
		return sub, false
	}
	return sub, true
}

// subscriberTokenGrantsPageAccess checks whether the ?token= query parameter of a
// request carries a valid subscriber token scoped to pageID
// (read access to private status pages without login). A token for a
// different status page deliberately grants NO access, even if it is
// otherwise valid.
func (h *Handler) subscriberTokenGrantsPageAccess(c *gin.Context, pageID uint) bool {
	sub, ok := h.subscriberFromToken(c.Query("token"))
	return ok && sub.StatusPageID == pageID && h.subscriberCreatorMayReadPage(sub, pageID)
}

// subscriberCreatorMayReadPage checks for private pages whether the user who created the
// subscription may still see the page itself (Global Admin or role
// on the page). The check at
// creation time is not enough. Legacy subscriptions without a creator created via the gap, subscriptions
// from the time before a page became private, and subscriptions after a role revocation
// would otherwise retain read access and incident mails permanently. Admin-created subscriptions
// for external parties remain valid as long as the creating admin is authorized.
func (h *Handler) subscriberCreatorMayReadPage(sub models.Subscriber, pageID uint) bool {
	if sub.CreatedByUserID == nil {
		return false
	}
	return h.userStillBelongsToPage(*sub.CreatedByUserID, pageID)
}

// userStillBelongsToPage reports whether the user is a Global Admin or has a role
// on the page. Unknown user and database errors count as "no"
// (fail closed): a derived ownership right, for example to a subscription
// they created, ends with the role revocation.
func (h *Handler) userStillBelongsToPage(userID, pageID uint) bool {
	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		return false
	}
	if user.IsGlobalAdmin {
		return true
	}
	var role models.UserStatusPageRole
	return h.DB.Where(whereUserIDAndStatusPageID, user.ID, pageID).First(&role).Error == nil
}

// unsubscribeRequestToken reads the unsubscribe token from ?token= (RFC 8058:
// the mail client POSTs "List-Unsubscribe=One-Click" to the URL from the
// List-Unsubscribe header, so the token is in the URL) or from the
// JSON body {"token": ...} (Unsubscribe.vue, analogous to
// /api/public/subscribers/confirm). An unreadable body yields "".
func unsubscribeRequestToken(c *gin.Context) string {
	if token := c.Query("token"); token != "" {
		return token
	}
	var body struct {
		Token string `json:"token"`
	}
	_ = c.ShouldBindJSON(&body)
	return body.Token
}

// UnsubscribeHandler removes a subscriber by means of a signed token
// without the caller having to log in - previously the
// only way was for a status page admin to manually remove the subscriber via
// DELETE /api/pages/:page_id/subscribers/:id.
//
// Unsubscribing now runs only via POST. As a GET route,
// merely fetching the link deleted the subscription - link scanners and prefetchers
// of mail gateways thus ended subscriptions unintentionally (GET must be safe).
func (h *Handler) UnsubscribeHandler(c *gin.Context) {
	sub, ok := h.subscriberFromToken(unsubscribeRequestToken(c))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültiger oder abgelaufener Abbestell-Link", "code": "unsubscribe_link_invalid"})
		return
	}
	if err := h.DB.Delete(&sub).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Abbestellen fehlgeschlagen", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "subscriber.unsubscribed", "subscriber", "Subscriber", strconv.FormatUint(uint64(sub.ID), 10), sub.Email, "")
	c.JSON(http.StatusOK, gin.H{"message": "Erfolgreich abbestellt"})
}

// UnsubscribeRedirectHandler serves GET /api/public/subscribers/unsubscribe:
// older mails or clients may still link directly
// to the API endpoint. Instead of deleting, GET now redirects to the
// frontend page /unsubscribe, which only triggers the unsubscribe via a button
// (POST) - analogous to the double opt-in confirmation.
func (h *Handler) UnsubscribeRedirectHandler(c *gin.Context) {
	target := getFrontendURL() + "/unsubscribe"
	if token := c.Query("token"); token != "" {
		target += "?token=" + url.QueryEscape(token)
	}
	c.Redirect(http.StatusSeeOther, target)
}

// listUnsubscribeHeaders returns the RFC 8058 headers (one-click unsubscribe)
// for a notification mail to a subscriber.
// The URL points to the POST endpoint, which is reachable under the same origin
// as the frontend (Caddy/Vite proxy forward /api).
// Mail clients POST "List-Unsubscribe=One-Click" there; a GET (scanner)
// only lands on the redirect above.
func listUnsubscribeHeaders(frontendURL, token string) []string {
	return []string{
		"List-Unsubscribe: <" + frontendURL + "/api/public/subscribers/unsubscribe?token=" + url.QueryEscape(token) + ">",
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
	}
}
