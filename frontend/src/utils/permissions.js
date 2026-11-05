// Rechtemodell je Tab der Admin-Navigation. Ein Recht ist
// `<ressource>:<aktion>` mit den Aktionen create, read, update, delete.
// Spiegel von backend/permissions/permissions.go (ein Test vergleicht beide
// Ressourcenlisten, backend/permissions/permissions.go bleibt die Quelle).

export const ACTIONS = ['create', 'read', 'update', 'delete']

// Altwert aus der Zeit vor Alt-Rollen, Alt-Token: zählt beim
// Prüfen als create+read+update+delete derselben Ressource, wird aber nie
// mehr vergeben.
const LEGACY_WRITE = 'write'

export const ADMIN_DASHBOARD = 'admin_dashboard'

// Bereiche der Navigation (Reihenfolge wie in der Oberfläche).
export const GROUPS = ['operations', 'content', 'management', 'system']

// scope 'global': ohne Seitenbezug, nur Global Admins (Backend: RequireGlobalAdmin).
export const RESOURCES = [
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

export const perm = (resource, action) => `${resource}:${action}`

export const isGlobalResource = (resource) =>
  RESOURCES.some(r => r.key === resource && r.scope === 'global')

const entries = (list) => (Array.isArray(list) ? list : String(list || '').split(','))
  .map(s => s.trim())
  .filter(Boolean)

// Gewährt die Liste das verlangte Recht? Das Recht selbst, bei read zusätzlich
// create/update/delete derselben Ressource, und der Altwert `:write`.
export function grants(list, required) {
  const [resource, action] = required.split(':')
  const granted = entries(list)
  // Sonderrechte ohne Aktion (admin_dashboard) gelten nur exakt.
  if (!action) return granted.includes(required)
  return granted.some(p => p === required
    || p === perm(resource, LEGACY_WRITE)
    || (action === 'read' && ['create', 'update', 'delete'].some(a => p === perm(resource, a))))
}

// Stellt zu create/update/delete das Recht read sicher (wie Backend `Normalize`
// für die Matrix); `:write` wird zu den vier Aktionen.
export function normalizePermissions(list) {
  const out = []
  const add = (p) => { if (!out.includes(p)) out.push(p) }
  for (const p of entries(list)) {
    const [resource, action] = p.split(':')
    if (action === LEGACY_WRITE) ACTIONS.forEach(a => add(perm(resource, a)))
    else add(p)
  }
  for (const p of [...out]) {
    const [resource, action] = p.split(':')
    if (['create', 'update', 'delete'].includes(action)) add(perm(resource, 'read'))
  }
  return out
}
