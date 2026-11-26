import { describe, it, expect } from 'vitest'
import { createI18n } from 'vue-i18n'
import de from './locales/de.json'
import en from './locales/en.json'

// Recursively collect all leaf keys as dot paths, e.g. "admin.users.title".
function flattenKeys(obj, prefix = '') {
  return Object.entries(obj).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      return flattenKeys(value, path)
    }
    return [path]
  })
}

// Guard test (batch 0, translation rollout): makes sure that de.json and
// en.json have exactly the same keys - a missing English key would otherwise
// silently fall back to the German fallback (fallbackLocale: 'de') instead of
// making the build/CI fail visibly.
describe('i18n locale key parity', () => {
  it('de.json und en.json haben identische Key-Mengen', () => {
    const deKeys = flattenKeys(de).sort()
    const enKeys = flattenKeys(en).sort()

    const missingInEn = deKeys.filter(k => !enKeys.includes(k))
    const missingInDe = enKeys.filter(k => !deKeys.includes(k))

    expect(missingInEn, `Keys fehlen in en.json: ${missingInEn.join(', ')}`).toEqual([])
    expect(missingInDe, `Keys fehlen in de.json: ${missingInDe.join(', ')}`).toEqual([])
  })

  // Guard against unescaped vue-i18n special characters (especially "@" - triggers "Invalid
  // linked format", see the bugfix in admin.subscribers.emailPlaceholder
  // & co.): every message must compile and render without errors for both locales,
  // instead of only crashing at runtime when it is actually used
  // in a view.
  it('jede Message kompiliert ohne Fehler (z.B. unescapte "@"-Zeichen)', () => {
    const i18n = createI18n({ legacy: false, locale: 'de', fallbackLocale: 'de', messages: { de, en } })
    const params = Object.fromEntries(
      ['id', 'name', 'message', 'count', 'brand', 'page', 'selected', 'total', 'components', 'incidents'].map(p => [p, 'x'])
    )
    const errors = []
    for (const locale of ['de', 'en']) {
      i18n.global.locale.value = locale
      for (const key of flattenKeys(locale === 'de' ? de : en)) {
        try {
          i18n.global.t(key, params)
        } catch (e) {
          errors.push(`${locale}:${key} -> ${e.message}`)
        }
      }
    }
    expect(errors).toEqual([])
  })
})

// The profile page must not show an internal developer note.
const FORBIDDEN = {
  de: /Issue|#|Minimal|vorerst/i,
  en: /Issue|#|Minimal|for now/i,
}

describe('profile.description', () => {
  it.each([
    ['de', de],
    ['en', en],
  ])('%s: nicht leer und ohne interne Hinweise', (name, messages) => {
    const text = messages.profile.description
    expect(text.trim()).not.toBe('')
    expect(text).not.toMatch(FORBIDDEN[name])
  })

  // The forbidden pattern must also catch word variants like "Minimalprofil".
  it.each([
    ['de', 'Minimalprofil'],
    ['de', 'Minimale Seite'],
    ['de', 'Minimal-Profil: kommt später'],
    ['de', 'Details folgen vorerst nicht'],
    ['de', 'siehe Issue-Tracker'],
    ['en', 'Minimal profile'],
    ['en', 'Minimalprofile'],
    ['en', 'Placeholder for now'],
  ])('%s: Verbotsmuster erkennt "%s"', (name, text) => {
    expect(text).toMatch(FORBIDDEN[name])
  })
})

