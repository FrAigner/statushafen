import { describe, it, expect } from 'vitest'
import i18n from '../i18n'
import de from '../i18n/locales/de.json'
import en from '../i18n/locales/en.json'
import router from '../router'
import { NAV_GROUPS } from './adminNav'
import { SETTINGS, normalize, buildSearchIndex, searchIndex, navItemForPath } from './adminSearch'

const lookup = (messages, key) => key.split('.').reduce((o, k) => (o == null ? undefined : o[k]), messages)
const t = (key) => i18n.global.t(key)
const all = () => true

function build({ canSee = all, activePageId = 7, docs = [] } = {}) {
  return buildSearchIndex({ t, canSee, activePageId, docs })
}

const keysOf = (groups, kind) => (groups.find(g => g.kind === kind)?.entries ?? []).map(e => e.key)

describe('settings registry', () => {
  it('every entry has a unique target id', () => {
    const ids = SETTINGS.map(s => s.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it.each(SETTINGS.map(s => [s.id, s]))('%s: route exists in the router', (_id, s) => {
    const resolved = router.resolve(s.route)
    expect(resolved.matched.length).toBeGreaterThan(0)
    expect(resolved.path).toBe(s.route)
  })

  it.each(SETTINGS.map(s => [s.id, s]))('%s: label key exists in de.json and en.json', (_id, s) => {
    expect(typeof lookup(de, s.labelKey)).toBe('string')
    expect(typeof lookup(en, s.labelKey)).toBe('string')
  })

  it('every admin route of the registry is either a nav item or the profile page', () => {
    for (const s of SETTINGS) {
      expect(navItemForPath(s.route) !== null || s.route === '/admin/profile').toBe(true)
    }
  })
})

describe('normalize', () => {
  it('lowercases and folds umlauts and diacritics', () => {
    expect(normalize('Übersetzung ÄÖ ß')).toBe('ubersetzung ao ss')
    expect(normalize('  Discovery   URL ')).toBe('discovery url')
  })
})

describe('admin quick search', () => {
  it.each([
    ['Antwortadresse', 'page-notification-from'],
    ['privat', 'isPrivate'],
    ['Discovery URL', 'sso-discovery-url'],
    ['Bestellung', 'setting-orders_enabled'],
    ['Auto-Beitritt', 'page-autojoin-domains'],
    ['reply-to', 'page-notification-from'],
    ['deepl', 'setting-translation'],
  ])('finds "%s" as setting %s', (query, id) => {
    i18n.global.locale.value = 'de'
    expect(keysOf(searchIndex(build(), query), 'setting')).toContain(id)
  })

  it('searches the labels of the current UI language', () => {
    i18n.global.locale.value = 'en'
    try {
      const enLabel = lookup(en, 'admin.sso.clientSecret')
      expect(keysOf(searchIndex(build(), enLabel), 'setting')).toContain('sso-client-secret')
    } finally {
      i18n.global.locale.value = 'de'
    }
  })

  it('finds admin pages from the navigation', () => {
    const groups = searchIndex(build(), 'Vorfälle')
    expect(keysOf(groups, 'page')).toContain('/admin/incidents')
  })

  it('requires all words of the query (AND search)', () => {
    expect(keysOf(searchIndex(build(), 'client secret'), 'setting')).toEqual(['sso-client-secret'])
  })

  it('ranks label prefix matches before keyword-only matches', () => {
    const settings = searchIndex(build(), 'privat').find(g => g.kind === 'setting').entries
    expect(settings[0].key).toBe('isPrivate')
  })

  it('returns nothing for an empty query', () => {
    expect(searchIndex(build(), '   ')).toEqual([])
  })

  it('hides pages and settings the user may not see (same rule as the nav)', () => {
    const canSee = (item) => item.resource !== 'sso'
    const groups = searchIndex(build({ canSee }), 'Discovery')
    expect(keysOf(groups, 'setting')).not.toContain('sso-discovery-url')
    expect(keysOf(searchIndex(build({ canSee }), 'SSO'), 'page')).not.toContain('/admin/sso')
  })

  it('keeps profile settings for everyone signed in', () => {
    const groups = searchIndex(build({ canSee: () => false }), 'Oberflächensprache')
    expect(keysOf(groups, 'setting')).toContain('profile-language')
  })

  it('opens the edit form of the active status page for page settings', () => {
    const entry = build({ activePageId: 7 }).find(e => e.key === 'page-notification-from')
    expect(entry.to).toEqual({ path: '/admin/pages', query: { edit: '7' } })
    expect(entry.targetId).toBe('page-notification-from')
    expect(entry.context).toBe(t('adminNav.items.pages'))
  })

  it('goes to the page without opening anything when no status page is active', () => {
    const entry = build({ activePageId: null }).find(e => e.key === 'page-notification-from')
    expect(entry.to).toEqual({ path: '/admin/pages' })
  })

  it('names a fallback target for fields that only show after a toggle', () => {
    const entry = build().find(e => e.key === 'page-autojoin-domains')
    expect(entry.fallbackId).toBe('isPrivate')
  })

  it('adds docs entries as their own group', () => {
    const docs = [{ key: 'sso#oidc', label: 'OIDC einrichten', context: 'SSO', to: '/docs/sso#oidc' }]
    const groups = searchIndex(build({ docs }), 'oidc einrichten')
    expect(groups.map(g => g.kind)).toContain('doc')
    expect(keysOf(groups, 'doc')).toEqual(['sso#oidc'])
  })

  it('orders groups pages, settings, docs and limits each group', () => {
    const docs = Array.from({ length: 20 }, (_, i) => ({ key: `d${i}`, label: `Statusseite ${i}`, context: 'x', to: '/docs' }))
    const groups = searchIndex(build({ docs }), 'statusseite', { limit: 5 })
    expect(groups.map(g => g.kind)).toEqual(['page', 'setting', 'doc'])
    expect(groups.find(g => g.kind === 'doc').entries).toHaveLength(5)
  })

  it('every nav item is in the index', () => {
    const keys = build().filter(e => e.kind === 'page').map(e => e.key)
    expect(keys).toEqual(NAV_GROUPS.flatMap(g => g.items).map(i => i.to))
  })
})

describe('registry targets exist in the views', () => {
  const sources = Object.values(import.meta.glob(['../views/admin/*.vue', '../components/*.vue'], { query: '?raw', import: 'default', eager: true })).join('\n')
  const systemSettings = Object.entries(import.meta.glob('../views/admin/AdminSystemSettings.vue', { query: '?raw', import: 'default', eager: true }))[0][1]

  it.each(SETTINGS.map(s => [s.id]))('%s has a stable id in the markup', (id) => {
    const dynamic = id.match(/^setting-(\w+_enabled)$/)
    if (dynamic) {
      expect(systemSettings).toContain(':id="`setting-${section.field}`"')
      expect(systemSettings).toContain(`field: '${dynamic[1]}'`)
    } else {
      expect(sources.includes(`"${id}"`) || sources.includes(`'${id}'`)).toBe(true)
    }
  })
})
