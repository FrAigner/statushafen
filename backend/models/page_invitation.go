package models

import (
	"time"

	"gorm.io/gorm"
)

// PageInvitation: pending invitation of an account to a
// status page. Non-global admins no longer attach an account directly to their
// page but create an invitation; only the account holder turns it into a membership
// with the link from the mail (signed in with exactly this address).
//
//   - Email: the address entered by the page admin (lowercase). The
//     row is also created for addresses without an account, so that response and list do not
//     reveal whether an account exists. Without an account no mail is sent.
//   - TokenHash: only the SHA-256 of the one-time token; the plaintext is only in the
//     mail link. Accepting deletes the row permanently. Withdrawing and declining
//     soft-delete it (tombstone) and keep the 30-minute block per (page,
//
// address) in place (review B1); the maintenance tick cleans it up afterwards.
//   - ExpiresAt: after that the invitation no longer applies (pageInvitationTTL).
//   - InvitedByID: who invited. On acceptance this person must still be allowed to assign the role
//     on the page, otherwise the invitation lapses.
//   - InvitedUserID: the (attachable) account found at invitation time, otherwise 0.
//     Only this account can accept; an account created later with the same address
//
// does not (security review). Never in API responses.
//
// Deliberately without foreign keys: page, role and inviter are freshly checked on
// acceptance (soft-deleted pages/roles are then not found).
type PageInvitation struct {
	gorm.Model
	StatusPageID  uint      `gorm:"not null;index"`
	Email         string    `gorm:"not null;index"`
	RoleID        uint      `gorm:"not null"`
	TokenHash     string    `gorm:"size:64;not null;uniqueIndex"`
	ExpiresAt     time.Time `gorm:"not null;index"` // H8: cleanup in the maintenance tick
	InvitedByID   uint      `gorm:"not null"`
	InvitedUserID uint      `gorm:"not null;default:0;index"`
}
