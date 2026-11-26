package models

import (
	"time"

	"gorm.io/gorm"
)

// StatusPage represents a dedicated status page (tenant)
type StatusPage struct {
	gorm.Model
	Name string `gorm:"not null"`
	// Slug is unique among active pages. Deliberately NO gorm tag
	// uniqueIndex: a regular index also sees soft-deleted
	// pages and would block their slug forever. The partial index
	// idx_status_pages_slug_active is created via raw SQL in
	// database.CreateSoftDeleteUniqueIndexes.
	Slug string `gorm:"not null"`
	// IsSystem marks the system page. The seed
	// finds it through this flag instead of the slug: a changed slug
	// used to produce a second system page on the next start. At most
	// one active page carries the flag (partial index
	// idx_status_pages_system_active in database.CreateSoftDeleteUniqueIndexes).
	IsSystem        bool `gorm:"not null;default:false"`
	Description     string
	LogoURL         string
	CustomDomain    string
	IsPublicIndexed bool  `gorm:"default:true"`
	IsPrivate       bool  `gorm:"default:false"`
	DefaultRoleID   *uint `gorm:"default:null"`
	// AutoJoinEmailDomains: comma-separated, lowercase
	// allowlist of email domains. Auto-join via DefaultRoleID only applies to
	// accounts with a matching domain; empty = no auto-join.
	AutoJoinEmailDomains string `gorm:"default:''"`

	// WebhookTokenHash authenticates the incoming monitoring webhook,
	// which may set the component status without normal session/JWT auth. Only
	// the hash is stored (HashToken, column webhook_token); the plaintext is
	// delivered once at creation. Empty =
	// no token issued, the webhook endpoint then rejects every request. Deliberately
	// excluded from the normal StatusPage JSON representation via json:"-",
	// so the token can never be sent or overwritten through the generic read endpoints (GET /api/pages, ...)
	// or an unwary PUT bind - it is set exclusively through the
	// dedicated generate/revoke endpoints and
	// returned once.
	WebhookTokenHash string `gorm:"column:webhook_token;default:null" json:"-"`

	// Branding: purely additive fields for page-specific
	// appearance of the public status page (logo, colors, header layout).
	// The defaults match exactly the previous global CSS values
	// (--primary / --text-secondary from frontend/src/style.css and
	// "classic" as the only header layout so far), so that existing pages
	// look unchanged after the migration until an admin sets the
	// fields actively.
	PrimaryColor   string `gorm:"default:'#4c6ef5'"`
	SecondaryColor string `gorm:"default:'#5e6c84'"`
	// HeaderStyle: "classic" (default, previous layout), "centered",
	// "minimal" - see frontend/src/views/public/StatusPage.vue.
	HeaderStyle string `gorm:"not null;default:'classic'"`
	// PageLayout: layout of the public page,
	// "standard" (default, previous look) or "liste". Additive and
	// independent of HeaderStyle.
	PageLayout string `gorm:"not null;default:'standard'"`

	// CustomDomainStatus (TXT verification since):
	// verification pipeline for CustomDomain.
	// "" (empty)   -> no custom domain set.
	// "pending"   -> domain entered, DNS TXT record (proof of ownership) not yet
	//                (successfully) checked.
	// "verified"  -> TXT record with CustomDomainVerificationToken found;
	//                ownership proven. Caddy's on-demand TLS may request
	//                a certificate for this domain; the customer must now
	//                still set the CNAME to the edge server.
	// "active"    -> additionally, real public traffic over the
	//                domain was observed at least once (proof that DNS+TLS work end to end).
	// "check_failed" -> the last DNS check failed;
	//                CustomDomainCheckedAt/CustomDomainCheckError give the time
	//                and reason. Not resolvable, like "pending". Existing
	//                records stay "pending" until checked.
	CustomDomainStatus     string     `gorm:"default:''"`
	CustomDomainVerifiedAt *time.Time `gorm:"default:null"`
	// CustomDomainCheckedAt/-CheckError: time of the last
	// DNS check and, on failure, the reason as a stable code
	// ("txt_not_found" | "dns_lookup_failed"; the texts come from the
	// language files). Both empty as long as it was never checked.
	CustomDomainCheckedAt  *time.Time `gorm:"default:null"`
	CustomDomainCheckError string     `gorm:"default:''"`

	// CustomDomainVerificationToken: random value, newly generated for each domain
	// entry, which the customer must publish as a TXT record at their
	// DNS provider (value: "statushafen-verify=<token>")
	// to prove ownership of the domain - independent of the CNAME, which is only
	// set afterwards for the actual routing (standard procedure as
	// with Google Search Console/Mailgun). json:"-" like WebhookToken, since the
	// value should only be returned through the dedicated set/verify endpoints,
	// not through generic read endpoints including public pages.
	CustomDomainVerificationToken string `gorm:"default:''" json:"-"`

	// NotificationFromAddress: optional, page-specific
	// sender for incident/maintenance-window notification emails, in the same
	// "Display Name <address>" format as the global SMTP_FROM env var (see
	// smtp.go). Empty = fall back to SMTP_FROM (previous behavior,
	// unchanged for pages without their own configuration).
	NotificationFromAddress string `gorm:"default:''"`
}

