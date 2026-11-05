package models

import (
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

// AuditCategories sind alle Kategorien, die das Audit-Log schreibt.
// Der Filter ?category= der Audit-Liste akzeptiert nur diese Werte; das
// Frontend zeigt je Kategorie einen Chip. Neue Kategorie = hier eintragen,
// Chip und Beschriftung (auditLog.cat*) im Frontend ergänzen -
// TestAuditCategories_CoverEveryWrittenCategory und die Frontend-Prüfung
// schlagen sonst fehl.
var AuditCategories = []string{
	"auth", "incident", "config", "user", "subscriber", "notice", "maintenance", "system",
}

// IsAuditCategory meldet, ob c eine bekannte Audit-Kategorie ist.
func IsAuditCategory(c string) bool {
	for _, known := range AuditCategories {
		if known == c {
			return true
		}
	}
	return false
}

// AuditLog ist ein append-only Eintrag für revisionssichere Protokollierung
// aller sicherheitsrelevanten Admin-Aktionen (Adressiert Review-Befund A4).
//
// Migration: In database.InitDB() zur AutoMigrate-Liste hinzufügen:
//
//	db.AutoMigrate(&models.AuditLog{})
type AuditLog struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	CreatedAt  int64  `gorm:"autoCreateTime;index" json:"created_at"` // Unix-Sekunden, spart Zeitzonen-Ärger
	ActorID    *uint  `gorm:"index" json:"actor_id"`                  // nil = System/Automation
	ActorEmail string `gorm:"index" json:"actor_email"`
	ActorRole  string `json:"actor_role"`

	// Action ist eine kurze, maschinenlesbare Kennung, z.B. "incident.status_changed",
	// "user.role_changed", "auth.login_failed", "postmortem.published".
	Action     string `gorm:"not null;index" json:"action"`
	Category   string `gorm:"not null;index" json:"category"` // siehe AuditCategories
	TargetType string `json:"target_type"`                    // "Incident" | "User" | "Component" | ...
	TargetID   string `json:"target_id"`                      // freies Feld (ID oder Slug/Code)
	TargetName string `json:"target_name"`                    // menschenlesbar für die UI

	// StatusPageID erlaubt späteres Scoping des Logs pro Mandant (optional).
	StatusPageID *uint  `gorm:"index" json:"status_page_id"`
	IP           string `json:"ip"`
	// Details: optionales JSON mit before/after o.ä. (revisionssicher).
	Details string `gorm:"type:text" json:"details,omitempty"`
}

// Obergrenzen (in Zeichen) für Audit-Felder. Mehrere stammen aus Nutzereingaben
// (E-Mail eines fehlgeschlagenen Logins, Honeypot-Feld) und würden sonst
// unbegrenzt in die Tabelle geschrieben.
const (
	auditMaxEmail   = 254
	auditMaxText    = 255
	auditMaxAction  = 128
	auditMaxShort   = 64
	auditMaxDetails = 4000
)

// BeforeCreate kürzt die Textfelder zentral, egal welcher Pfad den Eintrag
// anlegt (RecordAudit, RecordSystemAudit, Transaktionen). Der Schnitt erfolgt
// nach Zeichen, nicht nach Bytes, und ungültige UTF-8-Folgen werden ersetzt
// (Postgres lehnt sie in text-Spalten ab, der Eintrag ginge sonst verloren).
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
		return s // schneller Pfad: kurz und gültig
	}
	s = strings.ToValidUTF8(s, "\uFFFD")
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}
