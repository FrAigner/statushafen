package handlers

import (
	"strings"
	"time"

	"github.com/fraigner/statushafen/models"
)

// Grenzen für das Anlegen von E-Mail-Abonnenten durch Seiten-Admins. Jedes
// neue Abo löst eine Bestätigungsmail an eine Adresse aus, die nie zugestimmt
// hat; ohne Grenzen ließe sich der Server zum Mailbomben fremder Adressen
// missbrauchen.
const (
	// MaxAdminSubscriberCreatesPerHour: neue E-Mail-Abos je Seite und Stunde.
	MaxAdminSubscriberCreatesPerHour = 30
	// maxPendingSubscriptionsPerRecipient: unbestätigte Abos je Empfänger über
	// alle Seiten innerhalb von 24 Stunden (jedes löst eine Mail aus).
	maxPendingSubscriptionsPerRecipient = 3
)

func errSubscriberRateLimited() error {
	return newCodedError("subscriber_rate_limited", "Zu viele neue Abonnenten in kurzer Zeit")
}

// checkAdminSubscriberLimits prüft die Grenzen für ein neues E-Mail-Abo.
// Gelöschte Zeilen zählen mit (Unscoped), sonst ließe sich die Grenze durch
// Löschen und Neuanlegen umgehen.
func (h *Handler) checkAdminSubscriberLimits(pageID uint, email string, now time.Time) error {
	var perPage int64
	if err := h.DB.Unscoped().Model(&models.Subscriber{}).
		Where("status_page_id = ? AND channel = 'email' AND created_at > ?", pageID, now.Add(-time.Hour)).
		Count(&perPage).Error; err != nil {
		return err
	}
	if perPage >= MaxAdminSubscriberCreatesPerHour {
		return errSubscriberRateLimited()
	}
	var perRecipient int64
	if err := h.DB.Unscoped().Model(&models.Subscriber{}).
		Where("lower(email) = ? AND channel = 'email' AND status = ? AND created_at > ?",
			strings.ToLower(email), models.SubscriberStatusPending, now.Add(-24*time.Hour)).
		Count(&perRecipient).Error; err != nil {
		return err
	}
	if perRecipient >= maxPendingSubscriptionsPerRecipient {
		return errSubscriberRateLimited()
	}
	return nil
}
