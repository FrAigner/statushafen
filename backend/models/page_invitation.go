package models

import (
	"time"

	"gorm.io/gorm"
)

// PageInvitation: ausstehende Einladung eines Kontos auf eine
// Statusseite. Nicht-Global-Admins hängen ein Konto nicht mehr direkt an ihre
// Seite, sondern legen eine Einladung an; erst der Kontoinhaber macht sie mit
// dem Link aus der Mail (angemeldet mit genau dieser Adresse) zur Mitgliedschaft.
//
//   - Email: die vom Seitenadmin eingegebene Adresse (klein geschrieben). Die
//     Zeile entsteht auch für Adressen ohne Konto, damit Antwort und Liste nicht
//     verraten, ob ein Konto existiert. Ohne Konto geht keine Mail raus.
//   - TokenHash: nur der SHA-256 des Einmal-Tokens; der Klartext steht allein im
//     Mail-Link. Annehmen löscht die Zeile endgültig. Zurückziehen und Ablehnen
//     löschen sie weich (Grabstein) und halten die 30-Minuten-Sperre je (Seite,
//
// Adresse) aufrecht (Review B1); der Wartungs-Tick räumt sie danach ab.
//   - ExpiresAt: danach gilt die Einladung nicht mehr (pageInvitationTTL).
//   - InvitedByID: wer eingeladen hat. Beim Annehmen muss diese Person die Rolle
//     auf der Seite noch vergeben dürfen, sonst verfällt die Einladung.
//   - InvitedUserID: das beim Einladen gefundene (anhängbare) Konto, sonst 0.
//     Nur dieses Konto kann annehmen; ein später mit derselben Adresse neu
//
// angelegtes Konto nicht (Security-Review). Nie in API-Antworten.
//
// Bewusst ohne Fremdschlüssel: Seite, Rolle und Einladender werden beim
// Annehmen frisch geprüft (soft-gelöschte Seiten/Rollen finden sich dann nicht).
type PageInvitation struct {
	gorm.Model
	StatusPageID  uint      `gorm:"not null;index"`
	Email         string    `gorm:"not null;index"`
	RoleID        uint      `gorm:"not null"`
	TokenHash     string    `gorm:"size:64;not null;uniqueIndex"`
	ExpiresAt     time.Time `gorm:"not null;index"` // H8: Aufräumen im Wartungs-Tick
	InvitedByID   uint      `gorm:"not null"`
	InvitedUserID uint      `gorm:"not null;default:0;index"`
}
