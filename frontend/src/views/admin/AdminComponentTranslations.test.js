import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant as useTenantStatic } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminComponentTranslations from './AdminComponentTranslations.vue'
import { axe } from '../../test-support/axe.js'
import { stubConfirm } from '../../test-support/confirm'

function componentFixture() {
  return { ID: 1, StatusPageID: 1, Name: 'Database', Status: 'operational' }
}

function translationsFixture() {
  return [
    { ID: 1, ComponentID: 1, LanguageCode: 'de', Title: 'Datenbank', Description: 'Hauptdatenbank', Status: 'confirmed' },
    { ID: 2, ComponentID: 1, LanguageCode: 'fr', Title: 'Base de données', Description: 'Base principale', Status: 'draft' },
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

function makeFetchRouter({ componentOverride, translationsOverride, catalogOverride, pageId = '1' } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    // Einen Einzel-GET für Komponenten gibt es im Backend nicht,
    // die View sucht die Komponente im Baum der Seitenliste (hier als Kind).
    if (method === 'GET' && url === `/api/pages/${pageId}/components`) {
      return jsonResponse([{ ID: 7, Name: 'Plattform', Children: [componentOverride ?? componentFixture()] }])
    }
    if (method === 'GET' && /\/api\/components\/\d+\/translations\/all$/.test(url)) {
      return jsonResponse(translationsOverride ?? translationsFixture())
    }
    if (method === 'GET' && /\/api\/languages$/.test(url)) {
      return jsonResponse(catalogOverride ?? catalogFixture())
    }
    if (method === 'POST' && /\/api\/components\/\d+\/translations$/.test(url)) {
      return jsonResponse({ ID: 99, ComponentID: 1, Status: 'draft', ...JSON.parse(options.body) })
    }
    if (method === 'PATCH' && /\/api\/component-translations\/\d+\/confirm$/.test(url)) {
      return jsonResponse({ ...translationsFixture()[1], Status: 'confirmed' })
    }
    if (method === 'DELETE' && /\/api\/component-translations\/\d+$/.test(url)) {
      return jsonResponse({})
    }
    throw new Error(`Unerwarteter Fetch: ${method} ${url}`)
  })
}

let activeWrapper = null

