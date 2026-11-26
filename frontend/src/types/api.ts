// Shared shapes of the backend's JSON responses (backend/models and the handler DTOs).
//
// GORM models without json tags are serialized with their Go field names (ID, Name, StatusPageID, ...);
// gorm.Model contributes ID, CreatedAt, UpdatedAt and DeletedAt. Timestamps arrive as ISO strings.
// Fields that only some endpoints send are optional. View-specific DTOs live next to their view or in a
// further file under src/types/.

/** ISO 8601 timestamp as sent by the backend (time.Time / *time.Time). */
export type IsoDateString = string

/** Fields every GORM model carries (gorm.Model). */
export interface GormModel {
  ID: number
  CreatedAt?: IsoDateString
  UpdatedAt?: IsoDateString
  DeletedAt?: IsoDateString | null
}

/** Error thrown by apiFetch (useApi): an Error with the HTTP context attached. */
export interface ApiError extends Error {
  status?: number
  data?: ApiErrorBody
  forbidden?: boolean
  sessionExpired?: boolean
  basicAuth?: boolean
  network?: boolean
}

/** Error body of the backend: a fallback text plus a stable code (translated under api.errors.<code>). */
export interface ApiErrorBody {
  error?: string
  code?: string
  retry_after?: number
  count?: number
  pages?: string[]
  [key: string]: unknown
}

export type HeaderStyle = 'classic' | 'hero' | string
export type PageLayout = 'standard' | string

export interface StatusPage extends GormModel {
  Name: string
  Slug: string
  IsSystem?: boolean
  Description?: string
  LogoURL?: string
  CustomDomain?: string
  IsPublicIndexed?: boolean
  IsPrivate?: boolean
  DefaultRoleID?: number | null
  AutoJoinEmailDomains?: string
  PrimaryColor?: string
  SecondaryColor?: string
  HeaderStyle?: HeaderStyle
  PageLayout?: PageLayout
  CustomDomainStatus?: string
  CustomDomainVerifiedAt?: IsoDateString | null
  CustomDomainCheckedAt?: IsoDateString | null
  CustomDomainCheckError?: string
  NotificationFromAddress?: string
}

export interface Role extends GormModel {
  Name: string
  /** Comma-separated permission strings, e.g. "components:read,incidents:create". */
  Permissions: string
}

export interface UserStatusPageRole extends GormModel {
  UserID: number
  StatusPageID: number
  StatusPage?: StatusPage
  RoleID: number
  Role?: Role
  JoinedVia?: 'assigned' | 'auto_join' | 'legacy' | string
}

export interface User extends GormModel {
  Email: string
  RoleID?: number
  Role?: Role
  SSOProvider?: string
  SSOSubject?: string
  IsGlobalAdmin?: boolean
  PreferredLanguage?: string
  HideHelpTooltips?: boolean
  UserStatusPageRoles?: UserStatusPageRole[]
}

export interface Tag extends GormModel {
  Name: string
  StatusPageID?: number | null
}

/** Reference from a component to an external status source (externalstatus). */
export interface ExternalStatusRef {
  [key: string]: unknown
}

export interface Component extends GormModel {
  StatusPageID: number
  StatusPage?: StatusPage
  ParentID?: number | null
  Children?: Component[]
  Tags?: Tag[]
  Name: string
  Slug?: string
  Description?: string
  Visible?: boolean
  ArchivedAt?: IsoDateString | null
  Status: string
  IsGroup?: boolean
  SortOrder?: number
  effective_status?: string
  external_sources?: ExternalStatusRef[]
}

export interface Incident extends GormModel {
  StatusPageID: number
  StatusPage?: StatusPage
  Title: string
  Message: string
  Status: string
  Severity?: string
  IsMaintenance?: boolean
  ScheduledStartAt?: IsoDateString | null
  ScheduledEndAt?: IsoDateString | null
  ReminderLeadMinutes?: number
  ReminderSentAt?: IsoDateString | null
  StartedAt?: IsoDateString | null
  ResolvedAt?: IsoDateString | null
}

