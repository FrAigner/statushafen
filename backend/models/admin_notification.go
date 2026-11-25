package models

import "time"

// Admin message center (bell in the admin area).
//
// AdminNotification is a message to the admins of a status page
// (StatusPageID set) or to all global admins (StatusPageID nil,
// system-wide). The texts are NOT in the database: TitleKey/BodyKey are
// i18n keys (adminNotifications.*) that the frontend translates with params.
// Params contains only non-critical values (channel, ID, seconds), never
// webhook URLs, keys, tokens or email addresses of subscribers
// (see handlers.NotifyAdmins).
//
// The service merges identical messages: DedupKey (with page prefix)
// identifies an open message; within 15 minutes Count
// is incremented. Resolved or expired rows have an empty DedupKey and
// thus no longer take part in the partial unique index
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
	// Params: JSON object as text (portable between Postgres and SQLite).
	Params   string `gorm:"type:text;not null;default:'{}'" json:"-"`
	LinkPath string `gorm:"not null;default:''" json:"link_path"`

	// DedupKey: key of the OPEN message (partially unique, empty after
	// replacement/resolution). GroupKey remains permanently and allows resolving
	// also replaced but still open rows of the same message.
	DedupKey    string     `gorm:"not null;default:''" json:"-"`
	GroupKey    string     `gorm:"not null;default:'';index" json:"-"`
	Count       int        `gorm:"column:count;not null;default:1" json:"count"`
	FirstSeenAt time.Time  `gorm:"not null" json:"first_seen_at"`
	LastSeenAt  time.Time  `gorm:"not null;index" json:"last_seen_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
}

// Severity levels of an AdminNotification.
const (
	AdminNotificationInfo    = "info"
	AdminNotificationWarning = "warning"
	AdminNotificationError   = "error"
)

// NotificationRead: read status per user. Unique over
// (notification_id, user_id), created via raw SQL in
// database.CreateAdminNotificationIndexes. If a message is triggered
// again, the service deletes the rows and it counts as
// unread for everyone again.
type NotificationRead struct {
	ID             uint               `gorm:"primaryKey" json:"-"`
	NotificationID uint               `gorm:"not null;index" json:"notification_id"`
	Notification   *AdminNotification `gorm:"foreignKey:NotificationID;constraint:OnDelete:CASCADE" json:"-"`
	UserID         uint               `gorm:"not null;index" json:"user_id"`
	User           *User              `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	ReadAt         time.Time          `gorm:"not null" json:"read_at"`
}
