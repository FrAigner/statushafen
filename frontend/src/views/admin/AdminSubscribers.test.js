import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import { withArchivedComponent, componentTreeFixture, expectedTreeOptionLabels, multiSelectOptionLabels } from '../../test-support/componentTree'
import AdminSubscribers from './AdminSubscribers.vue'
import { axe } from '../../test-support/axe.js'
import { fieldByLabel, hasLabel } from '../../test-support/fields.js'
import { stubConfirm } from '../../test-support/confirm'

function subscribersFixture() {
  return [
    {
      ID: 1, Channel: 'email', Email: 'a@x.de', SubscribedComponentIDs: '10',
      CreatedBy: { Email: 'admin@x.de' },
    },
    {
      ID: 2, Channel: 'google_chat', WebhookURL: 'https://chat.googleapis.com/hook-1',
      SubscribedComponentIDs: '', CreatedBy: null,
    },
  ]
}
function componentsFixture() {
  // Baum mit Unterkomponente statt nur oberster Ebene.
  return componentTreeFixture()
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchRouter({
  subscribersOk = true, componentsOk = true, writeOk = true, writeStatus,
  subscribersFixtureOverride, testWebhookOk = true, testWebhookResponse, languages, languagesOk = true,
} = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/subscribers$/.test(url)) {
      return subscribersOk ? jsonResponse(subscribersFixtureOverride ?? subscribersFixture()) : jsonResponse({ error: 'kaputt' }, 500)
    }
    if (method === 'GET' && /\/components$/.test(url)) {
      return componentsOk ? jsonResponse(componentsFixture()) : jsonResponse({ error: 'kaputt' }, 500)
    }
    if (method === 'GET' && /\/languages$/.test(url)) {
      return languages && languagesOk ? jsonResponse(languages) : jsonResponse({ error: 'keine Sprachen' }, languagesOk ? 404 : 500)
    }
    if (method === 'POST' && /\/subscribers$/.test(url)) {
      return writeOk ? jsonResponse({ ID: 99 }) : jsonResponse({ error: 'Speichern kaputt' }, writeStatus || 500)
    }
    if (method === 'PUT' && /\/subscribers\/\d+$/.test(url)) {
      return writeOk ? jsonResponse({ ID: 1 }) : jsonResponse({ error: 'Speichern kaputt' }, writeStatus || 500)
    }
    if (method === 'DELETE' && /\/subscribers\/\d+$/.test(url)) {
      return writeOk ? { ok: true, status: 204 } : jsonResponse({ error: 'Löschen kaputt' }, writeStatus || 500)
    }
    if (method === 'POST' && /\/subscribe\/test-webhook$/.test(url)) {
      if (testWebhookResponse) return testWebhookResponse
      return testWebhookOk ? jsonResponse({}) : jsonResponse({ error: 'Webhook kaputt' }, 500)
    }
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/admin/subscribers', component: { template: '<div/>' } }],
  })
}

let activeWrapper = null

async function mountAdminSubscribers({ fetchImpl, tenantId = '1' } = {}) {
  const router = makeRouter()
  router.push('/admin/subscribers')
  await router.isReady()

  useTenant().setActiveTenant(tenantId)
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminSubscribers, {
    global: { plugins: [i18n, router], stubs: onyxFormStubs },
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper, router }
}

// Zeilenaktionen stehen im Zeilenmenü (RowMenu), die Einträge sind auch geschlossen im DOM.
const menuItem = (row, text) => row.findAll('[role="menuitem"]').find(i => i.text().includes(text))