export interface GeneralNotice extends GormModel {
  StatusPageID: number
  Title: string
  Message: string
  Level: 'info' | 'warning' | 'critical' | string
  IsActive: boolean
  Collapsed?: boolean
  StartAt?: IsoDateString | null
  EndAt?: IsoDateString | null
  NotifySubscribers?: boolean
}

/** Status of a machine or manual translation. */
export type TranslationStatus = 'draft' | 'published' | string

export interface IncidentTemplate extends GormModel {
  StatusPageID: number
  Name: string
  Kind: 'incident' | 'maintenance' | string
  Title: string
  Message: string
  DefaultStatus?: string
  /** Comma-separated component IDs. */
  ComponentIDs?: string
}

export interface NotificationTemplate extends GormModel {
  StatusPageID: number
  Kind: string
  Audience: 'staff' | 'subscriber' | string
  Language: string
  Subject: string
  BodyHTML: string
}

export interface Subscriber extends GormModel {
  StatusPageID: number
  Email?: string
  WebhookURL?: string
  Channel: 'email' | 'google_chat' | string
  /** "all" or comma-separated component IDs. */
  SubscribedComponentIDs: string
  Language?: string
  CreatedByUserID?: number | null
  CreatedBy?: User | null
  Status?: 'pending' | 'active' | string
}

/** A content language of the installation (models.Language). */
export interface ContentLanguage {
  code: string
  name?: string
  /** Set in the language list of a public status page: the page's default language. */
  is_default?: boolean
  [key: string]: unknown
}

/** One UTC day of GET /api/public/pages/{slug}/availability (AvailabilityDay in openapi.yaml). */
export interface AvailabilityDay {
  /** UTC day, YYYY-MM-DD. */
  date: string
  /** Worst status of the day; `no_data` = nothing observed. */
  status: string
  /** Percent of the observed time without an incident; null for `no_data`. */
  availability: number | null
  downtime_seconds: number
}

/** Per-component entry of the availability response (only with `?include=components`, no groups). */
export interface ComponentAvailability {
  id: number
  name: string
  uptime_percent: number | null
  days: AvailabilityDay[]
}

/** Response of GET /api/public/pages/{slug}/availability (Availability in openapi.yaml). */
export interface PageAvailability {
  generated_at: IsoDateString
  from: string
  to: string
  uptime_percent: number | null
  history_truncated?: boolean
  days: AvailabilityDay[]
  components?: ComponentAvailability[]
}

/** Profile of the logged-in user (login response, GET /api/me, or the claims of an SSO token). */
export interface AuthUser {
  id?: number
  email?: string
  role?: string
  /** Comma-separated permissions of the global role (never evaluated, see useAuth.hasPerm). */
  permissions?: string
  tag_ids?: number[]
  is_global_admin?: boolean
  preferred_language?: string
  help_tooltips?: boolean
  sso_provider?: string
  name?: string
}

/** Entry of GET /api/me/pages: a status page plus the user's role on it. */
export interface MyPage extends StatusPage {
  /** Comma-separated permissions of the page role (UserStatusPageRole). */
  Permissions: string
  RoleName?: string
}

/** Entry of the language catalog (models.Language, GET /api/languages). */
export interface Language extends GormModel {
  Code: string
  Name: string
  IsActive?: boolean
}

/** Language activated for a status page (models.StatusPageLanguage, GET /api/pages/:id/languages). */
export interface PageLanguage extends GormModel {
  StatusPageID?: number
  LanguageCode: string
  Language?: Language
  IsDefault: boolean
}

/**
 * Translation row of an incident, component, notice or template (IncidentTranslation & co.).
 * The content field is Message or Description depending on the entity.
 */
export interface EntityTranslation extends GormModel {
  LanguageCode: string
  Title?: string
  Message?: string
  Description?: string
  /** 'draft' | 'confirmed' */
  Status?: string
  Source?: string
  [key: string]: unknown
}

