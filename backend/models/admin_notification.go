package models

import "time"

// Admin-Nachrichtencenter (Glocke im Admin-Bereich).
//
// AdminNotification ist eine Nachricht an die Admins einer Statusseite
// (StatusPageID gesetzt) oder an alle Global Admins (StatusPageID nil,
// systemweit). Die Texte stehen NICHT in der Datenbank: TitleKey/BodyKey sind
// i18n-Schlüssel (adminNotifications.*), die das Frontend mit Params
// übersetzt. Params enthält nur unkritische Werte (Kanal, ID, Sekunden), nie
// Webhook-URLs, Schlüssel, Tokens oder E-Mail-Adressen von Abonnenten
// (siehe handlers.NotifyAdmins).
//
// Gleiche Meldungen fasst der Dienst zusammen: DedupKey (mit Seitenpräfix)
// identifiziert eine offene Meldung; innerhalb von 15 Minuten zählt Count
// hoch. Erledigte oder abgelaufene Zeilen haben einen leeren DedupKey und
// nehmen so nicht mehr am partiellen Unique-Index teil
// (database.CreateAdminNotificationIndexes).
type AdminNotification struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	StatusPageID *uint       `gorm:"index" json:"status_page_id"`
	StatusPage   *StatusPage `gorm:"foreignKey:StatusPageID;constraint:OnDelete:CASCADE" json:"-"`

	Kind     string `gorm:"not null" json:"kind"`
	Severity string `gorm:"not null;default:'info'" json:"severity"`
	TitleKey string `gorm:"not null" json:"title_key"`
	BodyKey  string `gorm:"not null;default:''" json:"body_key"`
	// Params: JSON-Objekt als Text (portabel zwischen Postgres und SQLite).
	Params   string `gorm:"type:text;not null;default:'{}'" json:"-"`
	LinkPath string `gorm:"not null;default:''" json:"link_path"`

	// DedupKey: Schlüssel der OFFENEN Meldung (partiell eindeutig, leer nach
	// Ablösung/Erledigung). GroupKey bleibt dauerhaft und erlaubt, auch
	// abgelöste, aber noch offene Zeilen derselben Meldung zu erledigen.
	DedupKey    string     `gorm:"not null;default:''" json:"-"`
	GroupKey    string     `gorm:"not null;default:'';index" json:"-"`
	Count       int        `gorm:"column:count;not null;default:1" json:"count"`
	FirstSeenAt time.Time  `gorm:"not null" json:"first_seen_at"`
	LastSeenAt  time.Time  `gorm:"not null;index" json:"last_seen_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
}

// Schweregrade einer AdminNotification.
const (
	AdminNotificationInfo    = "info"
	AdminNotificationWarning = "warning"
	AdminNotificationError   = "error"
)

// NotificationRead: Lesestatus je Benutzer. Eindeutig über
// (notification_id, user_id), angelegt per Raw-SQL in
// database.CreateAdminNotificationIndexes. Wird eine Nachricht erneut
// ausgelöst, löscht der Dienst die Zeilen und sie gilt für alle wieder als
// ungelesen.
type NotificationRead struct {
	ID             uint               `gorm:"primaryKey" json:"-"`
	NotificationID uint               `gorm:"not null;index" json:"notification_id"`
	Notification   *AdminNotification `gorm:"foreignKey:NotificationID;constraint:OnDelete:CASCADE" json:"-"`
	UserID         uint               `gorm:"not null;index" json:"user_id"`
	User           *User              `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	ReadAt         time.Time          `gorm:"not null" json:"read_at"`
}
