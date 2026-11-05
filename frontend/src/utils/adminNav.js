// Navigation des Admin-Bereichs als Daten. Jeder Tab ist eine
// Ressource des Rechtemodells (utils/permissions.js); Layout, Router-Metadaten,
// Startseite nach dem Login und die Rollen-Matrix lesen alle von hier, damit
// Tabs, Rechte und Beschriftungen nicht auseinanderlaufen.
//
// `resource: null` = kein Recht nötig (Doku). `labelKey` zeigt auf
// adminNav.items.<key> in den Sprachdateien.

export const NAV_GROUPS = [
  {
    key: 'operations',
    items: [
      { resource: 'overview', to: '/admin/overview', icon: 'chart-bar', labelKey: 'overview' },
      { resource: 'components', to: '/admin/components', icon: 'grid', labelKey: 'components' },
      { resource: 'external_sources', to: '/admin/external-sources', icon: 'globe', labelKey: 'externalSources' },
      { resource: 'incidents', to: '/admin/incidents', icon: 'alert-triangle', labelKey: 'incidents' },
      { resource: 'maintenance', to: '/admin/maintenance', icon: 'clock', labelKey: 'maintenance' },
      { resource: 'notices', to: '/admin/notices', icon: 'alert-circle', labelKey: 'notices' },
      { resource: 'incident_templates', to: '/admin/incident-templates', icon: 'clipboard', labelKey: 'incidentTemplates' },
      { resource: 'subscribers', to: '/admin/subscribers', icon: 'bell', labelKey: 'subscribers' },
    ],
  },
  {
    key: 'content',
    items: [
      { resource: 'notification_templates', to: '/admin/notification-templates', icon: 'mail', labelKey: 'notificationTemplates' },
      { resource: 'languages', to: '/admin/languages', icon: 'globe', labelKey: 'languages' },
      { resource: 'ui_translations', to: '/admin/ui-translations', icon: 'globe', labelKey: 'uiTranslations' },
    ],
  },
  {
    key: 'management',
    items: [
      // Die Liste aller Statusseiten ist plattformweit (GET /api/pages: Global Admin); die
      // Rechte pages:* gelten für die eigene Seite (Seitenbenutzer, Domain, Webhook), nicht für den Tab.
      { resource: 'pages', to: '/admin/pages', icon: 'globe', labelKey: 'pages', globalOnly: true },
      { resource: 'users', to: '/admin/users', icon: 'user', labelKey: 'users' },
      { resource: 'roles', to: '/admin/roles', icon: 'key', labelKey: 'roles' },
      { resource: 'tags', to: '/admin/tags', icon: 'tag', labelKey: 'tags' },
      { resource: 'audit', to: '/admin/audit', icon: 'file-text', labelKey: 'audit' },
    ],
  },
  {
    key: 'system',
    items: [
      { resource: 'monitoring', to: '/admin/monitoring', icon: 'activity', labelKey: 'monitoring' },
      { resource: 'sso', to: '/admin/sso', icon: 'shield', labelKey: 'sso' },
      { resource: 'system_settings', to: '/admin/system-settings', icon: 'settings', labelKey: 'systemSettings' },
      // Doku für alle Angemeldeten; führt aus dem Admin-Bereich auf /docs.
      { resource: null, to: '/docs', icon: 'book', labelKey: 'docs' },
    ],
  },
]

// Der erste Tab, den hasPerm lesen darf (Startseite nach dem Login, Ziel auf der
// „Keine Berechtigung“-Seite). Die Doku zählt nicht: Sie ist keine Admin-Seite.
// null, wenn nichts erlaubt ist.
export function firstAllowedItem(hasPerm, isGlobalAdmin = false) {
  for (const group of NAV_GROUPS) {
    for (const item of group.items) {
      if (item.resource && (!item.globalOnly || isGlobalAdmin) && hasPerm(`${item.resource}:read`)) return item
    }
  }
  return null
}