describe('AdminSubscribers', () => {
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

  // vorher bot die Auswahl nur die oberste Ebene an.
  it('bietet in der Komponenten-Auswahl auch Unterkomponenten eingerückt an', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const { wrapper } = await mountAdminSubscribers()
    await wrapper.find('.page-head__actions button').trigger('click')

    expect(multiSelectOptionLabels(wrapper)).toEqual(expectedTreeOptionLabels)
  })

  // eine inzwischen archivierte Komponente eines bestehenden Abonnenten bleibt in der Auswahl sichtbar.
  it('zeigt beim Bearbeiten eine archivierte, gewählte Komponente weiter in der Auswahl', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const subscribersFixtureOverride = [{ ...subscribersFixture()[0], SubscribedComponentIDs: '13' }]
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: withArchivedComponent(makeFetchRouter({ subscribersFixtureOverride })) })
    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    expect(multiSelectOptionLabels(wrapper)).toContain('Altsystem')
    await wrapper.find('.page-head__actions button').trigger('click')
    expect(multiSelectOptionLabels(wrapper)).not.toContain('Altsystem')
  })

  it('lädt Abonnenten und Components, sobald eine Statusseite aktiv ist, und rendert die Tabelle', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })

    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/subscribers', expect.anything())
    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/components', expect.anything())

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('a@x.de')
    expect(rows[0].text()).toContain('E-Mail')
  })

  it('zeigt eine Fehlermeldung, wenn Abonnenten/Components nicht geladen werden können', async () => {
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ subscribersOk: false }) })

    expect(wrapper.find('.alert-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('Fehler beim Laden')
  })

  // unbestätigte Abos (Double-Opt-in) sind in der Liste markiert.
  it('markiert unbestätigte E-Mail-Abos mit "Unbestätigt", bestätigte nicht', async () => {
    const subs = subscribersFixture()
    subs.push({ ID: 3, Channel: 'email', Email: 'offen@x.de', SubscribedComponentIDs: '', Status: 'pending', CreatedBy: null })
    subs[0].Status = 'active'
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ subscribersFixtureOverride: subs }) })

    const rows = wrapper.findAll('tbody tr')
    expect(rows[2].find('.badge-pending').text()).toBe('Unbestätigt')
    expect(rows[2].find('.badge-pending').attributes('title')).toBe('Die Adresse hat das Abonnement noch nicht per Link bestätigt und erhält keine Benachrichtigungen.')
    expect(rows[0].find('.badge-pending').exists()).toBe(false)
    expect(rows[1].find('.badge-pending').exists()).toBe(false)
  })

  it('zeigt Komponentennamen statt roher IDs und "alle" bei leerer Auswahl', async () => {
    const { wrapper } = await mountAdminSubscribers()

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('API')
    expect(rows[1].text()).toContain('alle')
  })

  it('zeigt den Google-Chat-Kanal mit Webhook-URL und Test-Button statt E-Mail', async () => {
    const { wrapper } = await mountAdminSubscribers()

    const webhookRow = wrapper.findAll('tbody tr')[1]
    expect(webhookRow.text()).toContain('Google Chat Webhook')
    expect(webhookRow.text()).toContain('https://chat.googleapis.com/hook-1')
    expect(webhookRow.find('.action-btn').text()).toContain('Test senden')
  })

  it('sendet einen Webhook-Test und zeigt eine Erfolgsmeldung', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })

    await wrapper.findAll('tbody tr')[1].find('.action-btn').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/subscribe/test-webhook', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ webhook_url: 'https://chat.googleapis.com/hook-1' }),
    }))
    expect(wrapper.find('.alert-success').text()).toContain('Test-Nachricht erfolgreich gesendet.')
  })

  it('zeigt eine Fehlermeldung, wenn der Webhook-Test fehlschlägt', async () => {
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ testWebhookOk: false }) })

    await wrapper.findAll('tbody tr')[1].find('.action-btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Fehler beim Testen')
  })

  // Google drosselt (429 + retry_after) - verständliche Meldung, kein roher Servertext.
  it('zeigt bei einer Drosselung des Webhook-Ziels die Wartezeit', async () => {
    const response = jsonResponse({ error: 'Webhook-Ziel drosselt gerade', code: 'webhook_rate_limited', retry_after: 9 }, 429)
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ testWebhookResponse: response }) })

    await wrapper.findAll('tbody tr')[1].find('.action-btn').trigger('click')
    await flushPromises()

    const text = wrapper.find('.alert-error').text()
    expect(text).toContain('Google drosselt gerade – bitte in 9 Sekunden erneut versuchen.')
    expect(text).not.toContain('Webhook-Ziel drosselt gerade')
  })

  it('zeigt "+ Abonnent hinzufügen" und die Aktionen-Spalte nur mit subscribers:create/update/delete-Berechtigung', async () => {
    const { wrapper: withoutPerm } = await mountAdminSubscribers()
    expect(withoutPerm.text()).not.toContain('Abonnent hinzufügen')
    // Ohne Recht bleibt nur „Ansehen“ im Zeilenmenü (Details schreibgeschützt).
    expect([...new Set(withoutPerm.findAll('[role="menuitem"]').map(i => i.text()))]).toEqual(['Ansehen'])

    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const { wrapper: withPerm } = await mountAdminSubscribers()
    expect(withPerm.text()).toContain('Abonnent hinzufügen')
    expect(withPerm.find('.actions-col').exists()).toBe(true)
  })

  it('öffnet ein leeres Formular mit Kanal "email" über "+ Abonnent hinzufügen"', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const { wrapper } = await mountAdminSubscribers()

    await wrapper.find('.page-head__actions button').trigger('click')

    expect(wrapper.find('.edit-form').exists()).toBe(true)
    expect(hasLabel(wrapper, 'E-Mail-Adresse')).toBe(true)
    expect(hasLabel(wrapper, 'Webhook URL')).toBe(false)
  })

  it('wechselt zwischen E-Mail- und Webhook-Feld je nach gewähltem Kanal', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const { wrapper } = await mountAdminSubscribers()
    await wrapper.find('.page-head__actions button').trigger('click')

    const channelSelect = fieldByLabel(wrapper, 'Kanal')
    await channelSelect.setValue('google_chat')

    expect(hasLabel(wrapper, 'E-Mail-Adresse')).toBe(false)
    expect(hasLabel(wrapper, 'Webhook URL')).toBe(true)
  })

  it('befüllt das Formular beim Bearbeiten inkl. vorausgewählter Komponenten aus der CSV-Liste', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const { wrapper } = await mountAdminSubscribers()

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')

    expect(fieldByLabel(wrapper, 'E-Mail-Adresse').element.value).toBe('a@x.de')
    const checkedBoxes = wrapper.findAll('.onyx-select-stub-option input[type="checkbox"]')
      .filter(cb => cb.element.checked)
    expect(checkedBoxes).toHaveLength(1)
    expect(checkedBoxes[0].element.value).toBe('10')
  })

  it('wandelt die Multi-Select-Auswahl in eine kommaseparierte SubscribedComponentIDs-Liste um', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })

    await wrapper.find('.page-head__actions button').trigger('click')
    await fieldByLabel(wrapper, 'E-Mail-Adresse').setValue('neu@x.de')
    const checkboxes = wrapper.findAll('.onyx-select-stub-option input[type="checkbox"]')
    await checkboxes[0].setValue(true)
    // Index 1 ist die Unterkomponente 12, 2 ist "Web" (11).
    await checkboxes[2].setValue(true)

    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/subscribers', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ Channel: 'email', Language: '', Email: 'neu@x.de', SubscribedComponentIDs: '10,11' }),
    }))
    expect(wrapper.find('.alert-success').text()).toContain('Abonnent hinzugefügt.')
  })

  it('speichert eine Bearbeitung per PUT an die Subscriber-ID', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/subscribers/1', expect.objectContaining({ method: 'PUT' }))
    expect(wrapper.find('.alert-success').text()).toContain('Änderungen gespeichert.')
  })

  it('zeigt eine Fehlermeldung, wenn das Speichern fehlschlägt', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Speichern fehlgeschlagen')
    expect(wrapper.find('.edit-form').exists()).toBe(true)
  })

  it('löscht einen Abonnenten nach Bestätigung per DELETE und lädt neu', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const confirm = stubConfirm(true)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })

    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/subscribers/1', expect.objectContaining({ method: 'DELETE' }))
  })

  it('löscht nicht, wenn die Bestätigung abgelehnt wird', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    stubConfirm(false)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })
    fetchMock.mockClear()

    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalledWith(expect.anything(), expect.objectContaining({ method: 'DELETE' }))
  })

  it('zeigt eine Fehlermeldung, wenn das Löschen fehlschlägt', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    stubConfirm(true)
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Löschen fehlgeschlagen')
  })

  it('zeigt einen Leerzustand, wenn keine Abonnenten vorhanden sind', async () => {
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ subscribersFixtureOverride: [] }) })

    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.text()).toContain('Keine Abonnenten vorhanden.')
  })

  // Richtung C: Kartenkopf, Untertitel mit Zählung, Suche, Fehler am Feld.
  it('zeigt Kartenkopf und Untertitel mit Zählung', async () => {
    const subs = subscribersFixture()
    subs.push({ ID: 3, Channel: 'email', Email: 'offen@x.de', SubscribedComponentIDs: '', Status: 'pending', CreatedBy: null })
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ subscribersFixtureOverride: subs }) })
    expect(wrapper.find('.comp-card__title').text()).toBe('Alle Abonnenten')
    expect(wrapper.find('.page-head__subtitle').text()).toBe('3 Abonnenten · 1 unbestätigt')
  })

  it('sucht nach Kontakt; ohne Treffer erscheint der Leerzustand statt der Tabelle', async () => {
    const { wrapper } = await mountAdminSubscribers()
    await wrapper.find('input[type="search"]').setValue('hook-1')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('[role="status"]').text()).toBe('1 Treffer')
    await wrapper.find('input[type="search"]').setValue('niemand')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.empty-state').text()).toContain('Kein Abonnent gefunden.')
  })

  it('zeigt „Hinzugefügt“ relativ und den Status „Unbestätigt“ mit Symbol und Text', async () => {
    const recent = new Date(Date.now() - 3 * 3600 * 1000).toISOString()
    const subs = [{ ID: 3, Channel: 'email', Email: 'offen@x.de', SubscribedComponentIDs: '', Status: 'pending', CreatedBy: null, CreatedAt: recent }]
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ subscribersFixtureOverride: subs }) })
    const row = wrapper.find('tbody tr')
    expect(row.find('time').exists()).toBe(true)
    expect(row.find('.badge-pending svg').exists()).toBe(true)
    expect(row.find('.badge-pending').text()).toBe('Unbestätigt')
  })

  // Regression: ein leerer Kontakt ging bisher als Serveranfrage raus.
  it('meldet fehlende E-Mail bzw. Webhook-URL am Feld, fokussiert es und sendet nichts', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })
    await wrapper.find('.page-head__actions button').trigger('click')
    const before = fetchMock.mock.calls.length
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(fieldByLabel(wrapper, 'E-Mail-Adresse').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('aside [role="alert"]').text()).toContain('E-Mail-Adresse')

    await fieldByLabel(wrapper, 'Kanal').setValue('google_chat')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('aside [role="alert"]').text()).toContain('Webhook-URL')
    expect(fetchMock.mock.calls.length).toBe(before)
  })

  it('schließt das Formular mit dem ×-Knopf', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const { wrapper } = await mountAdminSubscribers()
    await wrapper.find('.page-head__actions button').trigger('click')
    await wrapper.find('.form-panel__close').trigger('click')
    expect(wrapper.find('aside.form-panel').exists()).toBe(false)
  })

  it('nennt beim Bearbeiten eines Webhook-Abos die URL im Formulartitel', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const { wrapper } = await mountAdminSubscribers()
    await menuItem(wrapper.findAll('tbody tr')[1], 'Bearbeiten').trigger('click')
    expect(wrapper.find('aside.form-panel h3').text()).toBe('Abonnent „https://chat.googleapis.com/hook-1“ bearbeiten')
  })

  it('hat keine axe-Funde in Liste und Bearbeitungsformular', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const { wrapper } = await mountAdminSubscribers()
    expect(await axe(wrapper)).toHaveNoViolations()

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('zeigt Liste und Formular nebeneinander statt die Liste zu ersetzen', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const { wrapper } = await mountAdminSubscribers()
    expect(wrapper.find('.split').classes()).not.toContain('split--open')

    await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')

    expect(wrapper.find('.split').classes()).toContain('split--open')
    expect(wrapper.find('table.data-table').exists()).toBe(true)
    expect(wrapper.find('aside.form-panel').attributes('aria-label')).toBe('Abonnent „a@x.de“ bearbeiten')
    expect(await axe(wrapper)).toHaveNoViolations()
    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen').trigger('click')
    expect(wrapper.find('aside.form-panel').exists()).toBe(false)
  })

  it('zeigt den Leerzustand als EmptyState und das Webhook-Ziel gekürzt ohne Inline-Style', async () => {
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ subscribersFixtureOverride: [] }) })
    expect(wrapper.find('.comp-card .empty-state__title').exists()).toBe(true)

    const { wrapper: withHook } = await mountAdminSubscribers()
    expect(withHook.findAll('tbody tr')[1].find('.cell-truncate').text()).toContain('chat.googleapis.com')
  })

  it('legt einen Google-Chat-Abonnenten mit Webhook-URL an', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })

    await wrapper.find('.page-head__actions button').trigger('click')
    await fieldByLabel(wrapper, 'Kanal').setValue('google_chat')
    await fieldByLabel(wrapper, 'Webhook URL').setValue('https://chat.googleapis.com/neu')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    const [, options] = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
    expect(JSON.parse(options.body)).toMatchObject({ Channel: 'google_chat', WebhookURL: 'https://chat.googleapis.com/neu' })
  })
  // Nach dem Löschen blieb „Abonnent hinzugefügt.“ stehen.
  it('meldet das Löschen und entfernt die ältere Meldung', async () => {
    setAuthAsPageRole('jwt-1', { permissions: 'subscribers:create,subscribers:update,subscribers:delete' })
    stubConfirm(true)
    const { wrapper } = await mountAdminSubscribers()
    wrapper.vm.showAlert('Abonnent hinzugefügt.', 'success')
    await flushPromises()

    await menuItem(wrapper.find('tbody tr'), 'Löschen').trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Abonnent hinzugefügt.')
    expect(wrapper.find('.alert-success').text()).toContain('Abonnent gelöscht.')
  })
  // Mailsprache im Admin-Formular.
  describe('Mailsprache', () => {
    const twoLanguages = [
      { LanguageCode: 'de', IsDefault: true, Language: { Name: 'Deutsch' } },
      { LanguageCode: 'en', IsDefault: false, Language: { Name: 'English' } },
    ]
    const perms = 'subscribers:create,subscribers:update,subscribers:delete'

    it('bietet die Sprachen der Seite an und sendet die Wahl beim Anlegen', async () => {
      setAuthAsPageRole('jwt-1', { permissions: perms })
      const fetchMock = makeFetchRouter({ languages: twoLanguages })
      const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })
      expect(fetchMock).toHaveBeenCalledWith('/api/pages/1/languages', expect.anything())

      await wrapper.find('.page-head__actions button').trigger('click')
      const select = fieldByLabel(wrapper, 'Sprache der Benachrichtigungen')
      expect(select.findAll('option').map(o => o.text())).toEqual(['Seitenstandard (Deutsch)', 'English'])
      await select.setValue('en')
      await fieldByLabel(wrapper, 'E-Mail-Adresse').setValue('neu@x.de')
      await wrapper.find('.edit-form').trigger('submit')
      await flushPromises()

      const [, options] = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
      expect(JSON.parse(options.body)).toMatchObject({ Channel: 'email', Email: 'neu@x.de', Language: 'en' })
    })

    it('zeigt beim Bearbeiten die gespeicherte Sprache und ändert sie per PUT', async () => {
      setAuthAsPageRole('jwt-1', { permissions: perms })
      const fetchMock = makeFetchRouter({
        languages: twoLanguages,
        subscribersFixtureOverride: [{ ID: 1, Channel: 'email', Email: 'a@x.de', SubscribedComponentIDs: '10', Language: 'en', CreatedBy: null }],
      })
      const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })

      await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')
      const select = fieldByLabel(wrapper, 'Sprache der Benachrichtigungen')
      expect(select.element.value).toBe('en')
      await select.setValue('')
      await wrapper.find('.edit-form').trigger('submit')
      await flushPromises()

      const [, options] = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
      expect(JSON.parse(options.body).Language).toBe('')
    })

    it('bietet im Katalog deaktivierte Sprachen nicht an', async () => {
      setAuthAsPageRole('jwt-1', { permissions: perms })
      const langs = [...twoLanguages, { LanguageCode: 'hr', IsDefault: false, Language: { Name: 'Hrvatski', IsActive: false } }]
      const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ languages: langs }) })
      await wrapper.find('.page-head__actions button').trigger('click')

      const options = fieldByLabel(wrapper, 'Sprache der Benachrichtigungen').findAll('option').map(o => o.text())
      expect(options).toEqual(['Seitenstandard (Deutsch)', 'English'])
    })

    it('neues Formular startet mit Seitenstandard (Wert leer) und sendet Language leer', async () => {
      setAuthAsPageRole('jwt-1', { permissions: perms })
      const fetchMock = makeFetchRouter({ languages: twoLanguages })
      const { wrapper } = await mountAdminSubscribers({ fetchImpl: fetchMock })
      await wrapper.find('.page-head__actions button').trigger('click')

      const select = fieldByLabel(wrapper, 'Sprache der Benachrichtigungen')
      expect(select.element.value).toBe('')
      expect(select.element.selectedOptions[0].text).toBe('Seitenstandard (Deutsch)')
      await fieldByLabel(wrapper, 'E-Mail-Adresse').setValue('neu@x.de')
      await wrapper.find('.edit-form').trigger('submit')
      await flushPromises()
      const [, options] = fetchMock.mock.calls.find(([, o]) => o?.method === 'POST')
      expect(JSON.parse(options.body).Language).toBe('')
    })

    it('zeigt bei einem Abo ohne gespeicherte Sprache den Seitenstandard', async () => {
      setAuthAsPageRole('jwt-1', { permissions: perms })
      const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter({ languages: twoLanguages }) })

      await menuItem(wrapper.find('tbody tr'), 'Bearbeiten').trigger('click')

      expect(fieldByLabel(wrapper, 'Sprache der Benachrichtigungen').element.value).toBe('')
    })

    it('blendet die Sprachwahl bei nur einer Sprache oder ohne Sprachliste aus', async () => {
      setAuthAsPageRole('jwt-1', { permissions: perms })
      for (const opts of [{ languages: [twoLanguages[0]] }, { languagesOk: false }, {}]) {
        const { wrapper } = await mountAdminSubscribers({ fetchImpl: makeFetchRouter(opts) })
        await wrapper.find('.page-head__actions button').trigger('click')
        expect(hasLabel(wrapper, 'Sprache der Benachrichtigungen')).toBe(false)
        // Die Liste lädt trotz fehlender Sprachliste, ohne Fehlermeldung.
        expect(wrapper.findAll('tbody tr')).toHaveLength(2)
        expect(wrapper.find('.alert-error').exists()).toBe(false)
        activeWrapper.unmount()
      }
    })
  })
})
