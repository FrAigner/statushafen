// Admin quick search: index of admin pages (from the nav) and individual
// settings (maintained registry below), plus optional docs entries.
//
// Registry entry fields:
//   id        element id of the field/section on the target page (stable, see the views)
//   route     admin path; permissions come from the nav item with this path
//   labelKey  i18n key of the visible label (searched in the current UI language)
//   keywords  extra synonyms, German and English, always searched
//   open      'activePage': open the edit form of the active status page (?edit=<id>)
//   fallbackId  target to use when `id` only shows after a toggle (e.g. private page options)
import { NAV_GROUPS } from './adminNav'

const PAGES = '/admin/pages'
const SSO = '/admin/sso'
const SYSTEM = '/admin/system-settings'

export const SETTINGS = [
  // Status page (edit form of the active page)
  { id: 'page-name', route: PAGES, open: 'activePage', labelKey: 'statusPages.fieldName', keywords: ['titel', 'title'] },
  { id: 'page-slug', route: PAGES, open: 'activePage', labelKey: 'statusPages.fieldSlug', keywords: ['url', 'adresse', 'address', 'subdomain', 'pfad', 'path'] },
  { id: 'page-description', route: PAGES, open: 'activePage', labelKey: 'statusPages.fieldDescription', keywords: ['beschreibung', 'description', 'text'] },
  { id: 'page-logo-url', route: PAGES, open: 'activePage', labelKey: 'statusPages.fieldLogoUrl', keywords: ['logo', 'bild', 'image', 'branding'] },
  { id: 'page-primary-color', route: PAGES, open: 'activePage', labelKey: 'statusPages.fieldPrimaryColor', keywords: ['farbe', 'color', 'colour', 'branding', 'theme'] },
  { id: 'page-secondary-color', route: PAGES, open: 'activePage', labelKey: 'statusPages.fieldSecondaryColor', keywords: ['farbe', 'color', 'colour', 'branding', 'theme'] },
  { id: 'page-header-style', route: PAGES, open: 'activePage', labelKey: 'statusPages.fieldHeaderStyle', keywords: ['kopfbereich', 'header', 'design', 'branding'] },
  { id: 'page-layout', route: PAGES, open: 'activePage', labelKey: 'statusPages.fieldPageLayout', keywords: ['layout', 'darstellung', 'liste', 'list', 'ansicht', 'view'] },
  { id: 'page-notification-from', route: PAGES, open: 'activePage', labelKey: 'statusPages.fieldNotificationFrom', keywords: ['reply-to', 'antwortadresse', 'absender', 'sender', 'e-mail', 'email', 'mail'] },
  { id: 'isPrivate', route: PAGES, open: 'activePage', labelKey: 'statusPages.fieldIsPrivate', keywords: ['privat', 'private', 'login', 'sichtbarkeit', 'visibility', 'intern', 'internal'] },
  { id: 'page-default-role', route: PAGES, open: 'activePage', fallbackId: 'isPrivate', labelKey: 'statusPages.fieldDefaultRole', keywords: ['rolle', 'role', 'standardrolle', 'default role', 'zugriff', 'access', 'privat', 'private'] },
  { id: 'page-autojoin-domains', route: PAGES, open: 'activePage', fallbackId: 'isPrivate', labelKey: 'statusPages.fieldAutoJoinDomains', keywords: ['auto-beitritt', 'autobeitritt', 'auto-join', 'autojoin', 'beitritt', 'join', 'domain', 'privat', 'private'] },
  { id: 'page-webhook', route: PAGES, open: 'activePage', labelKey: 'statusPages.webhookHeading', keywords: ['webhook', 'token', 'monitoring', 'api', 'x-webhook-token'] },
  { id: 'custom-domain-input', route: PAGES, open: 'activePage', labelKey: 'statusPages.customDomainHeading', keywords: ['domain', 'eigene domain', 'cname', 'dns', 'txt'] },
  // SSO of the active page
  { id: 'sso-enabled', route: SSO, labelKey: 'admin.sso.enable', keywords: ['oidc', 'openid', 'sso', 'single sign-on', 'anmeldung', 'login'] },
  { id: 'sso-discovery-url', route: SSO, labelKey: 'admin.sso.discoveryUrl', keywords: ['oidc', 'openid', 'issuer', 'well-known'] },
  { id: 'sso-client-id', route: SSO, labelKey: 'admin.sso.clientId', keywords: ['oidc', 'openid', 'app id'] },
  { id: 'sso-client-secret', route: SSO, labelKey: 'admin.sso.clientSecret', keywords: ['oidc', 'geheimnis', 'passwort', 'password'] },
  { id: 'sso-scopes', route: SSO, labelKey: 'admin.sso.scopes', keywords: ['oidc', 'openid', 'claims'] },
  { id: 'sso-attribute-email', route: SSO, labelKey: 'admin.sso.attributeEmail', keywords: ['oidc', 'claim', 'mail', 'e-mail', 'attribut', 'attribute'] },
  { id: 'sso-attribute-name', route: SSO, labelKey: 'admin.sso.attributeName', keywords: ['oidc', 'claim', 'name', 'attribut', 'attribute'] },
  { id: 'sso-acs-url', route: SSO, labelKey: 'admin.sso.acsUrl', keywords: ['callback', 'redirect', 'weiterleitung', 'rückruf'] },
  { id: 'sso-test-login', route: SSO, labelKey: 'admin.sso.testLogin', keywords: ['test', 'probe', 'testanmeldung', 'test login'] },
  // Platform settings
  { id: 'setting-orders_enabled', route: SYSTEM, labelKey: 'systemSettings.ordersSectionTitle', keywords: ['bestellung', 'bestellen', 'order', 'self-service', 'neue statusseite', 'new status page'] },
  { id: 'setting-email_enabled', route: SYSTEM, labelKey: 'systemSettings.emailSectionTitle', keywords: ['e-mail', 'email', 'mail', 'versand', 'sending', 'smtp'] },
  { id: 'setting-selfmonitor_enabled', route: SYSTEM, labelKey: 'systemSettings.selfmonitorSectionTitle', keywords: ['selbstüberwachung', 'self-monitoring', 'health', 'monitoring', 'systemseite'] },
  { id: 'setting-translation', route: SYSTEM, labelKey: 'translationSettings.title', keywords: ['übersetzung', 'translation', 'deepl', 'libretranslate', 'api-key', 'api key', 'anbieter', 'provider'] },
  // Languages of the active page
  { id: 'page-languages', route: '/admin/languages', labelKey: 'languages.pageLanguagesHeading', keywords: ['sprache', 'language', 'standardsprache', 'default language', 'mehrsprachig', 'multilingual'] },
  // Own profile (every signed-in user)
  { id: 'profile-language', route: '/admin/profile', labelKey: 'profile.languageLabel', keywords: ['sprache', 'language', 'ui', 'oberfläche', 'interface'] },
]

