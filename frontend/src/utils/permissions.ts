// Permission model per tab of the admin navigation. A permission is
// `<resource>:<action>` with the actions create, read, update, delete.
// Mirror of backend/permissions/permissions.go (a test compares both
// resource lists; backend/permissions/permissions.go stays the source).

export type PermissionAction = 'create' | 'read' | 'update' | 'delete'
export type ResourceScope = 'global' | 'page'

export interface PermissionResource {
  key: string
  group: string
  scope: ResourceScope
}

/** A permission list: an array or the comma-separated string the backend stores (Role.Permissions). */
export type PermissionList = readonly string[] | string | null | undefined

export const ACTIONS: PermissionAction[] = ['create', 'read', 'update', 'delete']

// Legacy value from before legacy roles and legacy tokens: counts when
// checking as create+read+update+delete of the same resource, but is never
// granted any more.
const LEGACY_WRITE = 'write'

export const ADMIN_DASHBOARD = 'admin_dashboard'

// Areas of the navigation (order as in the UI).
export const GROUPS: string[] = ['operations', 'content', 'management', 'system']

// scope 'global': not tied to a page, global admins only (backend: RequireGlobalAdmin).
export const RESOURCES: PermissionResource[] = [
  { key: 'overview', group: 'operations', scope: 'global' },
  { key: 'components', group: 'operations', scope: 'page' },
  { key: 'external_sources', group: 'operations', scope: 'page' },
  { key: 'incidents', group: 'operations', scope: 'page' },
  { key: 'maintenance', group: 'operations', scope: 'page' },
  { key: 'notices', group: 'operations', scope: 'page' },
  { key: 'incident_templates', group: 'operations', scope: 'page' },
  { key: 'subscribers', group: 'operations', scope: 'page' },
  { key: 'notification_templates', group: 'content', scope: 'page' },
  { key: 'languages', group: 'content', scope: 'page' },
  { key: 'ui_translations', group: 'content', scope: 'page' },
  { key: 'pages', group: 'management', scope: 'page' },
  { key: 'users', group: 'management', scope: 'global' },
  { key: 'roles', group: 'management', scope: 'global' },
  { key: 'tags', group: 'management', scope: 'global' },
  { key: 'audit', group: 'management', scope: 'global' },
  { key: 'monitoring', group: 'system', scope: 'global' },
  { key: 'sso', group: 'system', scope: 'page' },
  { key: 'system_settings', group: 'system', scope: 'global' },
]

export const perm = (resource: string | undefined, action: string): string => `${resource}:${action}`

export const isGlobalResource = (resource: string): boolean =>
  RESOURCES.some(r => r.key === resource && r.scope === 'global')

const entries = (list: PermissionList): string[] => (Array.isArray(list) ? list as readonly string[] : String(list || '').split(','))
  .map(s => s.trim())
  .filter(Boolean)

// Does the list grant the required permission? The permission itself, for read also
// create/update/delete of the same resource, and the legacy value `:write`.
export function grants(list: PermissionList, required: string): boolean {
  const [resource, action] = required.split(':')
  const granted = entries(list)
  // Special permissions without an action (admin_dashboard) only apply exactly.
  if (!action) return granted.includes(required)
  return granted.some(p => p === required
    || p === perm(resource, LEGACY_WRITE)
    || (action === 'read' && ['create', 'update', 'delete'].some(a => p === perm(resource, a))))
}

// Mirror of backend permissions.GrantsAdminArea: any action (or the legacy
// write) on a known resource opens the admin area. Without one the role is
// "status page only": view and subscribe to the (private) page, no admin area.
// admin_dashboard alone does not count.
export function grantsAdminArea(list: PermissionList): boolean {
  return entries(list).some((p) => {
    const [resource, action] = p.split(':')
    return RESOURCES.some(r => r.key === resource) && (ACTIONS.includes(action as PermissionAction) || action === LEGACY_WRITE)
  })
}

// Ensures the read permission alongside create/update/delete (like backend `Normalize`
// for the matrix); `:write` becomes the four actions.
export function normalizePermissions(list: PermissionList): string[] {
  const out: string[] = []
  const add = (p: string) => { if (!out.includes(p)) out.push(p) }
  for (const p of entries(list)) {
    const [resource, action] = p.split(':')
    if (action === LEGACY_WRITE) ACTIONS.forEach(a => add(perm(resource, a)))
    else add(p)
  }
  for (const p of [...out]) {
    const [resource, action] = p.split(':')
    if (['create', 'update', 'delete'].includes(action as string)) add(perm(resource, 'read'))
  }
  return out
}