type Role struct {
	gorm.Model
	// Name is unique among active roles. Deliberately NO gorm tag
	// uniqueIndex: the partial index idx_roles_name_active
	// is created via raw SQL in database.CreateSoftDeleteUniqueIndexes, so that the
	// name of a deleted role is free again.
	Name        string `gorm:"not null"`
	Permissions string `gorm:"not null"`
}

// GlobalAdminRoleName: name of the role that the seed and SSO bootstrap
// set as the global role (User.RoleID) of a Global Admin. Global Admin is
// a global property (User.IsGlobalAdmin); assigned as a page role,
// it would suggest platform rights but only granted the role's permissions
// on that one page. It is therefore not assignable as a page role.
const GlobalAdminRoleName = "Global Admin"

// GlobalOnly: true for the role that must not be assigned as a page role
// (UserStatusPageRole, StatusPage.DefaultRoleID).
func (r Role) GlobalOnly() bool {
	return r.Name == GlobalAdminRoleName
}

// SystemSetting: generic global key/value system settings.
// The first setting is orders.enabled (self-service ordering on/off). Deliberately
// an extensible key/value table instead of single columns, so that further
// global switches can follow without a schema change. Convention:
// a missing key means the respective default (for orders.enabled "on"),
// so that existing deployments stay unchanged.
type SystemSetting struct {
	gorm.Model
	Key   string `gorm:"uniqueIndex;not null"`
	Value string `gorm:"not null"`
}

type User struct {
	gorm.Model
	// Email is unique among active users (partial
	// index idx_users_email_active instead of gorm uniqueIndex, see Role.Name).
	Email        string `gorm:"not null"`
	PasswordHash string `json:"-"`
	RoleID       uint   `gorm:"default:null"`
	Role         Role   `gorm:"foreignKey:RoleID"`
	// AllowedCountries was replaced by UserTag (Country → Tags
	// step 4/4). The old DB column remains for now (rollback
	// safety net) but is no longer read or written.
	SSOProvider   string `gorm:"default:null"`
	SSOSubject    string `gorm:"default:null"`
	IsGlobalAdmin bool   `gorm:"default:false"`
	// PreferredLanguage stores the UI language chosen by the logged-in user
	// (i18n infrastructure). Purely additive - AutoMigrate creates the column
	// automatically, existing users get the empty default (the frontend
	// then falls back to the browser/fallback value). Anonymous visitors
	// store their language choice in the browser's localStorage instead.
	PreferredLanguage string `gorm:"default:''"`
	// HideHelpTooltips: help tooltips in the admin area switched off for
	// this user. Deliberately stored negated: the default
	// "on" is thus the zero value false. A bool with gorm default true would
	// be silently replaced by the default when GORM creates it with false,
	// and AutoMigrate creates the column for existing users with false.
	HideHelpTooltips bool `gorm:"default:false"`
	// TokenVersion travels as a claim in every login token and is compared
	// with the account on every request. An increase (password change,
	// revocation of rights, deletion) immediately invalidates all tokens issued
	// until then, without needing a blocklist.
	TokenVersion uint `gorm:"not null;default:0"`

	// The linked roles per status page
	UserStatusPageRoles []UserStatusPageRole `gorm:"foreignKey:UserID"`
}

// UserStatusPageRole: granular rights per status page and user
type UserStatusPageRole struct {
	gorm.Model
	UserID       uint       `gorm:"not null;index"`
	User         User       `gorm:"foreignKey:UserID"`
	StatusPageID uint       `gorm:"not null;index"`
	StatusPage   StatusPage `gorm:"foreignKey:StatusPageID"`
	RoleID       uint       `gorm:"not null"`
	Role         Role       `gorm:"foreignKey:RoleID"`
	// JoinedVia records how the membership came about: JoinedViaAutoJoin
	// (AutoJoinEmailDomains, which the page's admins set), JoinedViaAssigned
	// (invitation, admin, order; the default, see BeforeCreate) or
	// JoinedViaLegacy (from before this was recorded). Only memberships the
	// account agreed to let a login return to the page's custom domain.
	JoinedVia string `gorm:"not null;default:''"`
}