const NAV_ITEMS = NAV_GROUPS.flatMap(group => group.items)

export function navItemForPath(path) {
  return NAV_ITEMS.find(item => item.to === path) ?? null
}

// Lowercase, fold diacritics (ü -> u, ß -> ss) and collapse whitespace.
export function normalize(text) {
  return String(text)
    .toLowerCase()
    .replace(/ß/g, 'ss')
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

// Builds all entries for the current language. `canSee(navItem)` is the nav's
// visibility rule; entries on a route without a nav item (profile) are open to all.
export function buildSearchIndex({ t, canSee, activePageId = null, docs = [] }) {
  const pages = NAV_ITEMS.filter(canSee).map(item => ({
    kind: 'page',
    key: item.to,
    label: t(`adminNav.items.${item.labelKey}`),
    context: t(`adminNav.groups.${NAV_GROUPS.find(g => g.items.includes(item)).key}`),
    to: item.to,
    icon: item.icon,
    keywords: [],
  }))

  const settings = SETTINGS.flatMap(setting => {
    const navItem = navItemForPath(setting.route)
    if (navItem && !canSee(navItem)) return []
    const to = setting.open === 'activePage' && activePageId != null
      ? { path: setting.route, query: { edit: String(activePageId) } }
      : { path: setting.route }
    return [{
      kind: 'setting',
      key: setting.id,
      label: t(setting.labelKey),
      context: navItem ? t(`adminNav.items.${navItem.labelKey}`) : t('nav.profile'),
      to,
      targetId: setting.id,
      fallbackId: setting.fallbackId,
      keywords: setting.keywords,
    }]
  })

  return [...pages, ...settings, ...docs.map(doc => ({ keywords: [], ...doc, kind: 'doc' }))]
}

const GROUP_ORDER = ['page', 'setting', 'doc']

// Lower is better: label starts with the query, label contains it, a label
// word starts with a token, anything else (context/keywords only).
function score(entry, query, tokens) {
  const label = normalize(entry.label)
  if (label.startsWith(query)) return 0
  if (label.includes(query)) return 1
  const words = label.split(/[^a-z0-9]+/)
  if (tokens.every(tok => words.some(w => w.startsWith(tok)))) return 2
  if (entry.keywords.some(k => normalize(k).startsWith(query))) return 3
  return 4
}

// All query tokens must occur in label, context or keywords.
export function searchIndex(entries, rawQuery, { limit = 8 } = {}) {
  const query = normalize(rawQuery)
  if (!query) return []
  const tokens = query.split(' ')
  const hits = entries
    .map((entry, order) => ({ entry, order, haystack: normalize([entry.label, entry.context, ...entry.keywords].join(' ')) }))
    .filter(({ haystack }) => tokens.every(tok => haystack.includes(tok)))
    .map(hit => ({ ...hit, score: score(hit.entry, query, tokens) }))
    .sort((a, b) => a.score - b.score || a.order - b.order)

  return GROUP_ORDER
    .map(kind => ({ kind, entries: hits.filter(h => h.entry.kind === kind).slice(0, limit).map(h => h.entry) }))
    .filter(group => group.entries.length > 0)
}
