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
  [key: string]: unknown
}
