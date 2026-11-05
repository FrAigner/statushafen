package handlers

import (
	"time"

	"github.com/fraigner/statushafen/models"
)

// subscriberCreatorDTO: was die Abonnentenliste vom anlegenden
// Konto zeigt. Früher ging das ganze models.User-Objekt hinaus (E-Mail,
// SSOProvider, SSOSubject, IsGlobalAdmin, Rolle samt Permissions,
// PreferredLanguage) – auch von Konten fremder Mandanten, die die Seite nur
// abonniert haben. Jetzt nur die E-Mail, und die nur, wenn das Konto selbst
// zum Personal dieser Seite gehört oder der Anfragende selbst ist. Sonst bleibt Email leer; das Objekt selbst
// bedeutet weiterhin "mit einem Konto verknüpft" (Frontend: Hinweis "kann das
// Abo selbst verwalten").
type subscriberCreatorDTO struct {
	Email string
}

// subscriberDTO ist die Ausgabeform eines Abonnenten für Admin-Antworten.
// Die JSON-Schlüssel entsprechen den bisherigen (Go-Feldnamen), ohne
// StatusPage, DeletedAt und CreatedByUserID.
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

// subscriberDTOs wandelt Abonnenten einer Seite in DTOs um. Die Abfrage der
// Seitenrollen läuft einmal für alle Ersteller.
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
			// Eigene Daten: der Anfragende sieht seine Adresse immer, auch als
			// Global Admin ohne Seitenrolle.
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