// Values of UserStatusPageRole.JoinedVia.
const (
	JoinedViaAssigned = "assigned"
	JoinedViaAutoJoin = "auto_join"
	JoinedViaLegacy   = "legacy"
)

// BeforeCreate marks a new membership as assigned unless the caller set its
// origin (the auto-join does).
func (r *UserStatusPageRole) BeforeCreate(*gorm.DB) error {
	if r.JoinedVia == "" {
		r.JoinedVia = JoinedViaAssigned
	}
	return nil
}

// PageOrder: self-service order of a new status page via the
// public landing page ("entry gate" homepage). Tracks the
// complete run from the order form to the fully provisioned page,
// without creating a loginable user before email confirmation -
// otherwise the public form itself would be a vector for arbitrarily
// many empty user rows. Status transitions: "pending_confirmation" (form
// submitted, confirmation mail sent) -> "confirmed" (link clicked,
// StatusPage/user/role created, password not yet set) -> "active"
// (password set, login possible). Expired, never-confirmed orders
// count as expired immediately in the checks of CreateOrderHandler (slug, quota);
// the maintenance tick sets them to "expired" and deletes them later.
type PageOrder struct {
	gorm.Model
	CompanyName string `gorm:"not null"`
	// Email is deliberately NOT unique - a contact may have several (historical)
	// orders. Abuse protection against mass orders
	// is instead provided by the pending cap in CreateOrderHandler (max. 3
	// simultaneously open orders per email) plus IP rate limiting.
	Email string `gorm:"not null;index"`
	// Slug: optional desired slug from the form. Empty = generated automatically
	// from CompanyName on confirmation (as with
	// CreateStatusPageHandler without a slug).
	Slug string `gorm:"default:null"`

	Status string `gorm:"not null;default:'pending_confirmation';index"`

	// ConfirmTokenHash/SetPasswordTokenHash: same pattern as
	// StatusPage.WebhookTokenHash. The tokens are 32 bytes of randomness (hex-encoded);
	// the columns confirm_token and set_password_token hold only the hash, the
	// plaintext is in the mail or in the response. Deliberately only
	// "index" instead of "uniqueIndex": after use the token is reset to ""
	// (see ConfirmOrderHandler/SetOrderPasswordHandler) - with
	// several already used orders a uniqueIndex on
	// empty strings would collide (GORM/Postgres/SQLite treat "" as a
	// normal value, not like NULL). Uniqueness in practice is given by the
	// cryptographic randomness of the 256-bit tokens, a
	// DB constraint is not needed here.
	ConfirmTokenHash   string     `gorm:"column:confirm_token;index;default:null" json:"-"`
	ConfirmTokenExpiry *time.Time `gorm:"default:null" json:"-"`

	SetPasswordTokenHash   string     `gorm:"column:set_password_token;index;default:null" json:"-"`
	SetPasswordTokenExpiry *time.Time `gorm:"default:null" json:"-"`

	StatusPageID *uint `gorm:"default:null"`
	UserID       *uint `gorm:"default:null"`

	// RequestIP: c.ClientIP() at order time, solely for
	// abuse analysis in the audit log - no functional use.
	RequestIP string `gorm:"default:null" json:"-"`
}

// Defaults of the SSO config: same values as the default tags
// below, so that the handler treats empty input on create and update
// the same and GET without a config reports the same values.
const (
	SSODefaultScopes         = "openid email profile"
	SSODefaultAttributeEmail = "email"
	SSODefaultAttributeName  = "name"
)

// StatusPageSSOConfig: per-status-page OIDC configuration.
// Empty config = SSO disabled.
type StatusPageSSOConfig struct {
	gorm.Model
	StatusPageID uint       `gorm:"not null;uniqueIndex" json:"status_page_id"`
	StatusPage   StatusPage `gorm:"foreignKey:StatusPageID" json:"-"`

	// OIDC configuration (OpenID Connect Discovery)
	OIDCEnabled        bool   `gorm:"default:false" json:"oidc_enabled"`
	OIDCDiscoveryURL   string `gorm:"default:''" json:"oidc_discovery_url"`
	OIDCClientID       string `gorm:"default:''" json:"oidc_client_id"`
	OIDCClientSecret   string `gorm:"default:''" json:"-"`
	OIDCScopes         string `gorm:"default:'openid email profile'" json:"oidc_scopes"`
	OIDCAttributeEmail string `gorm:"default:'email'" json:"oidc_attribute_email"`
	OIDCAttributeName  string `gorm:"default:'name'" json:"oidc_attribute_name"`
	// OIDCAttributeRole: not supported. A role from a
	// claim of the page IdP would be a privilege escalation bypassing
	// role management; the column remains only for existing data and the API shape.
	OIDCAttributeRole string `gorm:"default:''" json:"oidc_attribute_role"`
	// Callback URL (generated by the system, read-only)
	OIDCACSUrl string `gorm:"column:oidc_acs_url;default:''" json:"oidc_acs_url"`
	// Disable TestLogin when live
	TestLoginEnabled bool `gorm:"default:true" json:"test_login_enabled"`

	// The saml_* columns from the SAML era stay in existing databases
	// (AutoMigrate never drops columns) but are no longer read or written.
}

