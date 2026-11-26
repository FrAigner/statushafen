// Response and form shapes used only by the admin views (src/views/admin). Shared backend models live
// in api.ts; these are handler DTOs and view-local form states that more than one admin view uses or
// that are large enough to deserve a name of their own.

import type { IncidentResponse } from './api'

/** One status page row of GET /api/admin/overview (handlers/overview.go). */
export interface OverviewPage {
  id: number
  name: string
  slug: string
  status: string
  components: number
  subscribers: number
  open_incidents: number
}

/** GET /api/admin/overview: totals across all status pages plus one row per page. */
export interface OverviewResponse {
  pages: OverviewPage[]
  total_pages: number
  total_open_incidents: number
  total_subscribers: number
  worst_status: string
}

/** One entry of GET /api/admin/audit (handlers/audit.go); created_at is a Unix timestamp in seconds. */
export interface AuditEntry {
  id: number
  created_at: number
  actor_email?: string
  actor_role?: string
  action: string
  category: string
  target_name?: string
  ip?: string
}

/** GET /api/admin/audit: one page of entries plus the total count. */
export interface AuditPage {
  items?: AuditEntry[]
  total?: number
}

/** Golden signals of one route or of all routes together (handlers/monitoring.go). */
export interface MonitoringTotals {
  request_count: number
  error_count: number
  error_rate: number
  requests_per_minute: number
  avg_latency_ms: number
  p50_latency_ms: number
  p95_latency_ms: number
  max_latency_ms: number
}

export interface MonitoringRoute extends MonitoringTotals {
  method: string
  route: string
}

/** Resource usage of the backend process (golden signals step 4/4). */
export interface MonitoringSaturation {
  goroutine_count: number
  heap_alloc_bytes: number
  heap_sys_bytes: number
  sys_bytes: number
  num_gc: number
  db_stats_available: boolean
  db_max_open_connections: number
  db_open_connections: number
  db_in_use: number
  db_idle: number
}

/** GET /api/admin/monitoring. */
export interface MonitoringResponse {
  routes?: MonitoringRoute[]
  totals?: MonitoringTotals
  uptime_seconds?: number
  grafana_dashboard_url?: string
  saturation?: MonitoringSaturation
}

/** OIDC form of the page SSO settings (/api/admin/pages/:page_id/sso). */
export interface SsoForm {
  oidc_enabled: boolean
  oidc_discovery_url: string
  oidc_client_id: string
  oidc_client_secret: string
  oidc_scopes: string
  oidc_attribute_email: string
  oidc_attribute_name: string
  test_login_enabled: boolean
}

/** GET/PUT /api/admin/pages/:page_id/sso: the form fields plus read-only info (never the secret itself). */
export interface SsoConfig extends SsoForm {
  oidc_acs_url: string
  oidc_client_secret_set: boolean
}

/** Follow-up action of a post-mortem. */
export interface PostMortemAction {
  id?: number
  text: string
  owner: string
  state: string
}

/** The free-text fields of a post-mortem besides the summary. */
export type PostMortemTextField = 'root_cause' | 'impact' | 'resolution' | 'detection'

/** Editable part of a post-mortem (the PUT body without `publish`). */
export interface PostMortemForm extends Record<PostMortemTextField, string> {
  summary: string
  actions: PostMortemAction[]
}

export interface PostMortem extends PostMortemForm {
  published_at?: string | null
}

/**
 * Edit form of an incident or maintenance window: component IDs as strings for the multi-select,
 * tag IDs for the tag field, times as datetime-local input values.
 */
export interface IncidentForm extends Partial<Omit<IncidentResponse, 'Components' | 'Tags'>> {
  ComponentIDs?: string[]
  TagIDs?: number[]
}

/** An external status page that is polled (GET /api/pages/:page_id/external-sources). */
export interface ExternalSource {
  ID: number
  name: string
  base_url: string
  poll_interval_seconds: number
  outage_after_minutes: number
  active: boolean
  outage?: { active: boolean, since?: string } | null
  last_fetch_ok?: boolean | null
  last_fetch_at?: string | null
  last_success_at?: string | null
  last_error?: string
  component_count?: number
  link_count?: number
}

/** Form state for creating or editing an external source. */
export interface ExternalSourceForm {
  name?: string
  base_url?: string
  poll_interval_seconds?: number | string
  outage_after_minutes?: number | string
  active?: boolean
}

/** A component or group of an external source (GET .../external-sources/:id/components). */
export interface ExternalSourceComponent {
  ID: number
  external_id: string
  name: string
  group_name?: string
  group_external_id?: string
  is_group: boolean
  linked_component_ids?: number[] | null
}

/** Link of an own component to an external one (GET /api/pages/:page_id/external-links). */
export interface ExternalLink {
  id: number
  component_name: string
  external_component_name: string
  source_name: string
  status: string
  group_name?: string
  is_group?: boolean
  vanished?: boolean
  warnings: string[]
  worst?: { name: string, status: string } | null
}

/** POST .../external-sources/:id/import: the created components and the skipped external ones. */
export interface ExternalImportResponse {
  created?: unknown[] | null
  skipped?: { external_component_id: number, reason: string }[] | null
}

/** Open invitation of a status page (GET /api/admin/pages/:id/invitations). */
export interface PageInvitation {
  ID: number
  Email: string
  RoleName?: string
  ExpiresAt?: string
}

/** Response of the custom domain endpoints (PUT .../custom-domain, POST .../custom-domain/verify). */
export interface CustomDomainResponse {
  custom_domain_status: string
  cname_target: string
  txt_record_value?: string
  custom_domain_checked_at?: string | null
  custom_domain_check_error?: string
  message?: string
  verified_at?: string | null
}
