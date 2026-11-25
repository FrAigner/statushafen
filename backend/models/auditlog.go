package models

import (
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

// AuditCategories are all categories that the audit log writes.
// The ?category= filter of the audit list accepts only these values; the
// frontend shows a chip per category. New category = add it here,
// add the chip and label (auditLog.cat*) in the frontend -
// TestAuditCategories_CoverEveryWrittenCategory and the frontend check
// fail otherwise.
var AuditCategories = []string{
	"auth", "incident", "config", "user", "subscriber", "notice", "maintenance", "system",
}

// IsAuditCategory reports whether c is a known audit category.
func IsAuditCategory(c string) bool {
	for _, known := range AuditCategories {
		if known == c {
			return true
		}
	}
	return false
}

// AuditLog is an append-only entry for tamper-proof logging
// of all security-relevant admin actions (addresses review finding A4).
//
// Migration: add to the AutoMigrate list in database.InitDB():
//
//	db.AutoMigrate(&models.AuditLog{})
type AuditLog struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	CreatedAt  int64  `gorm:"autoCreateTime;index" json:"created_at"` // Unix seconds, avoids time-zone trouble
	ActorID    *uint  `gorm:"index" json:"actor_id"`                  // nil = system/automation
	ActorEmail string `gorm:"index" json:"actor_email"`
	ActorRole  string `json:"actor_role"`

	// Action is a short, machine-readable identifier, e.g. "incident.status_changed",
	// "user.role_changed", "auth.login_failed", "postmortem.published".
	Action     string `gorm:"not null;index" json:"action"`
	Category   string `gorm:"not null;index" json:"category"` // see AuditCategories
	TargetType string `json:"target_type"`                    // "Incident" | "User" | "Component" | ...
	TargetID   string `json:"target_id"`                      // free-form field (ID or slug/code)
	TargetName string `json:"target_name"`                    // human-readable for the UI

	// StatusPageID allows later scoping of the log per tenant (optional).
	StatusPageID *uint  `gorm:"index" json:"status_page_id"`
	IP           string `json:"ip"`
	// Details: optional JSON with before/after etc. (tamper-proof).
	Details string `gorm:"type:text" json:"details,omitempty"`
}

// Upper limits (in characters) for audit fields. Several come from user input
// (email of a failed login, honeypot field) and would otherwise be written
// into the table without limit.
const (
	auditMaxEmail   = 254
	auditMaxText    = 255
	auditMaxAction  = 128
	auditMaxShort   = 64
	auditMaxDetails = 4000
)

// BeforeCreate truncates the text fields centrally, whichever path creates
// the entry (RecordAudit, RecordSystemAudit, transactions). The cut is made
// by characters, not by bytes, and invalid UTF-8 sequences are replaced
// (Postgres rejects them in text columns, the entry would otherwise be lost).
func (a *AuditLog) BeforeCreate(_ *gorm.DB) error {
	a.ActorEmail = limitAuditText(a.ActorEmail, auditMaxEmail)
	a.ActorRole = limitAuditText(a.ActorRole, auditMaxText)
	a.Action = limitAuditText(a.Action, auditMaxAction)
	a.Category = limitAuditText(a.Category, auditMaxShort)
	a.TargetType = limitAuditText(a.TargetType, auditMaxShort)
	a.TargetID = limitAuditText(a.TargetID, auditMaxText)
	a.TargetName = limitAuditText(a.TargetName, auditMaxText)
	a.IP = limitAuditText(a.IP, auditMaxShort)
	a.Details = limitAuditText(a.Details, auditMaxDetails)
	return nil
}

func limitAuditText(s string, maxRunes int) string {
	if len(s) <= maxRunes && utf8.ValidString(s) {
		return s // fast path: short and valid
	}
	s = strings.ToValidUTF8(s, "\uFFFD")
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}