// OIDCLoginState holds the state, nonce and PKCE verifier of an
// ongoing OIDC login of a status page until the callback. Deliberately in the DB
// instead of process memory (like ssoCodeStore), because the IdP login lies between start and
// callback and a backend restart would otherwise abort every
// open login. Without gorm.Model (no soft delete): the callback
// deletes the row hard, so that a state can be redeemed exactly once.
// LinkUserID != 0 marks a linking login from an existing,
// signed-in account. TestLogin marks the test sign-in of a
// page admin from the SSO page: it only checks the flow at the provider,
// without a session and without linking.
type OIDCLoginState struct {
	ID           uint      `gorm:"primaryKey"`
	State        string    `gorm:"not null;uniqueIndex"`
	Nonce        string    `gorm:"not null"`
	CodeVerifier string    `gorm:"not null"`
	StatusPageID uint      `gorm:"not null"`
	LinkUserID   uint      `gorm:"not null;default:0"`
	TestLogin    bool      `gorm:"not null;default:false"`
	ExpiresAt    time.Time `gorm:"not null;index"`
	CreatedAt    time.Time
	// Provider is the ID of the global OIDC provider (OIDC_PROVIDERS) of this
	// login; empty for a page SSO login (StatusPageID is 0 otherwise).
	Provider string `gorm:"not null;default:''"`
	// ReturnTo is the validated frontend path the login started from
	// (handlers.sanitizeReturnTo); handed out with the one-time code.
	ReturnTo string `gorm:"not null;default:''"`
	// ReturnHost is the verified custom domain the login started on
	// (handlers.acceptReturnHost), "" for FRONTEND_URL. Checked again on the
	// callback.
	ReturnHost string `gorm:"not null;default:''"`
	// BrowserBinding is the hash of the binding cookie set on the host of the
	// login start; DomainBinding the one set on the custom domain in
	// ReturnHost before the move to BASE_URL (handlers/login_binding.go).
	BrowserBinding string `gorm:"not null;default:''"`
	DomainBinding  string `gorm:"not null;default:''"`
}

// The tables saml_login_states and saml_assertion_uses from the SAML era stay
// in existing databases but no longer have a model.

// SSOProtocolOIDC is the protocol of a UserSSOIdentity. Rows with the former
// value "saml" stay in place but have no effect.
const SSOProtocolOIDC = "oidc"

// UserSSOIdentity links an account to an identity at the
// OIDC provider of a status page. A login via page SSO finds the
// account only via (page, issuer, subject), never via the email:
// the IdP of a page is maintained by its admin, an email mapping would
// otherwise allow them to take over foreign accounts. No gorm.Model, so that a deleted
// link does not keep occupying the unique index. Email holds the
// value of the configured email claim (oidc_attribute_email), for display only
// in the profile, never for the mapping.
//
// Protocol and SubjectFormat date from the SAML era and remain part of the
// unique index idx_user_sso_identity_key: old SAML rows (Protocol "saml",
// SubjectFormat = NameID format) stay in place, do not collide with OIDC rows
// and have no effect. New rows always have Protocol "oidc" and an empty
// SubjectFormat. The former index idx_user_sso_identity without protocol is
// removed in database.dropLegacySSOIdentityIndex.
type UserSSOIdentity struct {
	ID            uint   `gorm:"primaryKey"`
	UserID        uint   `gorm:"not null;index"`
	StatusPageID  uint   `gorm:"not null;uniqueIndex:idx_user_sso_identity_key"`
	Protocol      string `gorm:"not null;default:'oidc';uniqueIndex:idx_user_sso_identity_key"`
	Issuer        string `gorm:"not null;uniqueIndex:idx_user_sso_identity_key"`
	Subject       string `gorm:"not null;uniqueIndex:idx_user_sso_identity_key"`
	SubjectFormat string `gorm:"not null;default:'';uniqueIndex:idx_user_sso_identity_key"`
	Email         string `gorm:"not null;default:''"`
	CreatedAt     time.Time
}

