package handlers

import (
	"time"

	"github.com/fraigner/statushafen/models"
)

// subscriberCreatorDTO: what the subscriber list shows of the creating
// account. Previously the whole models.User object went out (email,
// SSOProvider, SSOSubject, IsGlobalAdmin, role including permissions,
// PreferredLanguage) – also from accounts of other tenants that merely
// subscribed to the page. Now only the email, and only if the account itself
// belongs to the staff of this page or is the requester. Otherwise Email stays empty; the object itself
// still means "linked to an account" (frontend: hint "can manage the
// subscription itself").
type subscriberCreatorDTO struct {
	Email string
}

// subscriberDTO is the output form of a subscriber for admin responses.
// The JSON keys correspond to the previous ones (Go field names), without
// StatusPage, DeletedAt and CreatedByUserID.
type subscriberDTO struct {
	ID                     uint
	CreatedAt              time.Time
	UpdatedAt              time.Time
	StatusPageID           uint
	Email                  string
	WebhookURL             string
	Channel                string
	SubscribedComponentIDs string
	Language               string
	Status                 string
	CreatedBy              *subscriberCreatorDTO
}

// subscriberDTOs converts subscribers of a page into DTOs. The query of the
// page roles runs once for all creators.
func (h *Handler) subscriberDTOs(subs []models.Subscriber, pageID, viewerID uint) []subscriberDTO {
	ids := make([]uint, 0, len(subs))
	for _, s := range subs {
		if s.CreatedByUserID != nil {
			ids = append(ids, *s.CreatedByUserID)
		}
	}
	emails := map[uint]string{}
	viewerEmail := ""
	if viewerID != 0 {
		var v models.User
		if h.DB.Select("email").First(&v, viewerID).Error == nil {
			viewerEmail = v.Email
		}
	}
	if len(ids) > 0 {
		var rows []struct {
			UserID uint
			Email  string
		}
		h.DB.Table("user_status_page_roles AS r").
			Select("DISTINCT r.user_id AS user_id, u.email AS email").
			Joins("JOIN users u ON u.id = r.user_id AND u.deleted_at IS NULL").
			Where("r.deleted_at IS NULL AND r.status_page_id = ? AND r.user_id IN ?", pageID, ids).
			Scan(&rows)
		for _, r := range rows {
			emails[r.UserID] = r.Email
		}
	}
	out := make([]subscriberDTO, len(subs))
	for i, s := range subs {
		d := subscriberDTO{
			ID: s.ID, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, StatusPageID: s.StatusPageID,
			Email: s.Email, WebhookURL: s.WebhookURL, Channel: s.Channel,
			SubscribedComponentIDs: s.SubscribedComponentIDs, Language: s.Language, Status: s.Status,
		}
		if s.CreatedByUserID != nil {
			d.CreatedBy = &subscriberCreatorDTO{Email: emails[*s.CreatedByUserID]}
			// Own data: the requester always sees their address, even as
			// Global Admin without a page role.
			if viewerID != 0 && *s.CreatedByUserID == viewerID {
				d.CreatedBy.Email = viewerEmail
			}
		}
		out[i] = d
	}
	return out
}

func (h *Handler) subscriberDTO(sub models.Subscriber, viewerID uint) subscriberDTO {
	return h.subscriberDTOs([]models.Subscriber{sub}, sub.StatusPageID, viewerID)[0]
}