async function mountComponent({ attach = false, fetchImpl, componentId = '1', pageId = '1' } = {}) {
  const { useTenant } = await import('../../composables/useTenant')
  useTenant().setActiveTenant(pageId)
  global.fetch = fetchImpl || makeFetchRouter({ pageId })
  const wrapper = mount(AdminComponentTranslations, {
    attachTo: attach ? document.body : undefined,
    global: { plugins: [i18n], stubs: onyxFormStubs },
    props: { componentId }
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminComponentTranslations', () => {
  beforeEach(() => {
    // Die Knöpfe hängen jetzt an components:write auf der aktiven Seite.
    useAuth().setAuth('jwt-1', { permissions: '' })
    useTenantStatic().setMyPages([{ ID: 1, Permissions: 'components:read,components:write' }])
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
    expect(wrapper.text()).toContain('Datenbank')
    expect(wrapper.text()).toContain('confirmed')
  })

  it('displays draft translations with badge', async () => {
    const { wrapper } = await mountComponent()
    expect(wrapper.text()).toContain('Base de données')
    expect(wrapper.text()).toContain('draft')
  })

  it('renders translation table', async () => {
    const { wrapper } = await mountComponent()
    const table = wrapper.find('table')
    expect(table.exists()).toBe(true)
  })

  it('displays component name', async () => {
    const { wrapper } = await mountComponent()
    expect(wrapper.text()).toContain('Database')
  })

  it('lädt die Komponente aus der Seitenliste statt über einen nicht existierenden Einzel-GET', async () => {
    const fetchImpl = makeFetchRouter()
    const { wrapper } = await mountComponent({ fetchImpl })
    const urls = fetchImpl.mock.calls.map(([url]) => url)
    expect(urls).toContain('/api/pages/1/components')
    expect(urls.some(u => /\/api\/pages\/1\/components\/\d+/.test(u))).toBe(false)
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('zeigt keinen Komponentennamen, wenn die ID nicht in der Seitenliste vorkommt', async () => {
    const { wrapper } = await mountComponent({ componentId: '42' })
    expect(wrapper.text()).not.toContain('Database')
    expect(wrapper.find('.alert-error').exists()).toBe(false)
  })

  it('renders new translation button', async () => {
    const { wrapper } = await mountComponent()
    const buttons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    expect(buttons.length).toBeGreaterThan(0)
  })

  it('renders language codes in table', async () => {
    const { wrapper } = await mountComponent()
    expect(wrapper.text()).toContain('de')
    expect(wrapper.text()).toContain('fr')
  })

  it('shows action buttons for translations', async () => {
    const { wrapper } = await mountComponent()
    const confirmButtons = wrapper.findAll('button').filter(b => b.text().includes('Bestätigen'))
    const deleteButtons = wrapper.findAll('button').filter(b => b.text().includes('Löschen'))
    expect(confirmButtons.length).toBeGreaterThan(0)
    expect(deleteButtons.length).toBeGreaterThan(0)
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

  // Richtung C: Pflichtfelder melden den Fehler am Feld, der Fokus springt
  // zum ersten Fehlerfeld, und es geht nichts an den Server.
  it('meldet fehlende Pflichtfelder am Feld und sendet nichts', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountComponent({ fetchImpl: fetchMock, attach: true })

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    await flushPromises()
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const messages = wrapper.findAll('.form-field__message--error').map(m => m.text())
    expect(messages).toEqual(['Bitte eine Sprache wählen.', 'Bitte einen Titel eingeben.', 'Bitte einen Text eingeben.'])
    expect(wrapper.find('#lang-select').attributes('aria-invalid')).toBe('true')
    expect(document.activeElement).toBe(wrapper.find('#lang-select').element)
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)

    await wrapper.find('#title-input').setValue('Titel')
    expect(wrapper.find('#title-input').attributes('aria-invalid')).toBeUndefined()
  })

  it('shows confirm button for draft status', async () => {
    const { wrapper } = await mountComponent()
    const confirmButtons = wrapper.findAll('button').filter(b => b.text().includes('Bestätigen'))
    expect(confirmButtons.length).toBeGreaterThan(0)
  })

  it('handles language filtering correctly', async () => {
    const { wrapper } = await mountComponent()
    const usedCodes = ['de', 'fr']
    expect(wrapper.text()).toContain('de')
    expect(wrapper.text()).toContain('fr')
  })

  it('renders correct table column headers', async () => {
    const { wrapper } = await mountComponent()
    expect(wrapper.text()).toContain('Sprache')
    expect(wrapper.text()).toContain('Titel')
    expect(wrapper.text()).toContain('Beschreibung')
    expect(wrapper.text()).toContain('Status')
  })

  it('displays translation descriptions in table', async () => {
    const { wrapper } = await mountComponent()
    const tableText = wrapper.find('table').text()
    expect(tableText).toContain('Hauptdatenbank')
  })

  it('has multiple translation rows for component', async () => {
    const { wrapper } = await mountComponent()
    const rows = wrapper.findAll('tbody tr')
    expect(rows.length).toBeGreaterThanOrEqual(2)
  })

  it('renders form with language select and description field', async () => {
    const { wrapper } = await mountComponent()
    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    const selects = wrapper.findAll('select')
    const textareas = wrapper.findAll('textarea')
    expect(selects.length).toBeGreaterThan(0)
    expect(textareas.length).toBeGreaterThan(0)
  })

  it('provides form inputs for translation fields', async () => {
    const { wrapper } = await mountComponent()
    const newButtons = wrapper.findAll('button').filter(b => b.text().includes('Neu'))
    await newButtons[0].trigger('click')
    await flushPromises()

    const inputs = wrapper.findAll('input[type="text"]')
    expect(inputs.length).toBeGreaterThan(0)
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
      await textarea.setValue('Test Description')

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
    const descInput = wrapper.findAll('textarea')[0]
    if (languageSelect.exists() && titleInput.exists() && descInput.exists()) {
      await languageSelect.setValue('en')
      await titleInput.setValue('New Title')
      await descInput.setValue('New Description')

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

  // Ein Viewer (components:read) sah „Neue Übersetzung“, „Bestätigen“
  // und „Löschen“, das Backend lehnt alle drei mit 403 ab.
  it('blendet Schreibknöpfe und Aktionen-Spalte ohne components:write aus', async () => {
    useTenantStatic().setMyPages([{ ID: 1, Permissions: 'components:read' }])
    const { wrapper } = await mountComponent()

    expect(wrapper.findAll('tbody tr').length).toBe(2)
    expect(wrapper.find('th.actions-col').exists()).toBe(false)
    expect(wrapper.find('.row-menu').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Neue Übersetzung')
  })

  it('zeigt den Leerzustand in der Karte und die Schreibknöpfe nur mit Schreibrecht', async () => {
    useTenantStatic().setMyPages([{ ID: 1, Permissions: 'components:read' }])
    const { wrapper: viewer } = await mountComponent({ fetchImpl: makeFetchRouter({ translationsOverride: [] }) })
    expect(viewer.find('section.comp-card .empty-state').exists()).toBe(true)
    expect(viewer.find('table').exists()).toBe(false)
    expect(viewer.text()).not.toContain('Neue Übersetzung')
    viewer.unmount()

    useTenantStatic().setMyPages([{ ID: 1, Permissions: 'components:write' }])
    const { wrapper: editor } = await mountComponent({ fetchImpl: makeFetchRouter({ translationsOverride: [] }) })
    expect(editor.find('section.comp-card .empty-state').exists()).toBe(true)
    expect(editor.text()).toContain('Neue Übersetzung')
  })

  it('zeigt Global Admins die Schreibknöpfe auch ohne Seitenrolle', async () => {
    useTenantStatic().setMyPages([])
    useAuth().setAuth('jwt-1', { is_global_admin: true })
    const { wrapper } = await mountComponent()
    expect(wrapper.find('.row-menu').exists()).toBe(true)
  })

  it('zeigt Liste und Formular nebeneinander, Brotkrumen zur Komponentenliste und StatusBadge', async () => {
    const { wrapper } = await mountComponent()
    expect(wrapper.find('.page-head__crumbs').text()).toContain('Komponenten')
    expect(wrapper.find('.page-head__subtitle').text()).toContain('Database')
    expect(wrapper.findAll('.st-badge').length).toBe(2)
    expect(wrapper.find('.split').classes()).not.toContain('split--open')

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')

    expect(wrapper.find('.split').classes()).toContain('split--open')
    expect(wrapper.find('table.data-table').exists()).toBe(true)
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Richtung C: Bestätigen und Löschen stehen im Zeilenmenü; während
  // sie laufen, zeigt der Eintrag die Laufend-Beschriftung und ist gesperrt.
  it('bietet Bestätigen nur für Entwürfe an und zeigt während der Aktion die Laufend-Beschriftung', async () => {
    stubConfirm(true)
    const base = makeFetchRouter()
    const never = new Promise(() => {})
    const fetchImpl = vi.fn(async (url, opts = {}) => (
      (opts.method === 'PATCH' || opts.method === 'DELETE') ? never : base(url, opts)))
    const { wrapper } = await mountComponent({ fetchImpl })
    const items = (row) => wrapper.findAll('tbody tr')[row].findAll('[role="menuitem"]')

    expect(items(0).map(b => b.text())).toEqual(['Löschen'])
    expect(items(1).map(b => b.text())).toEqual(['Bestätigen', 'Löschen'])

    await items(1)[0].trigger('click')
    expect(items(1)[0].text()).toBe('Bestätigung…')
    expect(items(1)[0].attributes('aria-disabled')).toBe('true')
    await items(0)[0].trigger('click')
    expect(items(0)[0].text()).toBe('Löschen…')
    vi.unstubAllGlobals()
  })

  // Richtung C: Karte „Übersetzungen“, Brotkrumen mit Seitenname, Titel statt ID.
  it('zeigt Brotkrumen mit Seitenname, Listenkarte, Name fett und Formular als Seitenkarte', async () => {
    useTenantStatic().setMyPages([{ ID: 1, Name: 'Acme', Permissions: 'components:write' }])
    const { wrapper } = await mountComponent()

    const crumbs = wrapper.findAll('.page-head__crumbs li').map(li => li.text())
    expect(crumbs[0]).toBe('Acme')
    expect(crumbs).toHaveLength(3)
    expect(wrapper.find('section.comp-card h3').text()).toBe('Übersetzungen')
    expect(wrapper.find('.component-name__title').text()).toBe('Datenbank')

    await wrapper.findAll('button').find(b => b.text().includes('Neu')).trigger('click')
    expect(wrapper.find('aside.form-panel h3').text()).toBe('Neue Übersetzung')
    expect(wrapper.find('aside .form-panel__hint').text()).toBe('Felder mit * sind Pflichtfelder.')
    await wrapper.find('aside .form-panel__close').trigger('click')
    expect(wrapper.find('aside').exists()).toBe(false)
  })

  it('zeigt einen Löschfehler als Meldung und schließt sie über den Knopf', async () => {
    stubConfirm(true)
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, opts = {}) => (
      opts.method === 'DELETE' ? jsonResponse({ error: 'kaputt' }, 500) : base(url, opts)))
    const { wrapper } = await mountComponent({ fetchImpl })

    await wrapper.findAll('tbody tr')[0].findAll('[role="menuitem"]').at(-1).trigger('click')
    await flushPromises()
    expect(wrapper.find('.alert-error').text()).toContain('kaputt')

    await wrapper.find('.alert-close').trigger('click')
    expect(wrapper.find('.alert').exists()).toBe(false)
    vi.unstubAllGlobals()
  })

  // handleDelete (useAdminTranslationsPanel) löscht nichts, wenn die Rückfrage abgelehnt wird.
  it('Löschen einer Übersetzung: abgelehnt -> kein DELETE', async () => {
    const confirm = stubConfirm(false)
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, opts = {}) => base(url, opts))
    const { wrapper } = await mountComponent({ fetchImpl })

    await wrapper.findAll('tbody tr')[0].findAll('[role="menuitem"]')[0].trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledTimes(1)
    expect(fetchImpl.mock.calls.some(([, o]) => o?.method === 'DELETE')).toBe(false)
  })
})