// Ensure StatusPageSSOConfig scopes json fields per page config
func (sso *StatusPageSSOConfig) ToPublicResponse() map[string]interface{} {
	return map[string]interface{}{
		"status_page_id":       sso.StatusPageID,
		"oidc_enabled":         sso.OIDCEnabled,
		"oidc_discovery_url":   sso.OIDCDiscoveryURL,
		"oidc_client_id":       sso.OIDCClientID,
		"oidc_scopes":          sso.OIDCScopes,
		"oidc_attribute_email": sso.OIDCAttributeEmail,
		"oidc_attribute_name":  sso.OIDCAttributeName,
		"oidc_attribute_role":  sso.OIDCAttributeRole,
		"oidc_acs_url":         sso.OIDCACSUrl,
		"test_login_enabled":   sso.TestLoginEnabled,
		// The secret itself stays secret; the admin form
		// only needs to know whether one is stored.
		"oidc_client_secret_set": sso.OIDCClientSecret != "",
	}
}

// Component supports arbitrarily deep nesting via ParentID (folder structure)
// If ParentID == nil → top-level group or single component
// If Status == "group" → pure group (no status of its own, derived from children)
type Component struct {
	gorm.Model
	StatusPageID uint `gorm:"not null;index"`
	// Pointer + omitempty, so that the API does not return an empty, never
	// loaded StatusPage object per component.
	StatusPage *StatusPage `gorm:"foreignKey:StatusPageID" json:",omitempty"`

	// Self-reference: nil = root element
	ParentID *uint      `gorm:"index;default:null"`
	Parent   *Component `gorm:"foreignKey:ParentID"`
	// Children are filled by the API handler (not loaded directly in GORM, to avoid N+1)
	Children []Component `gorm:"-"` // filled manually
	// Tags (replacement for the former Country field) - like Children
	// deliberately not a GORM relation but filled manually in the handler
	// (see handlers/tag_access.go, tagsForComponent), to address the existing
	// ComponentTag join table without an implicit many2many tag.
	Tags []Tag `gorm:"-"`

	Name string `gorm:"not null"`
	// Slug: identifier for API and integrations, unique per status page
	// among active rows (partial unique index via raw SQL in
	// database/db.go, deliberately no uniqueIndex tag) and immutable
	// after creation. Empty only for old rows before the backfill.
	Slug        string `gorm:"not null;default:''"`
	Description string
	// Visible: false hides the component (for a group together with its
	// members) on the public status page, in availability, in the
	// subscription wizard and in the public overall status. In the admin
	// it stays visible, webhook and status changes keep working.
	// Default true, so that existing rows stay visible on AutoMigrate (ADD COLUMN ... DEFAULT
	// true). Beware GORM: false is the zero value and is replaced by the default on
	// create; an entry to be created invisible
	// needs an explicit update afterwards (see CreateComponentHandler).
	Visible bool `gorm:"not null;default:true"`
	// ArchivedAt: not nil = archived. Unlike the
	// GORM soft delete (DeletedAt, not retrievable) the component stays, including
	// history, availability, incident assignments and translations,
	// and is restorable. Archived components behave
	// publicly like invisible ones (status page, availability, overall status) and
	// are missing from the selections in the admin; the list shows them only in the
	// "Archived" segment. A group archives its members too (same timestamp).
	ArchivedAt *time.Time `gorm:"index"`
	// Status: "operational","degraded_performance","partial_outage","major_outage","group"
	// "group" = pure folder level, status is aggregated from children
	Status string `gorm:"not null;default:'operational'"`
	// IsGroup marks a group in API responses. Needed
	// because buildTree replaces the status of groups with the worst child status;
	// otherwise the admin would not recognize the group and would turn it into a
	// normal component on save. Not in the DB, set in the handler
	// like Children/Tags.
	IsGroup bool `gorm:"-"`
	// Country was replaced by ComponentTag (Country → Tags
	// step 4/4). The old DB column remains for now (rollback
	// safety net) but is no longer read or written.
	// Sort order within a group
	SortOrder int `gorm:"default:0"`

	// effective status (worst of own status, open
	// incidents and coupled external components) including origin. Not
	// persisted, filled only in the list responses (component tree, dashboard);
	// Status itself stays the stored value, so that nothing is lost when
	// uncoupling and existing clients stay unchanged.
	EffectiveStatus string              `gorm:"-" json:"effective_status,omitempty"`
	ExternalSources []ExternalStatusRef `gorm:"-" json:"external_sources,omitempty"`
}

