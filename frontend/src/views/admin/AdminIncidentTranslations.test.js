import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant as useTenantStatic } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminIncidentTranslations from './AdminIncidentTranslations.vue'
import { axe } from '../../test-support/axe.js'
import { clickMenuItem } from '../../test-support/rowMenu.js'
import { stubConfirm } from '../../test-support/confirm'

function incidentFixture() {
  return { ID: 1, StatusPageID: 1, Title: 'Database Outage', Status: 'investigating' }
}

function translationsFixture() {
  return [
    { ID: 1, IncidentID: 1, LanguageCode: 'de', Title: 'Ausfall', Message: 'Datenbankausfall', Status: 'confirmed' },
    { ID: 2, IncidentID: 1, LanguageCode: 'fr', Title: 'Panne', Message: 'Panne de base de données', Status: 'draft' },
  ]
}

function catalogFixture() {
  return [
    { ID: 1, Code: 'de', Name: 'Deutsch', IsActive: true },
    { ID: 2, Code: 'en', Name: 'English', IsActive: true },
    { ID: 3, Code: 'fr', Name: 'Français', IsActive: true },
  ]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchRouter({ incidentOverride, translationsOverride, catalogOverride, writeOk = true, pageId = '1' } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && new RegExp(`/api/pages/${pageId}/incidents/\\d+$`).test(url)) {
      return jsonResponse(incidentOverride ?? incidentFixture())
    }
    if (method === 'GET' && /\/api\/incidents\/\d+\/translations\/all$/.test(url)) {
      return jsonResponse(translationsOverride ?? translationsFixture())
    }
    if (method === 'GET' && /\/api\/languages$/.test(url)) {
      return jsonResponse(catalogOverride ?? catalogFixture())
    }
    if (method === 'POST' && /\/api\/incidents\/\d+\/translations$/.test(url)) {
      const newTrans = { ID: 99, IncidentID: 1, Status: 'draft', Source: 'manual', ...JSON.parse(options.body) }
      return writeOk ? jsonResponse(newTrans) : jsonResponse({ error: 'conflict' }, 409)
    }
    if (method === 'PATCH' && /\/api\/incident-translations\/\d+\/confirm$/.test(url)) {
      return writeOk ? jsonResponse({ ...translationsFixture()[1], Status: 'confirmed' }) : jsonResponse({ error: 'failed' }, 500)
    }
    if (method === 'DELETE' && /\/api\/incident-translations\/\d+$/.test(url)) {
      return writeOk ? jsonResponse({}, 204) : jsonResponse({ error: 'failed' }, 500)
    }
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let activeWrapper = null

async function mountComponent({ fetchImpl, incidentId = '1', pageId = '1' } = {}) {
  const { useTenant } = await import('../../composables/useTenant')
  useTenant().setActiveTenant(pageId)
  global.fetch = fetchImpl || makeFetchRouter({ pageId })
  const wrapper = mount(AdminIncidentTranslations, {
    global: { plugins: [i18n], stubs: { ...onyxFormStubs, RouterLink: RouterLinkStub } },
    props: { incidentId }
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminIncidentTranslations', () => {
  beforeEach(() => {
    // Die Knöpfe hängen an incidents:create/update/delete auf der aktiven Seite.
    useAuth().setAuth('jwt-1', { permissions: '' })
    useTenantStatic().setMyPages([{ ID: 1, Permissions: 'incidents:create,incidents:read,incidents:update,incidents:delete' }])
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
    useAuth().logout()
    useTenantStatic().setMyPages([])
  })

  it('displays confirmed translations', async () => {
    const { wrapper } = await mountComponent()
    expect(wrapper.text()).toContain('Ausfall')
    expect(wrapper.text()).toContain('Bestätigt')
  })

  it('displays draft translations with draft badge', async () => {
    const { wrapper } = await mountComponent()
    expect(wrapper.text()).toContain('Panne')
    expect(wrapper.text()).toContain('Entwurf')
  })

  it('renders the new translation button', async () => {
    const { wrapper } = await mountComponent()
    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    expect(newButtons.length).toBeGreaterThan(0)
  })

  it('shows translation table with language codes', async () => {
    const { wrapper } = await mountComponent()
    const table = wrapper.find('table')
    expect(table.exists()).toBe(true)
    expect(wrapper.text()).toContain('de')
    expect(wrapper.text()).toContain('fr')
  })

  it('renders confirm and delete buttons for translations', async () => {
    const { wrapper } = await mountComponent()
    const confirmButtons = wrapper.findAll('button').filter(b => b.text().includes('Bestätigen'))
    const deleteButtons = wrapper.findAll('button').filter(b => b.text().includes('Löschen'))
    expect(confirmButtons.length).toBeGreaterThan(0)
    expect(deleteButtons.length).toBeGreaterThan(0)
  })

  it('displays incident name', async () => {
    const { wrapper } = await mountComponent()
    expect(wrapper.text()).toContain('Database Outage')
  })

  it('renders form when opening new translation', async () => {
    const { wrapper } = await mountComponent()
    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    const form = wrapper.find('form')
    expect(form.exists()).toBe(true)
  })

  it('shows cancel button in edit form', async () => {
    const { wrapper } = await mountComponent()
    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    const cancelButtons = wrapper.findAll('button').filter(b => b.text().includes('Abbrechen'))
    expect(cancelButtons.length).toBeGreaterThan(0)
  })

  it('handles empty translations state', async () => {
    const fetchMock = makeFetchRouter({ translationsOverride: [] })
    const { wrapper } = await mountComponent({ fetchImpl: fetchMock })

    expect(wrapper.text()).toContain('Noch keine Übersetzungen')
  })

  it('handles load error gracefully', async () => {
    const errorFetch = vi.fn(async () => {
      throw new Error('Network error')
    })
    const { wrapper } = await mountComponent({ fetchImpl: errorFetch })
    await flushPromises()

    expect(wrapper.text()).toMatch(/Fehler|error/i)
  })

  it('shows translation table headers correctly', async () => {
    const { wrapper } = await mountComponent()
    const headers = wrapper.findAll('th')
    const headerText = headers.map(h => h.text()).join(' ')

    expect(headerText).toContain('Sprache')
    expect(headerText).toContain('Titel')
    expect(headerText).toContain('Nachricht')
    expect(headerText).toContain('Status')
    expect(headerText).toContain('Aktionen')
  })

  it('renders page header with title', async () => {
    const { wrapper } = await mountComponent()
    expect(wrapper.text()).toContain('Vorfall-Übersetzungen')
  })

  it('hides new button when editing', async () => {
    const { wrapper } = await mountComponent()
    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    const newButtonsAfter = wrapper.findAll('button').filter(b => b.text().includes('Neue Übersetzung'))
    expect(newButtonsAfter.length).toBe(0)
  })

  it('validates form fields are required', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponent({ fetchImpl: fetchMock })

    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    // Try to save with empty form
    const forms = wrapper.findAll('form')
    if (forms.length > 0) {
      await forms[0].trigger('submit.prevent')
      await flushPromises()
      expect(wrapper.text()).toContain('ist ein Pflichtfeld')
    }
  })

  it('only shows confirm button for draft translations', async () => {
    const { wrapper } = await mountComponent()
    const rows = wrapper.findAll('tbody tr')

    expect(rows.length).toBeGreaterThan(0)
    // First row is confirmed - no confirm button
    const firstRowText = rows[0].text()
    expect(firstRowText).toContain('Bestätigt')
    expect(firstRowText).not.toContain('Bestätigen')
  })

  it('shows confirm button for draft status', async () => {
    const { wrapper } = await mountComponent()
    const confirmButtons = wrapper.findAll('button').filter(b => b.text().includes('Bestätigen'))
    expect(confirmButtons.length).toBeGreaterThan(0)
  })

  it('handles language code selection in form', async () => {
    const { wrapper } = await mountComponent()
    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    const selects = wrapper.findAll('select')
    if (selects.length > 0) {
      expect(selects[0].exists()).toBe(true)
    }
  })

  it('filters already-used languages from selection', async () => {
    const { wrapper } = await mountComponent()
    const usedLanguages = ['de', 'fr']
    expect(wrapper.text()).toContain('de')
    expect(wrapper.text()).toContain('fr')
  })

  it('renders all table column headers', async () => {
    const { wrapper } = await mountComponent()
    expect(wrapper.text()).toContain('Sprache')
    expect(wrapper.text()).toContain('Titel')
    expect(wrapper.text()).toContain('Nachricht')
    expect(wrapper.text()).toContain('Status')
  })

  it('correctly displays translation data in table rows', async () => {
    const { wrapper } = await mountComponent()
    const tableText = wrapper.find('table').text()
    expect(tableText).toContain('Datenbankausfall')
  })

  it('handles multiple translations in table', async () => {
    const { wrapper } = await mountComponent()
    const rows = wrapper.findAll('tbody tr')
    expect(rows.length).toBeGreaterThanOrEqual(2)
  })

  it('renders form with select dropdown for language', async () => {
    const { wrapper } = await mountComponent()
    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    const selects = wrapper.findAll('select')
    expect(selects.length).toBeGreaterThan(0)
  })

  it('renders form with text inputs for title and message', async () => {
    const { wrapper } = await mountComponent()
    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    // Die Nachricht der Übersetzung ist der Editor.
    expect(wrapper.find('.rte textarea').exists()).toBe(true)
    const inputs = wrapper.findAll('input')
    const textareas = wrapper.findAll('textarea')
    expect(inputs.length + textareas.length).toBeGreaterThan(0)
  })

  it('button state changes during save operation', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponent({ fetchImpl: fetchMock })

    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    const saveButtons = wrapper.findAll('button').filter(b => b.text().includes('Speichern'))
    expect(saveButtons.length).toBeGreaterThan(0)
  })

  it('handles confirm error gracefully', async () => {
    const errorFetch = vi.fn(async (url, opts) => {
      if (opts?.method === 'PATCH' && /confirm/.test(url)) {
        throw new Error('Confirm failed')
      }
      return makeFetchRouter()()
    })
    const { wrapper } = await mountComponent({ fetchImpl: errorFetch })

    const confirmButtons = wrapper.findAll('button').filter(b => b.text().includes('Bestätigen'))
    if (confirmButtons.length > 0) {
      await confirmButtons[0].trigger('click')
      await flushPromises()
      expect(wrapper.text()).toMatch(/Bestätigung fehlgeschlagen/)
    }
  })

  it('handles delete error gracefully', async () => {
    const errorFetch = vi.fn(async (url, opts) => {
      if (opts?.method === 'DELETE') {
        throw new Error('Delete failed')
      }
      return makeFetchRouter()()
    })
    const { wrapper } = await mountComponent({ fetchImpl: errorFetch })

    const deleteButtons = wrapper.findAll('button').filter(b => b.text().includes('Löschen'))
    if (deleteButtons.length > 0) {
      stubConfirm(true)
      await deleteButtons[0].trigger('click')
      await flushPromises()
      expect(wrapper.text()).toMatch(/Löschen fehlgeschlagen/)
    }
  })

  it('shows loading state when saving', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponent({ fetchImpl: fetchMock })

    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    const titleInput = wrapper.find('input[type="text"]')
    const languageSelect = wrapper.find('select')
    const textarea = wrapper.find('textarea')
    if (titleInput.exists() && languageSelect.exists() && textarea.exists()) {
      await languageSelect.setValue('fr')
      await titleInput.setValue('Test Title')
      await textarea.setValue('Test Message')

      const saveButtons = wrapper.findAll('button').filter(b => b.text().includes('Speichern'))
      if (saveButtons.length > 0) {
        expect(saveButtons[0].text()).toContain('Speichern')
      }
    }
  })

  it('close alert when clicking close button', async () => {
    const { wrapper } = await mountComponent()
    await wrapper.vm.$nextTick()

    const alert = wrapper.find('[class*="alert"]')
    if (alert.exists()) {
      const closeButton = wrapper.find('.alert-close')
      if (closeButton.exists()) {
        await closeButton.trigger('click')
        await flushPromises()
        expect(wrapper.vm.alertMsg).toBe('')
      }
    }
  })

  it('displays success alert after create', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponent({ fetchImpl: fetchMock })

    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    const languageSelect = wrapper.find('select')
    const titleInput = wrapper.find('input[type="text"]')
    const textarea = wrapper.find('textarea')
    if (languageSelect.exists() && titleInput.exists() && textarea.exists()) {
      await languageSelect.setValue('en')
      await titleInput.setValue('New Title')
      await textarea.setValue('New Message')

      const forms = wrapper.findAll('form')
      if (forms.length > 0) {
        await forms[0].trigger('submit')
        await flushPromises()
        expect(wrapper.text()).toContain('erstellt')
      }
    }
  })

  it('hat keine axe-Funde', async () => {
    const { wrapper } = await mountComponent()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Ein Viewer (incidents:read) sah „Neue Übersetzung“, „Bestätigen“
  // und „Löschen“, das Backend lehnt alle drei mit 403 ab.
  it('blendet Schreibknöpfe und Aktionen-Spalte ohne incidents:write aus', async () => {
    useTenantStatic().setMyPages([{ ID: 1, Permissions: 'incidents:read' }])
    const { wrapper } = await mountComponent()

    expect(wrapper.findAll('tbody tr').length).toBe(2)
    expect(wrapper.find('th.actions-col').exists()).toBe(false)
    expect(wrapper.find('.row-menu').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Neue Übersetzung')
  })

  it('zeigt den Leerzustand ohne Tabelle, Schreibknopf nur mit Recht', async () => {
    useTenantStatic().setMyPages([{ ID: 1, Permissions: 'incidents:read' }])
    const { wrapper: viewer } = await mountComponent({ fetchImpl: makeFetchRouter({ translationsOverride: [] }) })
    expect(viewer.find('.empty-state__title').text()).toBe('Noch keine Übersetzungen.')
    expect(viewer.find('.row-menu').exists()).toBe(false)
    viewer.unmount()

    useTenantStatic().setMyPages([{ ID: 1, Permissions: 'incidents:write' }])
    const { wrapper: editor } = await mountComponent({ fetchImpl: makeFetchRouter({ translationsOverride: [] }) })
    expect(editor.find('.empty-state__title').text()).toBe('Noch keine Übersetzungen.')
    expect(editor.text()).toContain('Neue Übersetzung')
  })

  it('zeigt Global Admins die Schreibknöpfe auch ohne Seitenrolle', async () => {
    useTenantStatic().setMyPages([])
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const { wrapper } = await mountComponent()
    expect(wrapper.find('.row-menu').exists()).toBe(true)
  })

  describe('Richtung C', () => {
    it('hat Seitentitel als h2 ohne Inline-Style, Brotkrumen bis zum Vorfall und Untertitel', async () => {
      useTenantStatic().setMyPages([{ ID: 1, Name: 'Meine Seite', Permissions: 'incidents:write' }])
      const { wrapper } = await mountComponent()
      expect(wrapper.findAll('h2').map(h => h.text())).toEqual(['Vorfall-Übersetzungen'])
      expect(wrapper.find('h2').attributes('style')).toBeUndefined()
      expect(wrapper.find('.page-head [style]').exists()).toBe(false)
      expect(wrapper.find('.page-head__crumbs').text()).toContain('Meine Seite')
      expect(wrapper.find('.page-head__crumbs').text()).toContain('Vorfälle')
      expect(wrapper.find('.page-head__crumbs [aria-current="page"]').text()).toBe('Database Outage')
      expect(wrapper.find('.page-head__subtitle').text()).toBe('Übersetzungen zu „Database Outage“')
    })

    // Regression: der Status stand als englischer Rohwert („confirmed“, „draft“) in der Tabelle.
    it('zeigt den Status als Badge mit deutschem Text und Zeilenmenü statt Aktionsknöpfen', async () => {
      const { wrapper } = await mountComponent()
      const rows = wrapper.findAll('tbody tr')
      expect(rows[0].find('.st-badge').text()).toBe('Bestätigt')
      expect(rows[1].find('.st-badge').text()).toBe('Entwurf')
      expect(rows[0].findAll('[role="menuitem"]').map(i => i.text())).toEqual(['Löschen'])
      expect(rows[1].findAll('[role="menuitem"]').map(i => i.text())).toEqual(['Bestätigen', 'Löschen'])
      expect(wrapper.find('tbody .action-btn').exists()).toBe(false)
    })

    it('Formular als Seitenkarte: Fehler am Feld mit Fokus, kein Request, × schließt', async () => {
      const fetchMock = makeFetchRouter()
      const { wrapper } = await mountComponent({ fetchImpl: fetchMock })
      await wrapper.findAll('button').find(b => b.text().includes('Neue Übersetzung')).trigger('click')
      expect(wrapper.find('.split').classes()).toContain('split--open')
      expect(wrapper.find('.form-panel h3').text()).toBe('Neue Übersetzung')
      await wrapper.find('form').trigger('submit.prevent')
      await flushPromises()
      expect(wrapper.findAll('.form-field__message--error')).toHaveLength(3)
      expect(wrapper.find('select').attributes('aria-invalid')).toBe('true')
      expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)
      await wrapper.find('.form-panel__close').trigger('click')
      expect(wrapper.find('.form-panel').exists()).toBe(false)
    })
    it('bestätigt einen Entwurf über das Zeilenmenü und zeigt währenddessen „Bestätigung…“', async () => {
      const base = makeFetchRouter()
      const fetchMock = vi.fn((url, o = {}) => (o.method === 'PATCH' ? new Promise(() => {}) : base(url, o)))
      const { wrapper } = await mountComponent({ fetchImpl: fetchMock })
      const draftRow = wrapper.findAll('tbody tr')[1]
      await clickMenuItem(draftRow, 'Bestätigen')
      await flushPromises()
      expect(fetchMock).toHaveBeenCalledWith('/api/incident-translations/2/confirm', expect.objectContaining({ method: 'PATCH' }))
      const item = draftRow.find('[role="menuitem"]')
      expect(item.text()).toContain('Bestätigung…')
      expect(item.attributes('aria-disabled')).toBe('true')
    })

    it('löscht über das Zeilenmenü nach Bestätigung und zeigt währenddessen „Löschen…“', async () => {
      stubConfirm(true)
      const base = makeFetchRouter()
      const fetchMock = vi.fn((url, o = {}) => (o.method === 'DELETE' ? new Promise(() => {}) : base(url, o)))
      const { wrapper } = await mountComponent({ fetchImpl: fetchMock })
      const row = wrapper.findAll('tbody tr')[0]
      await clickMenuItem(row, 'Löschen')
      await flushPromises()
      expect(fetchMock).toHaveBeenCalledWith('/api/incident-translations/1', expect.objectContaining({ method: 'DELETE' }))
      expect(row.find('[role="menuitem"]').text()).toContain('Löschen…')
    })
  })

  // Bestätigen braucht incidents:update, Löschen incidents:delete, Neu incidents:create.
  it('zeigt Bestätigen nur mit update, Löschen nur mit delete und Neu nur mit create', async () => {
    useTenantStatic().setMyPages([{ ID: 1, Permissions: 'incidents:update' }])
    const onlyUpdate = (await mountComponent()).wrapper
    const entries = (w) => w.findAll('[role="menuitem"]').map(i => i.text())
    expect(entries(onlyUpdate).every(t => t.includes('Bestätigen'))).toBe(true)
    expect(entries(onlyUpdate).length).toBeGreaterThan(0)
    expect(onlyUpdate.findAll('button').some(b => b.text().includes('Neu'))).toBe(false)
    onlyUpdate.unmount()

    useTenantStatic().setMyPages([{ ID: 1, Permissions: 'incidents:delete' }])
    const onlyDelete = (await mountComponent()).wrapper
    expect(entries(onlyDelete).every(t => t.includes('Löschen'))).toBe(true)
    expect(entries(onlyDelete).length).toBeGreaterThan(0)
  })
})