// "Mein Abo laden" (removed button) and "Abo-Einstellungen"
// (old term in the user menu) must no longer appear in either locale
// file; the entry point is uniformly called "Abonnement
// verwalten" (de) / "Manage subscription" (en).
describe('Abonnement-Begriffe', () => {
  it.each([
    ['de.json', de, 'Mein Abo laden'],
    ['de.json', de, 'Abo-Einstellungen'],
    ['en.json', en, 'Load my subscription'],
    ['en.json', en, 'Subscription settings'],
  ])('%s enthält "%s" nirgends mehr', (_name, messages, forbidden) => {
    expect(JSON.stringify(messages)).not.toContain(forbidden)
  })

  // in the text always "Abonnement", also in loading and error messages
  // (e.g. unsubscribing, subscribing on the status page). \b separates "Abo" from
  // "Abonnement"/"Abonnenten", but catches "Abo-..." and "Abo ...".
  it('de.json verwendet nirgends die Kurzform "Abo"', () => {
    const leftovers = JSON.stringify(de).match(/[^"]*\bAbos?\b[^"]*/g) ?? []
    expect(leftovers).toEqual([])
  })

  it.each([
    ['de', de, 'Abonnement verwalten'],
    ['en', en, 'Manage subscription'],
  ])('%s: Bereichsüberschrift und Benutzermenü verwenden einheitlich "%s"', (_name, messages, unified) => {
    // 4a: the second button in the subscribe box no longer exists
    expect(messages.statusPage.editSubscriptionButton).toBeUndefined()
    expect(messages.components.subscriptionManager.editSectionTitle).toBe(unified)
    expect(messages.layout.public.subscriptionSettings).toBe(unified)
  })
})

// Branding: the template's placeholder branding ("Onyx Status
// Enterprise" or "Status Enterprise CMS") was replaced by the real product name
// "Statushafen". This guard prevents the old placeholders from
// coming back unnoticed through merges or future translation batches, and
// at the same time ensures that the hero subtitle carries the new name.
describe('Branding Statushafen', () => {
  it.each([
    ['de.json', de, 'Onyx Status Enterprise'],
    ['de.json', de, 'Status Enterprise CMS'],
    ['en.json', en, 'Onyx Status Enterprise'],
    ['en.json', en, 'Status Enterprise CMS'],
  ])('%s enthält den Platzhalter "%s" nirgends mehr', (_name, messages, forbidden) => {
    expect(JSON.stringify(messages)).not.toContain(forbidden)
  })

  it.each([
    ['de', de],
    ['en', en],
  ])('%s: heroSubtitle nennt den Produktnamen "Statushafen"', (_name, messages) => {
    expect(messages.landing.heroSubtitle).toContain('Statushafen')
  })
})

// The title of the incidents page mixed German and English ("Vorfälle (Incidents)");
// all other admin titles are monolingual.
describe('Seitentitel Vorfälle', () => {
  it('de.json: incidents.title ist rein deutsch', () => {
    expect(de.incidents.title).toBe('Vorfälle')
    expect(de.incidents.title).not.toMatch(/\(/)
  })
})

// German texts consistently say "Vorfall"/"Vorfälle", not "Incident(s)"
// ("Offene Incidents", "Incident-Detail →" …). Key names, placeholders, English texts and
// code stay untouched; only the values of de.json are checked.
// Allowlist: paths in which "Incident" may deliberately appear (proper names, API field names).
const INCIDENT_ALLOWLIST = []

function leafValues(obj, prefix = '') {
  return Object.entries(obj).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === 'object') return leafValues(value, path)
    return typeof value === 'string' ? [[path, value]] : []
  })
}

describe('Begriff Vorfall statt Incident', () => {
  it('de.json: kein Wert enthält „Incident“ (außer Allowlist)', () => {
    const offenders = leafValues(de)
      .filter(([path]) => !INCIDENT_ALLOWLIST.includes(path))
      // Placeholders like {incidents} or {{.IncidentTitle}} are not visible texts.
      .filter(([, value]) => /incident/i.test(value.replace(/\{\{?[^}]*\}\}?/g, '')))
      .map(([path, value]) => `${path}: ${value}`)
    expect(offenders).toEqual([])
  })
})

// Sign-in is OIDC only: no UI text mentions SAML. Audit log actions are the exception,
// so older entries (sso.saml_identities_reset) stay readable.
describe('no SAML texts', () => {
  function flattenEntries(obj, prefix = '') {
    return Object.entries(obj).flatMap(([key, value]) => {
      const path = prefix ? `${prefix}.${key}` : key
      return value && typeof value === 'object' ? flattenEntries(value, path) : [[path, String(value)]]
    })
  }

  it.each([['de', de], ['en', en]])('%s.json has neither keys nor texts about SAML', (_, messages) => {
    const hits = flattenEntries(messages)
      .filter(([path]) => !path.startsWith('auditLog.actions.'))
      .filter(([path, value]) => /saml/i.test(path) || /saml/i.test(value))
      .map(([path]) => path)
    expect(hits).toEqual([])
  })
})