type Incident struct {
	gorm.Model
	StatusPageID uint `gorm:"not null;index"`
	// Pointer + omitempty like Component.StatusPage
	// - the never-loaded relation would otherwise deliver in every
	// (also public) incident response an empty StatusPage object with
	// the complete internal configuration schema of the page.
	StatusPage *StatusPage `gorm:"foreignKey:StatusPageID" json:",omitempty"`
	Title      string      `gorm:"not null"`
	Message    string      `gorm:"not null"`
	Status     string      `gorm:"not null;default:'investigating'"`
	// Severity: severity of the incident for the weighted
	// availability (degraded_performance | partial_outage | major_outage).
	// Old data and maintenance windows carry the default.
	Severity string `gorm:"not null;default:'degraded_performance'"`
	// Country was replaced by IncidentTag (Country → Tags
	// step 4/4). The old DB column remains for now (rollback
	// safety net) but is no longer read or written.
	// Multi-assignment to components (replaces the former 1:1 FK
	// ComponentID/Component). Deliberately an explicit join model
	// (IncidentComponent) instead of a GORM many2many tag, analogous to
	// UserStatusPageRole - avoids table collisions and can be extended with
	// further fields (SortOrder).
	IncidentComponents []IncidentComponent `gorm:"foreignKey:IncidentID" json:"-"`

	// Maintenance windows: extends Incident instead of being a separate
	// model, so that the incident's timeline/notifications/component assignment
	// can be reused directly. Status lifecycle for maintenance windows:
	// "scheduled" -> "in_progress" -> "resolved" (deliberately the same terminal
	// status as for incidents, so that the existing "active incidents" filter
	// (models.IsResolvedIncidentStatus: resolved) on the public status page keeps
	// working unchanged). Advanced automatically by the scheduler in
	// backend/scheduler/maintenance.go.
	IsMaintenance       bool       `gorm:"not null;default:false"`
	ScheduledStartAt    *time.Time `gorm:""`
	ScheduledEndAt      *time.Time `gorm:""`
	ReminderLeadMinutes int        `gorm:"not null;default:0"` // 0 = no reminder
	ReminderSentAt      *time.Time `gorm:""`

	// Event times of normal incidents: optional start/end,
	// so that history can be filled in retroactively. CreatedAt remains the technical
	// timestamp; for maintenance windows ScheduledStartAt/ScheduledEndAt apply.
	StartedAt  *time.Time `gorm:""`
	ResolvedAt *time.Time `gorm:""`
}

// IncidentComponent: explicit join table incident <-> component (many-to-many).
// Replaces the former 1:1 FK Incident.ComponentID/Component.
type IncidentComponent struct {
	gorm.Model
	IncidentID  uint      `gorm:"not null;index"`
	Incident    Incident  `gorm:"foreignKey:IncidentID;constraint:OnDelete:CASCADE"`
	ComponentID uint      `gorm:"not null;index"`
	Component   Component `gorm:"foreignKey:ComponentID"`
	SortOrder   int       `gorm:"default:0"`
}

// Tag is the new, freely definable counterpart to the previous
// country strings (step 1/4 - Country → Tags). Purely
// additive: initially runs only in parallel to Component.Country, Incident.Country
// and User.AllowedCountries, without the existing access check
// (UserHasAccessToCountry) making use of it - that follows only in step
// 2/4.
//
// StatusPageID == nil marks a global (repo-/instance-wide) tag,
// otherwise the tag is bound to exactly one status page. Since Postgres
// treats NULL values in a normal unique index as pairwise distinct,
// a unique index on (Name, StatusPageID) is NOT enough to prevent
// duplicate global tags - see the two partial
// unique indexes idx_tags_global_name_active / idx_tags_scoped_name_active (active rows only), which
// CreateSoftDeleteUniqueIndexes() in backend/database/db.go creates via raw SQL
// (GORM AutoMigrate does not create partial indexes).
type Tag struct {
	gorm.Model
	Name         string      `gorm:"not null"`
	StatusPageID *uint       `gorm:"index"`
	StatusPage   *StatusPage `gorm:"foreignKey:StatusPageID"`
}

// ComponentTag: explicit join table component <-> tag (many-to-many),
// analogous to IncidentComponent - deliberately no GORM many2many tag.
type ComponentTag struct {
	gorm.Model
	ComponentID uint      `gorm:"not null;index"`
	Component   Component `gorm:"foreignKey:ComponentID;constraint:OnDelete:CASCADE"`
	TagID       uint      `gorm:"not null;index"`
	Tag         Tag       `gorm:"foreignKey:TagID;constraint:OnDelete:CASCADE"`
}

// IncidentTag: explicit join table incident <-> tag (many-to-many),
// analogous to IncidentComponent - deliberately no GORM many2many tag.
type IncidentTag struct {
	gorm.Model
	IncidentID uint     `gorm:"not null;index"`
	Incident   Incident `gorm:"foreignKey:IncidentID;constraint:OnDelete:CASCADE"`
	TagID      uint     `gorm:"not null;index"`
	Tag        Tag      `gorm:"foreignKey:TagID;constraint:OnDelete:CASCADE"`
}

