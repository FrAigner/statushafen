// Navigation of the admin area as data. Every tab is a resource of the
// permission model (utils/permissions.ts); layout, router metadata, the start
// page after login and the role matrix all read from here, so that tabs,
// permissions and labels do not drift apart.
//
// `resource: null` = no permission needed (docs). `labelKey` points to
// adminNav.items.<key> in the locale files.

export interface NavItem {
  resource: string | null
  to: string
  icon: string
  labelKey: string
  globalOnly?: boolean
}

export interface NavGroup {
  key: string
  items: NavItem[]
}

export const NAV_GROUPS: NavGroup[] = [
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
      // The list of all status pages is platform-wide (GET /api/pages: global admin); the
      // pages:* permissions apply to one's own page (page users, domain, webhook), not to the tab.
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
      // Docs for every signed-in user, shown inside the admin frame.
      { resource: null, to: '/admin/docs', icon: 'book', labelKey: 'docs' },
    ],
  },
]

// The first tab hasPerm may read (start page after login, target on the
// "no permission" page). The docs do not count: they are not an admin page.
// null if nothing is allowed.
export function firstAllowedItem(hasPerm: (perm: string) => boolean, isGlobalAdmin = false): NavItem | null {
  for (const group of NAV_GROUPS) {
    for (const item of group.items) {
      if (item.resource && (!item.globalOnly || isGlobalAdmin) && hasPerm(`${item.resource}:read`)) return item
    }
  }
  return null
}
