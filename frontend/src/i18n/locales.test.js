import { describe, it, expect } from 'vitest'
import { createI18n } from 'vue-i18n'
import de from './locales/de.json'
import en from './locales/en.json'

// Rekursiv alle Blatt-Keys als Punkt-Pfade sammeln, z.B. "admin.users.title".
function flattenKeys(obj, prefix = '') {
  return Object.entries(obj).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      return flattenKeys(value, path)
    }
    return [path]
  })
}

// Guard-Test (Batch 0, Übersetzungs-Rollout): stellt sicher, dass de.json und
// en.json exakt dieselben Keys haben - ein fehlender englischer Key fällt
// sonst still auf den deutschen Fallback zurück (fallbackLocale: 'de') statt
// den Build/CI sichtbar scheitern zu lassen.
describe('i18n locale key parity', () => {
  it('de.json und en.json haben identische Key-Mengen', () => {
    const deKeys = flattenKeys(de).sort()
    const enKeys = flattenKeys(en).sort()

    const missingInEn = deKeys.filter(k => !enKeys.includes(k))
    const missingInDe = enKeys.filter(k => !deKeys.includes(k))

    expect(missingInEn, `Keys fehlen in en.json: ${missingInEn.join(', ')}`).toEqual([])
    expect(missingInDe, `Keys fehlen in de.json: ${missingInDe.join(', ')}`).toEqual([])
  })

  // Guard gegen unescapte vue-i18n-Sonderzeichen (v.a. "@" - löst "Invalid
  // linked format" aus, siehe Bugfix in admin.subscribers.emailPlaceholder
  // & Co.): jede Message muss sich für beide Locales fehlerfrei kompilieren
  // und rendern lassen, statt erst beim tatsächlichen Aufruf in einer View
  // zur Laufzeit zu krachen.
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

// Die Profilseite darf keinen internen Entwicklerhinweis zeigen.
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

  // Das Verbotsmuster muss auch Wortvarianten wie „Minimalprofil“ erkennen.
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

// "Mein Abo laden" (entfernter Knopf) und "Abo-Einstellungen"
// (alter Begriff im Benutzermenü) dürfen in keiner der beiden Locale-Dateien
// mehr vorkommen; der Einstieg heißt überall einheitlich "Abonnement
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

  // im Text immer "Abonnement", auch in Lade- und Fehlermeldungen
  // (z. B. Abbestellen, Abonnieren auf der Statusseite). \b trennt "Abo" von
  // "Abonnement"/"Abonnenten", erkennt aber "Abo-..." und "Abo ...".
  it('de.json verwendet nirgends die Kurzform "Abo"', () => {
    const leftovers = JSON.stringify(de).match(/[^"]*\bAbos?\b[^"]*/g) ?? []
    expect(leftovers).toEqual([])
  })

  it.each([
    ['de', de, 'Abonnement verwalten'],
    ['en', en, 'Manage subscription'],
  ])('%s: Bereichsüberschrift und Benutzermenü verwenden einheitlich "%s"', (_name, messages, unified) => {
    // 4a: den zweiten Knopf im Abonnieren-Kasten gibt es nicht mehr
    expect(messages.statusPage.editSubscriptionButton).toBeUndefined()
    expect(messages.components.subscriptionManager.editSectionTitle).toBe(unified)
    expect(messages.layout.public.subscriptionSettings).toBe(unified)
  })
})

// Branding: Das Platzhalter-Branding der Vorlage ("Onyx Status
// Enterprise" bzw. "Status Enterprise CMS") wurde durch den echten Produktnamen
// "Statushafen" ersetzt. Dieser Guard verhindert, dass die alten Platzhalter
// über Merges oder künftige Übersetzungs-Batches unbemerkt zurückkehren, und
// sichert zugleich ab, dass der Hero-Subtitle den neuen Namen führt.
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

// Der Titel der Vorfallsseite mischte Deutsch und Englisch („Vorfälle (Incidents)“);
// alle übrigen Admin-Titel sind einsprachig.
describe('Seitentitel Vorfälle', () => {
  it('de.json: incidents.title ist rein deutsch', () => {
    expect(de.incidents.title).toBe('Vorfälle')
    expect(de.incidents.title).not.toMatch(/\(/)
  })
})

// In deutschen Texten steht durchgängig „Vorfall“/„Vorfälle“, nicht „Incident(s)“
// („Offene Incidents“, „Incident-Detail →“ …). Schlüsselnamen, Platzhalter, englische Texte und
// Code bleiben unberührt; geprüft werden nur die Werte der de.json.
// Allowlist: Pfade, in denen „Incident“ bewusst stehen darf (Eigennamen, API-Feldnamen).
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
      // Platzhalter wie {incidents} oder {{.IncidentTitle}} sind keine sichtbaren Texte.
      .filter(([, value]) => /incident/i.test(value.replace(/\{\{?[^}]*\}\}?/g, '')))
      .map(([path, value]) => `${path}: ${value}`)
    expect(offenders).toEqual([])
  })
})
