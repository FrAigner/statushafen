package handlers

import (
	"strings"
	"time"

	"github.com/fraigner/statushafen/models"
)

// Limits for creating email subscribers by page admins. Every
// new subscription triggers a confirmation mail to an address that never agreed;
// without limits the server could be used to mail-bomb third-party addresses
// missbrauchen.
const (
	// MaxAdminSubscriberCreatesPerHour: new email subscriptions per page and hour.
	MaxAdminSubscriberCreatesPerHour = 30
	// maxPendingSubscriptionsPerRecipient: unconfirmed subscriptions per recipient across
	// all pages within 24 hours (each one triggers a mail).
	maxPendingSubscriptionsPerRecipient = 3
)

func errSubscriberRateLimited() error {
	return newCodedError("subscriber_rate_limited", "Zu viele neue Abonnenten in kurzer Zeit")
}

// checkAdminSubscriberLimits checks the limits for a new email subscription.
// Deleted rows count too (Unscoped), otherwise the limit could be bypassed by
// deleting and recreating.
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
