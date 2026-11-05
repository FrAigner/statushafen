package handlers

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// subscriberFromToken validiert den ?token=-Query-Parameter und lädt den
// zugehörigen Subscriber, gescoped auf die im Token enthaltene StatusPageID
// Ein ungültiger/manipulierter Token oder ein Token für einen
// inzwischen gelöschten Subscriber liefert ok=false, ohne zwischen "Token
// falsch" und "Subscriber existiert nicht mehr" zu unterscheiden - beides
// führt zum selben, generischen Fehler, damit kein Enumeration-Leak über
// gültige Subscriber-/Statusseiten-IDs entsteht.
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

// subscriberTokenGrantsPageAccess prüft, ob der ?token=-Query-Parameter einer
// Anfrage einen gültigen, auf pageID gescopten Subscriber-Token trägt
// (Lesezugriff auf private Statusseiten ohne Login). Ein Token für eine
// andere Statusseite gewährt bewusst KEINEN Zugriff, selbst wenn er
// anderweitig gültig ist.
func (h *Handler) subscriberTokenGrantsPageAccess(c *gin.Context, pageID uint) bool {
	sub, ok := h.subscriberFromToken(c.Query("token"))
	return ok && sub.StatusPageID == pageID && h.subscriberCreatorMayReadPage(sub, pageID)
}

// subscriberCreatorMayReadPage prüft für private Seiten, ob der User, der das
// Abo angelegt hat, die Seite selbst noch sehen darf (Global Admin oder Rolle
// auf der Seite). Die Prüfung beim
// Anlegen reicht nicht. Über die Lücke angelegte Altabos ohne Ersteller, Abos
// aus der Zeit, bevor eine Seite privat wurde, und Abos nach einem Rollenentzug
// behielten sonst dauerhaft Lesezugriff und Vorfall-Mails. Admin-angelegte Abos
// für Externe bleiben gültig, solange der anlegende Admin berechtigt ist.
func (h *Handler) subscriberCreatorMayReadPage(sub models.Subscriber, pageID uint) bool {
	if sub.CreatedByUserID == nil {
		return false
	}
	return h.userStillBelongsToPage(*sub.CreatedByUserID, pageID)
}

// userStillBelongsToPage meldet, ob der User Global Admin ist oder eine Rolle
// auf der Seite hat. Unbekannter User und Datenbankfehler gelten als "nein"
// (fail closed): Ein abgeleitetes Besitzrecht, etwa an einem von ihm angelegten
// Abo, endet mit dem Rollenentzug.
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

// unsubscribeRequestToken liest den Abbestell-Token aus ?token= (RFC 8058:
// der Mail-Client POSTet "List-Unsubscribe=One-Click" an die URL aus dem
// List-Unsubscribe-Header, der Token steckt also in der URL) oder aus dem
// JSON-Body {"token": ...} (Unsubscribe.vue, analog zu
// /api/public/subscribers/confirm). Ein unlesbarer Body ergibt "".
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

// UnsubscribeHandler entfernt einen Subscriber anhand eines signierten Tokens
// ohne dass sich der Aufrufer einloggen muss - der bisher
// einzige Weg war, dass ein Statusseiten-Admin den Subscriber manuell über
// DELETE /api/pages/:page_id/subscribers/:id entfernt.
//
// läuft nur noch per POST. Als GET-Route löschte
// schon das bloße Abrufen des Links das Abo - Link-Scanner und Prefetcher
// von Mail-Gateways beendeten so Abos ungewollt (GET muss sicher sein).
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

// UnsubscribeRedirectHandler bedient GET /api/public/subscribers/unsubscribe
// ältere Mails oder Clients können noch direkt
// auf den API-Endpunkt verlinken. Statt zu löschen, leitet GET jetzt auf die
// Frontend-Seite /unsubscribe weiter, die das Abbestellen erst per Knopf
// (POST) auslöst - analog zur Double-Opt-in-Bestätigung.
func (h *Handler) UnsubscribeRedirectHandler(c *gin.Context) {
	target := getFrontendURL() + "/unsubscribe"
	if token := c.Query("token"); token != "" {
		target += "?token=" + url.QueryEscape(token)
	}
	c.Redirect(http.StatusSeeOther, target)
}

// listUnsubscribeHeaders liefert die RFC-8058-Header (One-Click-Abbestellen)
// für eine Benachrichtigungsmail an einen Abonnenten.
// Die URL zeigt auf den POST-Endpunkt, der unter derselben Origin
// wie das Frontend erreichbar ist (Caddy/Vite-Proxy leiten /api weiter).
// Mail-Clients POSTen "List-Unsubscribe=One-Click" dorthin; ein GET (Scanner)
// landet nur auf der Weiterleitung oben.
func listUnsubscribeHeaders(frontendURL, token string) []string {
	return []string{
		"List-Unsubscribe: <" + frontendURL + "/api/public/subscribers/unsubscribe?token=" + url.QueryEscape(token) + ">",
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
	}
}
