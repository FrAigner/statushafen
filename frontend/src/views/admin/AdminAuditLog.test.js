import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { OnyxInputStub } from '../../test-support/onyxStubs'
import i18n from '../../i18n'
import AdminAuditLog from './AdminAuditLog.vue'
import { axe } from '../../test-support/axe.js'

function entriesFixture() {
  return [
    {
      id: 1, created_at: 1700000000, actor_email: 'admin@x.de', actor_role: 'Global Admin',
      action: 'hat Incident erstellt', target_name: 'DB Ausfall', category: 'incident', ip: '10.0.0.1',
    },
    {
      id: 2, created_at: 1700000100, actor_email: 'user@x.de', actor_role: 'Editor',
      action: 'hat sich angemeldet', target_name: '', category: 'auth', ip: '10.0.0.2',
    },
  ]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

async function mountView({ items = entriesFixture(), total } = {}) {
  global.fetch = vi.fn(async () => jsonResponse({ items, total: total ?? items.length }))
  const wrapper = mount(AdminAuditLog, {
    global: { plugins: [i18n], stubs: { OnyxInput: OnyxInputStub } },
  })
  await flushPromises()
  return wrapper
}

describe('AdminAuditLog', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    // Tests assertieren auf den deutschen Wortlaut übersetzter Default-Texte
    // (ResponsiveTable-Kindkomponente); ohne gespeicherte Präferenz würde
    // detectInitialLocale() in der Testumgebung (happy-dom navigator.language
    // === 'en-US') sonst auf 'en' fallen.
    i18n.global.locale.value = 'de'
  })

  it('rendert eine Tabellenzeile pro Audit-Eintrag mit Akteur, Aktion, Kategorie und IP', async () => {
    const wrapper = await mountView()

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('admin@x.de')
    expect(rows[0].text()).toContain('DB Ausfall')
    expect(rows[0].text()).toContain('Vorfall')
    expect(rows[0].text()).toContain('10.0.0.1')
  })

  it('zeigt einen Leer-Hinweis, wenn keine Einträge vorhanden sind', async () => {
    const wrapper = await mountView({ items: [], total: 0 })

    expect(wrapper.text()).toContain('Keine Einträge gefunden.')
    expect(wrapper.find('.empty-state').exists()).toBe(true)
    expect(wrapper.find('table').exists()).toBe(false)
  })

  // Stufe 4d: Titel als h2 (die Topbar trägt die h1), Untertitel im Kopf.
  it('rendert den Titel als h2 ohne h1 und mit Untertitel', async () => {
    const wrapper = await mountView()

    expect(wrapper.find('h2.page-head__title').exists()).toBe(true)
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.find('.page-head__subtitle').text()).not.toBe('')
  })

  // Stufe 4d: Die Kategoriefarben waren Inline-Styles mit Hex
  // (#6b4de6, #fff). Jetzt stehen sie als Klassen auf Tokens; der Filter ist
  // über aria-pressed auch ohne Farbe erkennbar.
  it('färbt Kategorien über Klassen statt Inline-Styles und setzt aria-pressed am aktiven Chip', async () => {
    const wrapper = await mountView()

    expect(wrapper.html()).not.toContain('style=')
    expect(wrapper.findAll('tbody tr')[0].find('.ent-pill').classes()).toContain('cat--incident')
    const chip = wrapper.findAll('.chip').find(c => c.text() === 'Vorfall')
    expect(chip.attributes('aria-pressed')).toBe('false')
    await chip.trigger('click')
    await flushPromises()
    expect(chip.attributes('aria-pressed')).toBe('true')
    expect(chip.classes()).toContain('active')
  })

  it('zeigt für Einträge ohne Akteur "System" und die Automations-Rolle', async () => {
    const wrapper = await mountView({ items: [{ ...entriesFixture()[0], actor_email: '', actor_role: '' }] })

    expect(wrapper.find('.a-name').text()).toBe(i18n.global.t('auditLog.systemActor'))
    expect(wrapper.find('.a-role').text()).toBe(i18n.global.t('auditLog.automationRole'))
  })

  it('zeigt eine unbekannte Kategorie neutral mit dem Rohwert', async () => {
    const wrapper = await mountView({ items: [{ ...entriesFixture()[0], category: 'billing' }] })

    const pill = wrapper.find('tbody .ent-pill')
    expect(pill.classes()).toContain('cat--other')
    expect(pill.text()).toBe('billing')
  })

  it('zeigt den Ladezustand, solange die Einträge geladen werden', async () => {
    global.fetch = vi.fn(() => new Promise(() => {}))
    const wrapper = mount(AdminAuditLog, {
      global: { plugins: [i18n], stubs: { OnyxInput: OnyxInputStub } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Lädt')
    expect(wrapper.find('.empty-state').exists()).toBe(false)
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    global.fetch = vi.fn(async () => jsonResponse({ error: 'kaputt' }, 500))
    const wrapper = mount(AdminAuditLog, {
      global: { plugins: [i18n], stubs: { OnyxInput: OnyxInputStub } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Audit-Log konnte nicht geladen werden')
  })

  it('filtert nach Kategorie, wenn ein Kategorie-Chip angeklickt wird', async () => {
    const wrapper = await mountView()
    global.fetch.mockClear()

    await wrapper.findAll('.chip').find(c => c.text() === 'Vorfall').trigger('click')
    await flushPromises()

    const calledUrl = global.fetch.mock.calls[0][0]
    expect(calledUrl).toContain('category=incident')
  })

  it('formatiert den Zeitstempel im englischen Format, wenn die UI-Sprache Englisch ist', async () => {
    i18n.global.locale.value = 'en'
    const wrapper = await mountView()

    const expected = new Date(1700000000 * 1000).toLocaleString('en-US', { dateStyle: 'short', timeStyle: 'short' })
    expect(wrapper.findAll('tbody tr')[0].text()).toContain(expected)
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = await mountView()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Richtung C: Brotkrumen, Listenkarte mit Suche und Filter im Kopf,
  // Zeit absolut mit relativer Angabe darunter.
  it('zeigt Brotkrumen, Karte mit Suche und Kategorie-Filter und relative Zeit', async () => {
    const wrapper = await mountView({ items: [{ ...entriesFixture()[0], created_at: Math.floor(Date.now() / 1000) - 3 * 86400 }] })

    expect(wrapper.find('.page-head__crumbs').text()).toContain('Administration')
    expect(wrapper.find('.page-head__crumbs [aria-current="page"]').text()).toBe('Audit-Log')
    expect(wrapper.find('section.comp-card h3').text()).toBe('Aktivitäten')
    expect(wrapper.find('.comp-card__head input[type="search"]').exists()).toBe(true)
    expect(wrapper.find('.comp-card__head .cat-chips').exists()).toBe(true)
    const time = wrapper.find('tbody time')
    expect(time.attributes('datetime')).toMatch(/^\d{4}-\d{2}-\d{2}T/)
    expect(time.find('.time-cell__rel').text()).toMatch(/3 Tagen/)
  })

  it('sucht über das Suchfeld der Karte mit Verzögerung und setzt auf Seite 1 zurück', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = await mountView()
      global.fetch.mockClear()

      await wrapper.find('input[type="search"]').setValue('admin')
      expect(global.fetch).not.toHaveBeenCalled()
      await vi.advanceTimersByTimeAsync(300)
      await flushPromises()

      expect(global.fetch).toHaveBeenCalledTimes(1)
      expect(global.fetch.mock.calls[0][0]).toContain('q=admin')
      expect(global.fetch.mock.calls[0][0]).toContain('page=1')
    } finally {
      vi.useRealTimers()
    }
  })

  it('blättert mit Weiter und Zurück und lädt die jeweilige Seite', async () => {
    const wrapper = await mountView({ total: 60 })
    expect(wrapper.find('.pager-info').text()).toContain('Seite 1 / 3')
    expect(wrapper.findAll('.pager-btn')[0].attributes('disabled')).toBeDefined()
    global.fetch.mockClear()

    await wrapper.findAll('.pager-btn')[1].trigger('click')
    await flushPromises()
    expect(global.fetch.mock.calls[0][0]).toContain('page=2')
    expect(wrapper.find('.pager-info').text()).toContain('Seite 2 / 3')

    await wrapper.findAll('.pager-btn')[0].trigger('click')
    await flushPromises()
    expect(global.fetch.mock.calls[1][0]).toContain('page=1')
  })

  it('zeigt für einen Eintrag ohne Zeitstempel einen Strich und keine relative Zeit', async () => {
    const wrapper = await mountView({ items: [{ ...entriesFixture()[0], created_at: 0 }] })

    const time = wrapper.find('tbody time')
    expect(time.attributes('datetime')).toBeUndefined()
    expect(time.find('.ent-mono').text()).toBe('—')
    expect(time.find('.time-cell__rel').text()).toBe('')
  })

  // Aktionen kamen als technische Schlüssel ("subscriber.deleted")
  // und Kategorien ohne Filter-Chip als Rohwert in die Tabelle.
  it('übersetzt bekannte Aktionsschlüssel und zeigt unbekannte unverändert', async () => {
    const wrapper = await mountView({ items: [
      { id: 1, created_at: 1700000000, actor_email: 'a@x.de', action: 'subscriber.deleted', target_name: 'x@y.de', category: 'subscriber', ip: '1.1.1.1' },
      { id: 2, created_at: 1700000000, actor_email: 'a@x.de', action: 'external_source.created', target_name: '', category: 'config', ip: '1.1.1.1' },
      { id: 3, created_at: 1700000000, actor_email: 'a@x.de', action: 'zukunft.neu', target_name: '', category: 'sonstige', ip: '1.1.1.1' },
      { id: 4, created_at: 1700000000, actor_email: 'a@x.de', action: 'notice.created', target_name: '', category: 'notice', ip: '1.1.1.1' },
    ] })

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('Abonnent gelöscht')
    expect(rows[0].text()).not.toContain('subscriber.deleted')
    expect(rows[0].find('.ent-pill').text()).toBe('Abonnent')
    expect(rows[1].text()).toContain('Externe Quelle angelegt')
    expect(rows[2].text()).toContain('zukunft.neu')
    expect(rows[2].find('.ent-pill').text()).toBe('sonstige')
    expect(rows[3].find('.ent-pill').text()).toBe('Ankündigung')
  })

  it('übersetzt die Wartungs-Kategorie und die zuletzt ergänzten Aktionen', async () => {
    const wrapper = await mountView({ items: [
      { id: 1, created_at: 1700000000, actor_email: '', action: 'maintenance.window_started', target_name: 'W', category: 'maintenance', ip: '' },
      { id: 2, created_at: 1700000000, actor_email: 'a@x.de', action: 'language.deleted', target_name: 'fr', category: 'config', ip: '' },
    ] })
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].find('.ent-pill').text()).toBe('Wartung')
    expect(rows[1].text()).toContain('Sprache gelöscht')
  })

  it('zeigt für Systemeinträge ohne Rolle die übersetzte Rolle statt "Automation"', async () => {
    const wrapper = await mountView({ items: [
      { id: 1, created_at: 1700000000, actor_email: '', actor_role: '', action: 'x.y', category: 'auth', ip: '' },
    ] })
    expect(wrapper.find('.a-role').text()).toBe('Automatisierung')
  })
  // Abonnent, Ankündigung und Wartung (und System) hatten keinen Filter-Chip.
  describe('Filter-Chips aller Kategorien', () => {
    const CHIPS_DE = ['Authentifizierung', 'Vorfall', 'Konfiguration', 'Nutzer', 'Abonnent', 'Ankündigung', 'Wartung', 'System']
    const CHIPS_EN = ['Authentication', 'Incident', 'Configuration', 'User', 'Subscriber', 'Notice', 'Maintenance', 'System']

    it('zeigt je Backend-Kategorie einen Chip, deutsch beschriftet', async () => {
      const wrapper = await mountView()
      expect(wrapper.findAll('.chip').map(c => c.text())).toEqual(CHIPS_DE)
    })

    it('beschriftet die Chips auf Englisch', async () => {
      i18n.global.locale.value = 'en'
      try {
        const wrapper = await mountView()
        expect(wrapper.findAll('.chip').map(c => c.text())).toEqual(CHIPS_EN)
      } finally {
        i18n.global.locale.value = 'de'
      }
    })

    it.each([
      ['Abonnent', 'subscriber'], ['Ankündigung', 'notice'], ['Wartung', 'maintenance'], ['System', 'system'],
    ])('Chip %s filtert per category=%s und lädt auf Seite 1', async (label, cat) => {
      const wrapper = await mountView()
      const chip = wrapper.findAll('.chip').find(c => c.text() === label)
      await chip.trigger('click')
      await flushPromises()
      const url = global.fetch.mock.calls.at(-1)[0]
      expect(url).toContain(`category=${cat}`)
      expect(url).toContain('page=1')
      expect(chip.attributes('aria-pressed')).toBe('true')
      expect(chip.classes()).toContain(`cat--${cat}`)
      // zweiter Klick hebt den Filter wieder auf
      await chip.trigger('click')
      await flushPromises()
      expect(global.fetch.mock.calls.at(-1)[0]).not.toContain('category=')
    })

    it('zeigt den Leerzustand, wenn die gefilterte Kategorie keine Einträge hat', async () => {
      const wrapper = await mountView()
      global.fetch = vi.fn(async () => jsonResponse({ items: [], total: 0 }))
      await wrapper.findAll('.chip').find(c => c.text() === 'Wartung').trigger('click')
      await flushPromises()
      expect(wrapper.text()).toContain('Keine Einträge gefunden.')
      expect(wrapper.find('table').exists()).toBe(false)
    })

    it('Pille und Chip jeder Kategorie sind übersetzt und nicht neutral', async () => {
      const items = ['subscriber', 'notice', 'maintenance', 'system'].map((category, n) =>
        ({ id: n + 1, created_at: 1700000000, actor_email: 'a@x.de', action: 'x.y', category, ip: '' }))
      const wrapper = await mountView({ items })
      const pills = wrapper.findAll('tbody .ent-pill')
      expect(pills.map(p => p.text())).toEqual(['Abonnent', 'Ankündigung', 'Wartung', 'System'])
      pills.forEach(p => expect(p.classes()).not.toContain('cat--other'))
    })
  })
})