// UserTag: explicit join table user <-> tag (many-to-many), analogous to
// UserStatusPageRole - deliberately no GORM many2many tag.
type UserTag struct {
	gorm.Model
	UserID uint `gorm:"not null;index"`
	User   User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	TagID  uint `gorm:"not null;index"`
	Tag    Tag  `gorm:"foreignKey:TagID;constraint:OnDelete:CASCADE"`
}

// GeneralNotice: site-wide announcements not tied to a
// specific incident/maintenance window (e.g. policy
// changes, organizational notices). Deliberately a separate, lean
// model instead of an extension of Incident (unlike for maintenance
// windows): notices have no status lifecycle and no
// component assignment, only a visibility switch (IsActive)
// and an optional time window (StartAt/EndAt).
type GeneralNotice struct {
	gorm.Model
	StatusPageID uint `gorm:"not null;index"`
	// see Incident.StatusPage.
	StatusPage *StatusPage `gorm:"foreignKey:StatusPageID" json:",omitempty"`
	Title      string      `gorm:"not null"`
	Message    string      `gorm:"not null"` // Markdown-capable, stored as a plain string
	// Level: "info" | "warning" | "critical"
	Level string `gorm:"not null;default:'info'"`
	// IsActive: deliberately NO gorm "default:true" - GORM treats bool
	// zero values (false) with "default" tags as "not set" and would reset them
	// silently to the DB default on insert, so that an explicitly inactive
	// notice could never be created. The default "active" is enforced
	// on the application side in CreateNoticeHandler instead.
	IsActive bool `gorm:"not null"`
	// Collapsed: reduces space on the status page - the notice appears
	// behind a collapsible header instead of being written out directly.
	Collapsed bool       `gorm:"not null;default:false"`
	StartAt   *time.Time `gorm:""`
	EndAt     *time.Time `gorm:""`

	// NotifySubscribers: controls whether creating/activating
	// this notice triggers a mail notification to all matching subscribers.
	// Deliberately opt-in (default false) - purely cosmetic notices
	// (e.g. "we have a new logo") should not spam anyone
	// unasked. Existing notices keep the previous behavior
	// (no mail sending), since AutoMigrate sets the new column to false.
	NotifySubscribers bool `gorm:"not null;default:false"`
}

// IncidentTranslation (Stage 2): translations of incident title/message
// with draft/confirmed status. Only confirmed ones are shown on the public page.
type IncidentTranslation struct {
	gorm.Model
	IncidentID   uint     `gorm:"not null"`
	Incident     Incident `gorm:"foreignKey:IncidentID;constraint:OnDelete:CASCADE"`
	LanguageCode string   `gorm:"not null"`
	Title        string   `gorm:"not null"`
	Message      string   `gorm:"not null"`
	Status       string   `gorm:"not null;default:'draft'"`
	Source       string   `gorm:"not null;default:'manual'"`
}

// ComponentTranslation (Stage 3): translations of component name/description
// with draft/confirmed status. Only confirmed ones are shown on the public page.
type ComponentTranslation struct {
	gorm.Model
	ComponentID  uint      `gorm:"not null"`
	Component    Component `gorm:"foreignKey:ComponentID;constraint:OnDelete:CASCADE"`
	LanguageCode string    `gorm:"not null"`
	Title        string    `gorm:"not null"`
	Description  string    `gorm:"not null"`
	Status       string    `gorm:"not null;default:'draft'"`
	Source       string    `gorm:"not null;default:'manual'"`
}

// GeneralNoticeTranslation (Stage 4): translations of notice title/message
// with draft/confirmed status. Only confirmed ones are shown on the public page.
type GeneralNoticeTranslation struct {
	gorm.Model
	GeneralNoticeID uint          `gorm:"not null"`
	GeneralNotice   GeneralNotice `gorm:"foreignKey:GeneralNoticeID;constraint:OnDelete:CASCADE"`
	LanguageCode    string        `gorm:"not null"`
	Title           string        `gorm:"not null"`
	Message         string        `gorm:"not null"`
	Status          string        `gorm:"not null;default:'draft'"`
	Source          string        `gorm:"not null;default:'manual'"`
}

// IncidentTemplate: template for recurring incidents/
// maintenance windows, so that title/message/status/components do not have to be
// retyped every time. Deliberately NO FK relation from Incident back
// to the template - on creation the field values are only copied
// (see CreateIncidentHandler/TemplateID handling), so that deleting a
// template never affects already created incidents/maintenance windows.
type IncidentTemplate struct {
	gorm.Model
	StatusPageID  uint       `gorm:"not null;index"`
	StatusPage    StatusPage `gorm:"foreignKey:StatusPageID"`
	Name          string     `gorm:"not null"`                    // internal template name, e.g. "DB-Wartung"
	Kind          string     `gorm:"not null;default:'incident'"` // "incident" | "maintenance"
	Title         string     `gorm:"not null"`
	Message       string     `gorm:"not null"`
	DefaultStatus string     `gorm:""`
	// ComponentIDs: preselected components, comma-separated (analogous to
	// Subscriber.SubscribedComponentIDs) instead of a separate join table, since
	// no referential integrity is needed here - the IDs are read out only once as
	// prefill when creating an incident.
	ComponentIDs string `gorm:""`
}

