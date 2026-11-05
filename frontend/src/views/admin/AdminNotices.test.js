import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminNotices from './AdminNotices.vue'
import { axe } from '../../test-support/axe.js'
import { fieldByLabel } from '../../test-support/fieldByLabel.js'
import { clickMenuItem } from '../../test-support/rowMenu.js'
import { expectNoInlineStyles } from '../../test-support/noInlineStyles.js'
import { stubConfirm } from '../../test-support/confirm'

function noticesFixture() {
  return [
    {
      ID: 1, Title: 'Geplante Wartung', Message: 'Details folgen', Level: 'warning', IsActive: true,
      StartAt: '2026-08-01T10:00:00Z', EndAt: '2026-08-01T12:00:00Z', CreatedAt: '2026-07-30T09:00:00Z',
    },
  ]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchImpl({ ok = true, writeOk = true } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET') {
      return ok ? jsonResponse(noticesFixture()) : jsonResponse({ error: 'kaputt' }, 500)
    }
    return writeOk ? jsonResponse({}) : jsonResponse({ error: 'Schreiben fehlgeschlagen' }, 500)
  })
}

let activeWrapper = null

async function mountAdminNotices({ fetchImpl, tenantId = '1' } = {}) {
  useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: true })
  useTenant().setActiveTenant(tenantId)
  global.fetch = fetchImpl || makeFetchImpl()

  const wrapper = mount(AdminNotices, {
    global: { plugins: [i18n], stubs: { ...onyxFormStubs, RouterLink: RouterLinkStub } },
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminNotices', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    // Tests assertieren auf den deutschen Wortlaut übersetzter Texte;
    // ohne gespeicherte Präferenz würde detectInitialLocale() in der Testumgebung
    // (happy-dom navigator.language === 'en-US') sonst auf 'en' fallen.
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('verlinkt je Ankündigung die Übersetzungsseite', async () => {
    const { wrapper } = await mountAdminNotices()

    const link = wrapper.findComponent(RouterLinkStub)
    expect(link.props('to')).toBe('/admin/notices/1/translations')
    expect(link.text()).toContain('Übersetzungen')
  })

  it('lädt Ankündigungen und formatiert Zeitfenster + Erstellt-Zeitpunkt im deutschen Format', async () => {
    const { wrapper } = await mountAdminNotices()

    const row = wrapper.find('tbody tr')
    expect(row.text()).toContain('Geplante Wartung')
    const expectedStart = new Date('2026-08-01T10:00:00Z').toLocaleString('de-DE', { dateStyle: 'short', timeStyle: 'short' })
    const expectedCreated = new Date('2026-07-30T09:00:00Z').toLocaleString('de-DE', { dateStyle: 'short', timeStyle: 'short' })
    expect(row.text()).toContain(expectedStart)
    // Erstellt steht relativ („vor …“), der genaue Zeitpunkt im title.
    expect(row.find('time').attributes('title')).toBe(expectedCreated)
  })

  it('formatiert Zeitfenster + Erstellt-Zeitstempel im englischen Format, wenn die UI-Sprache Englisch ist', async () => {
    i18n.global.locale.value = 'en'
    const { wrapper } = await mountAdminNotices()

    const expectedStart = new Date('2026-08-01T10:00:00Z').toLocaleString('en-US', { dateStyle: 'short', timeStyle: 'short' })
    expect(wrapper.find('tbody tr').text()).toContain(expectedStart)
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const { wrapper } = await mountAdminNotices({ fetchImpl: makeFetchImpl({ ok: false }) })

    expect(wrapper.find('.alert-error').exists()).toBe(true)
  })

  // Laden -> Fehlerfall -> Speichern/Löschen werden ausdrücklich und mit
  // flushPromises abgewartet, damit die Coverage nicht von zufällig noch laufender
  // asynchroner Arbeit anderer Tests abhängt.
  it('legt eine neue Ankündigung per POST an und lädt die Liste neu', async () => {
    const fetchMock = makeFetchImpl()
    const { wrapper } = await mountAdminNotices({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('Neu')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Text')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const post = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
    expect(post[0]).toBe('/api/pages/1/notices')
    expect(JSON.parse(post[1].body)).toMatchObject({ Level: 'info', StartAt: null, EndAt: null })
    expect(wrapper.find('.alert-success').exists()).toBe(true)
    expect(wrapper.find('.form-panel').exists()).toBe(false)
    // Liste einmal beim Öffnen, einmal nach dem Speichern (Sprachabfragen für den Umschalter zählen nicht)
    expect(fetchMock.mock.calls.filter(([u, o]) => !o?.method && u.endsWith('/notices/all')).length).toBe(2)
  })

  it('bearbeitet eine Ankündigung per PUT mit ISO-Zeitfenster und kann abbrechen', async () => {
    const fetchMock = makeFetchImpl()
    const { wrapper } = await mountAdminNotices({ fetchImpl: fetchMock })

    await clickMenuItem(wrapper, 'Bearbeiten')
    expect(wrapper.find('.form-panel').exists()).toBe(true)
    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen').trigger('click')
    expect(wrapper.find('.form-panel').exists()).toBe(false)

    await clickMenuItem(wrapper, 'Bearbeiten')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const put = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
    expect(put[0]).toBe('/api/pages/1/notices/1')
    expect(JSON.parse(put[1].body).StartAt).toMatch(/^2026-08-01T/)
    expect(wrapper.find('.alert-success').exists()).toBe(true)
  })

  it('zeigt eine Fehlermeldung, wenn das Speichern fehlschlägt', async () => {
    const { wrapper } = await mountAdminNotices({ fetchImpl: makeFetchImpl({ writeOk: false }) })

    await clickMenuItem(wrapper, 'Bearbeiten')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Schreiben fehlgeschlagen')
    expect(wrapper.find('.form-panel').exists()).toBe(true)
  })

  it('löscht nach Bestätigung per DELETE, bei Ablehnung nicht, und meldet Fehler', async () => {
    const fetchMock = makeFetchImpl()
    const { wrapper } = await mountAdminNotices({ fetchImpl: fetchMock })

    stubConfirm(false)
    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'DELETE')).toBe(false)

    stubConfirm(true)
    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/notices/1', expect.objectContaining({ method: 'DELETE' }))

    global.fetch = makeFetchImpl({ writeOk: false })
    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()
    expect(wrapper.find('.alert-error').text()).toContain('Schreiben fehlgeschlagen')
    await wrapper.find('.alert-close').trigger('click')
    expect(wrapper.find('.alert').exists()).toBe(false)
  })

  it('zeigt "unbegrenzt" ohne Zeitfenster, neutrales Badge bei unbekannter Stufe und den Leer-Hinweis', async () => {
    const items = [{ ID: 5, Title: 'Offen', Message: 'x', Level: 'unbekannt', IsActive: false, CreatedAt: '2026-07-30T09:00:00Z' }]
    const { wrapper } = await mountAdminNotices({ fetchImpl: vi.fn(async () => jsonResponse(items)) })

    expect(wrapper.find('tbody tr').text()).toContain('unbegrenzt')
    const badge = wrapper.find('tbody .st-badge')
    expect(badge.text()).toBe('unbekannt')
    expect(badge.classes()).toContain('st-badge--unknown')

    wrapper.unmount()
    const empty = await mountAdminNotices({ fetchImpl: vi.fn(async () => jsonResponse([])) })
    expect(empty.wrapper.find('.empty-state__title').text()).toBe('Keine Ankündigungen vorhanden.')
    expect(empty.wrapper.find('table').exists()).toBe(false)
  })

  it('hat keine axe-Funde in Liste und Bearbeitungsformular', async () => {
    const { wrapper } = await mountAdminNotices()
    expect(await axe(wrapper)).toHaveNoViolations()

    await clickMenuItem(wrapper, 'Bearbeiten')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
  it('Seitentitel ist eine h2 (PageHeader level 2, die Topbar trägt die h1), Karten- und Formulartitel eine h3', async () => {
    const { wrapper } = await mountAdminNotices()

    expect(wrapper.findAll('h2.page-head__title').map(h => h.text())).toEqual(['Ankündigungen (Notices)'])
    expect(wrapper.find('h1').exists()).toBe(false)

    await wrapper.find('.page-head button').trigger('click')
    expect(wrapper.findAll('h2.page-head__title')).toHaveLength(1)
    expect(wrapper.find('.form-panel h3').text()).toBe('Neue Ankündigung anlegen')
    expect(wrapper.find('.comp-card h3').text()).toBe('Alle Ankündigungen')
    expect(wrapper.find('.page-header').exists()).toBe(false)
  })

  it.each([
    ['info', 'Info', 'st-badge--maintenance'],
    ['warning', 'Warnung', 'st-badge--degraded'],
    ['critical', 'Kritisch', 'st-badge--major'],
  ])('zeigt die Stufe %s als StatusBadge „%s“ statt des Rohwerts', async (Level, label, cls) => {
    const items = [{ ID: 5, Title: 'T', Message: 'x', Level, IsActive: true, CreatedAt: '2026-07-30T09:00:00Z' }]
    const { wrapper } = await mountAdminNotices({ fetchImpl: vi.fn(async () => jsonResponse(items)) })

    const badge = wrapper.find('tbody .st-badge')
    expect(badge.text()).toBe(label)
    expect(badge.classes()).toContain(cls)
    expect(wrapper.find('.badge').exists()).toBe(false)
    expect(wrapper.find('td[data-label="Aktiv"]').text()).toBe('Aktiv')
  })

  it('nutzt den gemeinsamen Tabellenstil, zeigt „Inaktiv“ für inaktive Ankündigungen', async () => {
    const items = [{ ID: 5, Title: 'T', Message: 'x', Level: 'info', IsActive: false, CreatedAt: '2026-07-30T09:00:00Z' }]
    const { wrapper } = await mountAdminNotices({ fetchImpl: vi.fn(async () => jsonResponse(items)) })

    expect(wrapper.find('.data-table-wrapper table.data-table').exists()).toBe(true)
    expect(wrapper.find('td[data-label="Aktiv"]').text()).toBe('Inaktiv')
  })

  it('beschriftet das Formular über FormField: Pflicht mit Stern, Zeitfenster „optional“', async () => {
    const { wrapper } = await mountAdminNotices()
    await wrapper.find('.page-head button').trigger('click')

    const required = wrapper.findAll('.form-field').filter(f => f.find('.form-field__required').exists())
    expect(required.map(f => f.find('label').text())).toEqual(['Titel', 'Nachricht / Details', 'Stufe'])
    const optional = wrapper.findAll('.form-field').filter(f => f.find('.form-field__optional').exists())
    expect(optional.map(f => f.find('label').text())).toEqual(['Beginn', 'Ende'])
    expect(fieldByLabel(wrapper, 'Stufe').findAll('option').map(o => o.text())).toEqual(['Info', 'Warnung', 'Kritisch'])
  })

  // Die Nachricht wird formatiert dargestellt, das Feld ist der
  // Editor; der Platzhalter sagte vorher „Markdown wird nicht dargestellt“.
  it.each([
    ['de', 'Werkzeugleiste'],
    ['en', 'toolbar'],
  ])('Nachrichtenfeld ist der Editor mit passendem Hinweis (%s)', async (locale, hint) => {
    i18n.global.locale.value = locale
    const { wrapper } = await mountAdminNotices()
    await wrapper.find('.page-head button').trigger('click')

    expect(wrapper.find('.rte [role="toolbar"]').exists()).toBe(true)
    const placeholder = wrapper.find('.rte textarea').attributes('placeholder')
    expect(placeholder).toContain(hint)
    expect(placeholder).not.toMatch(/nicht dargestellt|not rendered/)
  })

  it('sendet die gewählte Stufe als Rohwert', async () => {
    const fetchMock = makeFetchImpl()
    const { wrapper } = await mountAdminNotices({ fetchImpl: fetchMock })
    await wrapper.find('.page-head button').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('T')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('M')
    await fieldByLabel(wrapper, 'Stufe').setValue('critical')
    await fieldByLabel(wrapper, 'Beginn').setValue('2026-10-02T08:00')
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const post = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
    const body = JSON.parse(post[1].body)
    expect(body.Level).toBe('critical')
    expect(body.StartAt).toBe(new Date('2026-10-02T08:00').toISOString())
  })

  it('zeigt Lade- und Fehlerzustand statt eines falschen Leerzustands', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: true })
    useTenant().setActiveTenant('1')
    global.fetch = vi.fn(() => new Promise(() => {}))
    const loadingWrapper = mount(AdminNotices, { global: { plugins: [i18n], stubs: { ...onyxFormStubs, RouterLink: RouterLinkStub } } })
    await flushPromises()
    expect(loadingWrapper.find('[role="status"][aria-busy="true"]').exists()).toBe(true)
    expect(loadingWrapper.text()).not.toContain('Keine Ankündigungen vorhanden.')
    loadingWrapper.unmount()

    const { wrapper } = await mountAdminNotices({ fetchImpl: makeFetchImpl({ ok: false }) })
    expect(wrapper.find('[role="alert"]').text()).toBe('Die Liste konnte nicht geladen werden.')
    expect(wrapper.text()).not.toContain('Keine Ankündigungen vorhanden.')
  })

  it('behandelt eine null-Antwort als leere Liste', async () => {
    const { wrapper } = await mountAdminNotices({ fetchImpl: vi.fn(async () => jsonResponse(null)) })

    expect(wrapper.find('.empty-state__title').text()).toBe('Keine Ankündigungen vorhanden.')
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('hat keine Inline-Styles und keine Hex-Farben', async () => {
    const { wrapper } = await mountAdminNotices()
    expectNoInlineStyles(wrapper, import.meta.url, 'AdminNotices.vue')

    await clickMenuItem(wrapper, 'Bearbeiten')
    expectNoInlineStyles(wrapper, import.meta.url, 'AdminNotices.vue')
  })
  it('übernimmt Nachricht, Schalter und Ende-Zeitpunkt ins Speichern', async () => {
    const fetchMock = makeFetchImpl()
    const { wrapper } = await mountAdminNotices({ fetchImpl: fetchMock })
    await wrapper.find('.page-head button').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('T')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Text')
    await fieldByLabel(wrapper, 'Ende').setValue('2026-10-03T08:00')
    const boxes = wrapper.findAll('.notice-options input[type="checkbox"]')
    expect(boxes).toHaveLength(3)
    await boxes[0].setValue(false)
    await boxes[1].setValue(true)
    await boxes[2].setValue(true)
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const body = JSON.parse(fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')[1].body)
    expect(body).toMatchObject({ Message: 'Text', IsActive: false, Collapsed: true, NotifySubscribers: true })
    expect(body.EndAt).toBe(new Date('2026-10-03T08:00').toISOString())
  })

  it('zeigt während des Löschens „Löschen…“ und sperrt die Schaltfläche', async () => {
    stubConfirm(true)
    const base = makeFetchImpl()
    const fetchMock = vi.fn((url, o = {}) => (o.method === 'DELETE' ? new Promise(() => {}) : base(url, o)))
    const { wrapper } = await mountAdminNotices({ fetchImpl: fetchMock })

    await clickMenuItem(wrapper, 'Löschen')
    await flushPromises()
    const item = wrapper.find('[role="menuitem"]:last-child')
    expect(item.text()).toContain('Löschen…')
    expect(item.attributes('aria-disabled')).toBe('true')
  })
  describe('Richtung C', () => {
    const notices = () => [
      { ID: 1, Title: 'Wartung Sonntag', Message: 'Details folgen', Level: 'warning', IsActive: true, CreatedAt: '2026-07-30T09:00:00Z' },
      { ID: 2, Title: 'Altes Release', Message: 'Archiv', Level: 'info', IsActive: false, CreatedAt: '2026-07-29T09:00:00Z' },
    ]

    it('zeigt Untertitel mit Zählung, Erstellt relativ und das Zeilenmenü mit Übersetzungs-Link', async () => {
      const { wrapper } = await mountAdminNotices({ fetchImpl: vi.fn(async () => jsonResponse(notices())) })
      expect(wrapper.find('.page-head__subtitle').text()).toBe('2 Ankündigungen · 1 aktiv')
      expect(wrapper.find('tbody time').attributes('datetime')).toBe('2026-07-30T09:00:00Z')
      expect(wrapper.find('thead').text()).not.toContain('ID')
      expect(wrapper.find('tbody .action-btn').exists()).toBe(false)
      expect(wrapper.findAll('tbody tr')[0].findAll('[role="menuitem"]').map(i => i.text())).toEqual(['Bearbeiten', 'Übersetzungen', 'Löschen'])
    })

    it('sucht und filtert nach Aktiv/Inaktiv', async () => {
      const { wrapper } = await mountAdminNotices({ fetchImpl: vi.fn(async () => jsonResponse(notices())) })
      const seg = (text) => wrapper.findAll('.segment__item').find(b => b.text() === text)
      await seg('Inaktiv').trigger('click')
      expect(wrapper.findAll('tbody tr').map(r => r.text())).toEqual([expect.stringContaining('Altes Release')])
      await seg('Alle').trigger('click')
      await wrapper.find('input[type="search"]').setValue('sonntag')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
      await wrapper.find('input[type="search"]').setValue('gibtesnicht')
      expect(wrapper.find('.empty-state__title').text()).toBe('Keine Ankündigung gefunden.')
    })

    it('Formular: Titel mit Namen, Pflichtfelder mit Fehler am Feld und ohne Request', async () => {
      const fetchMock = makeFetchImpl()
      const { wrapper } = await mountAdminNotices({ fetchImpl: fetchMock })
      await clickMenuItem(wrapper, 'Bearbeiten')
      expect(wrapper.find('.form-panel h3').text()).toMatch(/^Ankündigung „.+“ bearbeiten$/)
      await wrapper.find('.form-panel__close').trigger('click')

      await wrapper.find('.page-head button').trigger('click')
      await wrapper.find('form.edit-form').trigger('submit.prevent')
      await flushPromises()
      expect(wrapper.findAll('.form-field__message--error')).toHaveLength(2)
      expect(fieldByLabel(wrapper, 'Titel').attributes('aria-invalid')).toBe('true')
      expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)
    })

    it('sucht auch in Ankündigungen ohne Nachricht', async () => {
      const items = [{ ID: 3, Title: 'Nur Titel', Message: null, Level: 'info', IsActive: true, CreatedAt: '2026-07-30T09:00:00Z' }]
      const { wrapper } = await mountAdminNotices({ fetchImpl: vi.fn(async () => jsonResponse(items)) })
      await wrapper.find('input[type="search"]').setValue('titel')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
      await wrapper.find('input[type="search"]').setValue('null')
      expect(wrapper.findAll('tbody tr')).toHaveLength(0)
    })
  })
})
