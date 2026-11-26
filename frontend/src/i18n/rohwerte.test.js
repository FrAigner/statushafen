import { describe, it, expect } from 'vitest'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { incidentBadge, maintenanceBadge, noticeBadge, actionBadge } from '../utils/badgeStatus'
import de from './locales/de.json'
import en from './locales/en.json'

// Raw values, English leftovers and a mix of Sie/du in the German
// UI. Every test covers one of the cases found.

function values(obj) {
  return Object.values(obj).flatMap(v => (v && typeof v === 'object' ? values(v) : [String(v)]))
}

describe('Anrede in de.json', () => {
  it('verwendet nirgends die förmliche Sie-Anrede (Projekt duzt)', () => {
    // Imperative/modal forms with "Sie" or possessive "Ihr..." as an address;
    // "Sie gelten" (sie = plural) and "Ihre Bausteine" (source) stay allowed.
    const formal = /\b(Fügen Sie|Geben Sie|Bitte (prüfen|versuchen|wählen|geben) Sie|Sie (können|müssen|sollten|haben)|bei Ihr\w*|Ihr(e|en|em|er)? (DNS|Konto|Passwort|E-Mail))\b/
    const hits = values(de).filter(v => formal.test(v))
    expect(hits).toEqual([])
  })

  it('schreibt E-Mail mit Bindestrich', () => {
    expect(values(de).filter(v => /\bEmails?\b/.test(v))).toEqual([])
  })
})

describe('Übersetzte Rohwerte in den Admin-Oberflächen', () => {
  it('Kopfzeilen-Ausrichtung, Verantwortlich-Platzhalter, Auslastung und Tag-Texte sind deutsch', () => {
    expect(de.statusPages.headerCentered).toBe('Zentriert')
    expect(de.incidentDetail.actionOwnerPlaceholder).toBe('Verantwortlich')
    expect(de.monitoring.saturationHeading).toBe('Auslastung')
    expect(de.auditLog.automationRole).toBe('Automatisierung')
    expect(de.tags.scopeGlobalOption).not.toMatch(/repo/)
    expect(de.tags.confirmDelete).not.toMatch(/Components|Incidents|Usern/)
  })

  it('Navigationseintrag und Seitentitel der Mail-Templates heißen gleich', () => {
    expect(de.notificationTemplates.title).toBe(de.adminNav.items.notificationTemplates)
    expect(en.notificationTemplates.title).toBe(en.adminNav.items.notificationTemplates)
  })
})

describe('Audit-Aktionen', () => {
  it('jede Aktion hat in de und en eine Beschriftung, die kein Rohschlüssel ist', () => {
    for (const messages of [de, en]) {
      for (const [group, entries] of Object.entries(messages.auditLog.actions)) {
        for (const [key, label] of Object.entries(entries)) {
          expect(label, `${group}.${key}`).not.toBe(`${group}.${key}`)
          expect(label.length).toBeGreaterThan(3)
        }
      }
    }
  })
})

// defaultRoleHint claimed the default role applied to every
// signed-in account; in fact it only applies with a matching email domain.
describe('statusPages.defaultRoleHint', () => {
  it('nennt die E-Mail-Domain-Bedingung und behauptet nicht "jeder Account"', () => {
    for (const loc of [de, en]) {
      const hint = loc.statusPages.defaultRoleHint
      expect(hint).toContain(loc.statusPages.fieldAutoJoinDomains)
      expect(hint).not.toMatch(/jeden eingeloggten Account|every logged-in account/)
    }
  })
})

// Remainder: the audit actions originate in the backend; every action written
// there needs a label in de and en, otherwise the list shows
// the raw key ("language.deleted"). The test reads the action literals
// from handlers/*.go, so that a new action without a translation turns red.
describe('Backend-Audit-Aktionen sind übersetzt', () => {
  const dir = resolve(__dirname, '../../../backend/handlers')
  const source = readdirSync(dir)
    .filter(f => f.endsWith('.go') && !f.endsWith('_test.go'))
    .map(f => readFileSync(resolve(dir, f), 'utf8'))
    .join('\n')
  const literal = /(?:Audit\((?:c, )?|[aA]ction\w*\s*(?::?=|:)\s*|\baction = )"([a-z_]+\.[a-z_]+)"/g
  const actions = [...new Set([...source.matchAll(literal)].map(m => m[1]))]

  it('findet die Aktionen im Backend (Selbstprüfung des Tests)', () => {
    expect(actions.length).toBeGreaterThan(50)
    expect(actions).toContain('language.deleted')
  })

  it.each([['de', de], ['en', en]])('%s: jede Backend-Aktion hat eine Beschriftung', (_n, messages) => {
    const missing = actions.filter(a => {
      const [group, key] = a.split('.')
      return !messages.auditLog.actions[group]?.[key]
    })
    expect(missing).toEqual([])
  })

  it('de und en haben dieselben Aktionsschlüssel und Kategorien', () => {
    const keys = m => Object.entries(m.auditLog.actions).flatMap(([g, v]) => Object.keys(v).map(k => `${g}.${k}`)).sort()
    expect(keys(de)).toEqual(keys(en))
    const cats = m => Object.keys(m.auditLog).filter(k => k.startsWith('cat')).sort()
    expect(cats(de)).toEqual(cats(en))
  })
})

describe('Status-, Stufen- und Aktionswerte werden nie roh angezeigt', () => {
  const t = (messages) => (key) => key.split('.').reduce((o, k) => o?.[k], messages) ?? key
  const cases = [
    [incidentBadge, ['investigating', 'identified', 'monitoring', 'resolved']],
    [maintenanceBadge, ['scheduled', 'in_progress', 'resolved']],
    [noticeBadge, ['info', 'warning', 'critical']],
    [actionBadge, ['open', 'in_progress', 'done']],
  ]
  it.each([['de', de], ['en', en]])('%s: Beschriftung existiert und ist nicht der Rohwert', (name, messages) => {
    for (const [fn, values] of cases) {
      for (const v of values) {
        const label = fn(v, t(messages)).label
        expect(label, `${fn.name}(${v})`).not.toBe(v)
        expect(label, `${fn.name}(${v})`).not.toMatch(/^[a-z]+\.[A-Za-z]+$/)
        // English may equal the raw value except for capitalization (Investigating); "Info" is German too.
        if (name === 'de' && v !== 'info') expect(label.toLowerCase(), `${fn.name}(${v})`).not.toBe(v.toLowerCase())
      }
    }
  })
  it('Statusauswahl in Vorfällen und Vorlagen nutzt dieselben übersetzten Schlüssel', () => {
    for (const messages of [de, en]) {
      for (const k of ['Investigating', 'Identified', 'Monitoring', 'Resolved']) {
        expect(messages.incidentDetail[`status${k}`]).toBeTruthy()
      }
    }
  })
})