// IncidentTemplateTranslation: translation of the title and message
// of a template into a further language of the page. The default language is the
// template itself. On "use template" the texts are also
// carried over into the form. Status as with the other translations: draft | confirmed.
type IncidentTemplateTranslation struct {
	gorm.Model
	IncidentTemplateID uint             `gorm:"not null"`
	IncidentTemplate   IncidentTemplate `gorm:"foreignKey:IncidentTemplateID;constraint:OnDelete:CASCADE"`
	LanguageCode       string           `gorm:"not null"`
	Title              string           `gorm:"not null"`
	Message            string           `gorm:"not null"`
	Status             string           `gorm:"not null;default:'draft'"`
}

// NotificationTemplate: template, storable per status page and notification type,
// for the subject and HTML body of notification mails,
// rendered via Go html/template. If an entry is missing for a (StatusPageID, Kind,
// Audience, Language) tuple, the hard-coded
// default renderer (buildNotificationEmailHTML) applies - existing behavior
// thus stays unchanged as long as no admin stores their own template.
//
// Kind identifies the occasion (incident/maintenance-window/notice
// notifications, see the NotificationKind* constants in
// handlers/notification_template.go).
//
// Audience (stage 5 of multilingual support): "staff" (internal
// users) | "subscriber" (public subscribers). Previously this
// distinction was implicit in the Language field itself (Language=en meant
// "internal copy", Language=de meant "subscriber copy" - see
// handlers/incident.go buildNotificationDispatchContext) and thereby collided
// with every further real language that happened not to be called
// "en"/"de". With Audience as its own field, Language is now a real
// locale code validated against the language catalog (models.Language/StatusPageLanguage)
// instead of a disguised audience indication.
//
// (StatusPageID, Kind, Audience, Language) is unique among active rows
// (partial index idx_notification_template_active in
// database.CreateSoftDeleteUniqueIndexes, no gorm tag) - a tenant has at most
// one custom template per combination.
type NotificationTemplate struct {
	gorm.Model
	StatusPageID uint       `gorm:"not null"`
	StatusPage   StatusPage `gorm:"foreignKey:StatusPageID"`
	Kind         string     `gorm:"not null"`
	Audience     string     `gorm:"not null;default:'staff'"`
	Language     string     `gorm:"not null"`
	Subject      string     `gorm:"not null"`
	BodyHTML     string     `gorm:"not null"`
}

type Subscriber struct {
	gorm.Model
	StatusPageID           uint       `gorm:"not null;index"`
	StatusPage             StatusPage `gorm:"foreignKey:StatusPageID"`
	Email                  string     `gorm:"default:null"`             // empty for Google Chat
	WebhookURL             string     `gorm:"default:null"`             // empty for email
	Channel                string     `gorm:"not null;default:'email'"` // "email" | "google_chat"
	SubscribedComponentIDs string     `gorm:"not null;default:'all'"`
	// Language: desired language of the notification mails, a
	// language code active for the page. Empty = default language of the status page.
	Language string `gorm:"not null;default:''"`
	// CreatedByUserID links the subscription to the creating user (if signed in),
	// so that they can find and edit it later via /me/subscription
	// – previously there was only the fragile email string matching, which
	// came to nothing entirely for admin-created webhook subscriptions (without email).
	CreatedByUserID *uint `gorm:"default:null;index"`
	CreatedBy       *User `gorm:"foreignKey:CreatedByUserID"`

	// Double opt-in: status "pending" = email address not yet
	// confirmed via link, "active" = confirmed or valid without confirmation
	// (own account address, webhook channels). Notifications go only to
	// "active". The DB default "active" ensures that AutoMigrate adopts all
	// existing subscriptions as confirmed when creating the column (additionally
	// secured via database.backfillSubscriberStatus).
	Status string `gorm:"not null;default:'active';index"`
	// ConfirmTokenHash: SHA-256 (hex) of the confirmation token. The plaintext
	// is only in the mail link, so that a DB leak cannot confirm any open subscriptions.
	// Empty after confirmation, like PageOrder.ConfirmToken.
	ConfirmTokenHash   string     `gorm:"index;default:null" json:"-"`
	ConfirmTokenExpiry *time.Time `gorm:"default:null" json:"-"`
}

// Subscriber status values.
const (
	SubscriberStatusPending = "pending"
	SubscriberStatusActive  = "active"
)