/** Response of POST /api/pages/:id/translate. */
export interface TranslateResponse {
  title: string
  message: string
  provider?: string
  source_language?: string
  fallback?: boolean
}

/** Settings of the automatic translation (GET/PUT /api/settings/translation); the key itself is never sent. */
export interface TranslationSettings {
  enabled: boolean
  provider: string
  url: string
  api_key_set: boolean
  env_url_set: boolean
  env_api_key_set: boolean
  effective_provider: string
  configured: boolean
}

/** Global system settings (GET/PUT /api/settings). */
export interface SystemSettings {
  orders_enabled: boolean
  email_enabled: boolean
  selfmonitor_enabled: boolean
}

/** Message of the admin notification center (adminNotificationItem). */
export interface AdminNotification {
  id: number
  created_at?: IsoDateString
  updated_at?: IsoDateString
  status_page_id?: number | null
  kind: string
  severity: 'info' | 'warning' | 'error' | string
  title_key: string
  body_key: string
  link_path: string
  count: number
  first_seen_at?: IsoDateString
  last_seen_at: IsoDateString
  resolved_at?: IsoDateString | null
  params?: Record<string, unknown>
  read: boolean
}

/** Status page the user is the sole admin of (409 sole_admin_pages of DELETE /api/me, handlers/me_delete.go). */
export interface SoleAdminPage {
  id: number
  name: string
  slug: string
}

/** Duration of one severity within an availability day (availabilityImpairment, handlers/availability.go). */
export interface AvailabilityImpairment {
  status: string
  seconds: number
}

/** Fields of a component's link to an external status source (models.ExternalStatusRef). */
export interface ExternalComponentRef extends ExternalStatusRef {
  external_component_id: number
  source_id: number
  source_name: string
  name: string
  group_name?: string
  status: string
  url: string
  vanished: boolean
  /** The link points to a whole external group; `status` is then the worst of its members. */
  is_group: boolean
  /** Member of a linked group that yields its worst status. */
  worst?: { name: string, status: string } | null
  /** The source is down: `status` is the last known one and no longer counts. */
  source_outage: boolean
}

/** Update of a mirrored external incident (externalIncidentUpdateView). */
export interface ExternalIncidentUpdate {
  id: string
  status: string
  body: string
  display_at: IsoDateString
}

/** Mirrored incident or maintenance of an external source (externalIncidentView, dashboard field external_incidents). */
export interface ExternalIncident {
  id: number
  source_id: number
  source_name: string
  source_url: string
  kind: 'incident' | 'maintenance' | string
  external_id: string
  title: string
  status: string
  raw_status: string
  impact: string
  closed: boolean
  url: string
  started_at: IsoDateString | null
  updated_at: IsoDateString | null
  resolved_at: IsoDateString | null
  scheduled_for: IsoDateString | null
  scheduled_until: IsoDateString | null
  affected_components: { id: number, name: string }[] | null
  external_components?: unknown[] | null
  updates: ExternalIncidentUpdate[] | null
}

/** Incident with its affected components and tags (IncidentResponse, handlers/incident.go). */
export interface IncidentResponse extends Incident {
  Components?: Component[]
  Tags?: Tag[]
}

/** Timeline entry of an incident (incidentUpdateView); author_email only for editors. */
export interface IncidentUpdateView {
  id: number
  created_at: IsoDateString
  incident_id?: number
  status: string
  message: string
  author_email?: string
}

/** Response of GET /api/pages/:page_id/incidents/:id (incidentDetail). */
export interface IncidentDetail extends IncidentResponse {
  updates: IncidentUpdateView[] | null
  post_mortem?: unknown
}

/** Response of GET /api/public/pages/:slug/dashboard (writePublicDashboard, handlers/statuspage.go). */
export interface PublicDashboard {
  page: StatusPage
  languages: ContentLanguage[]
  /** Component tree with tags; Status already overlaid with open incidents. */
  components: Component[]
  incidents: IncidentResponse[]
  incidents_more?: boolean
  notices: GeneralNotice[] | null
  external_incidents: ExternalIncident[] | null
}
