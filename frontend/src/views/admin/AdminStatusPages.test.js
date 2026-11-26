import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import AdminStatusPages from './AdminStatusPages.vue'
import statusPagesSource from './AdminStatusPages.vue?raw'
import { axe } from '../../test-support/axe'
import { stubConfirm } from '../../test-support/confirm'
import { setConfirmHandler } from '../../composables/useConfirm'

function pagesFixture() {
  return [
    { ID: 1, Name: 'Acme Corp', Slug: 'acme', Description: 'Acme Status', IsPrivate: false, CustomDomain: '' },
    { ID: 2, Name: 'Beta Internal', Slug: 'beta', Description: '', IsPrivate: true, CustomDomain: 'status.beta.example', CustomDomainStatus: 'pending' },
  ]
}

function usersFixture() {
  return [{ ID: 1, Email: 'a@b.de' }]
}

function rolesFixture() {
  // "Global Admin" (GlobalOnly) must not be a page role.
  return [{ ID: 1, Name: 'Global Admin', GlobalOnly: true }, { ID: 3, Name: 'Viewer', GlobalOnly: false }]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeFetchRouter({ pages = pagesFixture(), loadOk = true, writeOk = true, writeError = { error: 'Fehlgeschlagen' }, writeStatus = 500 } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/api\/pages$/.test(url)) {
      return loadOk ? jsonResponse(pages) : jsonResponse({ error: 'kaputt' }, 500)
    }
    if (method === 'GET' && /\/api\/users$/.test(url)) return jsonResponse(usersFixture())
    if (method === 'GET' && /\/api\/roles$/.test(url)) return jsonResponse(rolesFixture())
    if (method === 'GET' && /\/invitations$/.test(url)) return jsonResponse([])
    if (method === 'GET' && /\/users$/.test(url)) return jsonResponse([])
    if (['POST', 'PUT', 'DELETE'].includes(method)) {
      return writeOk ? jsonResponse({}) : jsonResponse(writeError, writeStatus)
    }
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let activeWrapper = null

// Row menu (RowMenu): trigger an entry of a table row of the list.
const rowAction = (wrapper, rowIndex, label) =>
  wrapper.findAll('tbody tr')[rowIndex].findAll('[role="menuitem"]').find(b => b.text() === label).trigger('click')
// Submit the form (save is a submit button; a wrapper that is not attached does not submit on click).
const submitForm = (wrapper) => wrapper.find('form').trigger('submit')

async function mountPages({ fetchImpl, isGlobalAdmin = true, attach = false } = {}) {
  useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: isGlobalAdmin })
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminStatusPages, { attachTo: attach ? document.body : undefined, global: { plugins: [i18n] } })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminStatusPages', () => {
  beforeEach(() => {
    useAuth().logout()
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.unstubAllEnvs()
    vi.restoreAllMocks()
    useAuth().logout()
  })

  // The global .meta-tag rule sets capitals ("SYSTEM"), but the
  // slug is part of the (lower-case) URL. Vitest does not load scoped CSS,
  // so the class on the slug and the rule in the style block are checked.
  it('zeigt den Slug unverändert klein statt in Großbuchstaben', async () => {
    const { wrapper } = await mountPages()

    const slug = wrapper.findAll('tbody tr')[0].find('.meta-tag--slug')
    expect(slug.text()).toBe('acme')
    expect(statusPagesSource).toMatch(/\.meta-tag--slug\s*\{[^}]*text-transform:\s*none/)
  })

  // The system page (IsSystem) cannot be deleted and its slug
  // cannot be changed (backend: 409 system_page_slug_locked) - the frontend
  // does not offer either in the first place.
  describe('Systemseite', () => {
    const withSystem = () => [
      ...pagesFixture(),
      { ID: 9, Name: 'System', Slug: 'system', Description: '', IsPrivate: false, IsSystem: true },
    ]

    it('bietet für die Systemseite kein Löschen an, für normale Seiten schon', async () => {
      const { wrapper } = await mountPages({ fetchImpl: makeFetchRouter({ pages: withSystem() }) })
      const labels = i => wrapper.findAll('tbody tr')[i].findAll('[role="menuitem"]').map(b => b.text())
      expect(labels(0)).toContain('Löschen')
      expect(labels(2)).toEqual(['Öffnen', 'Benutzer', 'Bearbeiten'])
    })

    it('sperrt den Slug der Systemseite und erklärt es, andere Seiten bleiben editierbar', async () => {
      const { wrapper } = await mountPages({ fetchImpl: makeFetchRouter({ pages: withSystem() }) })

      await rowAction(wrapper, 2, 'Bearbeiten')
      expect(wrapper.find('#page-slug').attributes('disabled')).toBeDefined()
      const hint = wrapper.find(`#${wrapper.find('#page-slug').attributes('aria-describedby')}`)
      expect(hint.text()).toBe('Der Slug der Systemseite ist fest vorgegeben und kann nicht geändert werden.')

      await wrapper.findAll('button').find(b => b.text() === 'Abbrechen').trigger('click')
      await rowAction(wrapper, 0, 'Bearbeiten')
      expect(wrapper.find('#page-slug').attributes('disabled')).toBeUndefined()
      expect(wrapper.find('#page-slug').attributes('aria-describedby')).toBeUndefined()
    })

    it('lässt den Slug beim Anlegen einer neuen Seite editierbar', async () => {
      const { wrapper } = await mountPages({ fetchImpl: makeFetchRouter({ pages: withSystem() }) })
      await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
      expect(wrapper.find('#page-slug').attributes('disabled')).toBeUndefined()
    })
  })

  it('lädt Statusseiten und zeigt Sichtbarkeit + Custom-Domain-Status', async () => {
    const { wrapper } = await mountPages()

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('Acme Corp')
    expect(rows[0].text()).toContain('Öffentlich')
    expect(rows[1].text()).toContain('Beta Internal')
    expect(rows[1].text()).toContain('Privat')
    expect(rows[1].text()).toContain('status.beta.example')
  })

  // Stage 4d: h2 instead of h1 (the admin top bar carries the h1), empty state,
  // domain status as StatusBadge (shape + text), fields via FormField.
  it('rendert den Titel als h2 ohne h1', async () => {
    const { wrapper } = await mountPages()

    expect(wrapper.find('h2.page-head__title').exists()).toBe(true)
    expect(wrapper.find('h1').exists()).toBe(false)
  })

  // Direction C: page header with breadcrumbs and a count, list card with
  // search, bold name with description, relative time, row menu instead of a row of buttons.
  it('zeigt Brotkrumen, Zählung und Karte mit Suche im Richtung-C-Muster', async () => {
    const { wrapper } = await mountPages()

    expect(wrapper.find('.page-head__crumbs').text()).toContain('Administration')
    expect(wrapper.find('.page-head__crumbs [aria-current="page"]').text()).toBe('Statusseiten')
    expect(wrapper.find('.page-head__subtitle').text()).toBe('2 Statusseiten · 1 privat')
    expect(wrapper.find('section.comp-card h3').text()).toBe('Alle Statusseiten')
    expect(wrapper.find('.comp-search input[type="search"]').exists()).toBe(true)
    expect(wrapper.find('.component-name__title').text()).toBe('Acme Corp')
    expect(wrapper.find('.component-name__desc').text()).toBe('Acme Status')
    expect(wrapper.find('tbody .action-btn').exists()).toBe(false)
    expect(wrapper.findAll('tbody tr')[0].findAll('[role="menuitem"]').map(b => b.text()))
      .toEqual(['Öffnen', 'Benutzer', 'Bearbeiten', 'Löschen'])
  })

  it('zeigt die letzte Änderung relativ und ohne Zeitstempel einen Strich', async () => {
    const recent = new Date(Date.now() - 3 * 86400000).toISOString()
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options) =>
      /\/api\/pages$/.test(url) ? jsonResponse([{ ID: 1, Name: 'A', Slug: 'a', UpdatedAt: recent }, { ID: 2, Name: 'B', Slug: 'b' }]) : base(url, options))
    const { wrapper } = await mountPages({ fetchImpl })

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].find('time').text()).toMatch(/3 Tagen/)
    expect(rows[1].find('time').exists()).toBe(false)
    expect(rows[1].text()).toContain('–')
  })

  it('filtert die Liste nach Name, Slug oder Domain und meldet leere Treffer', async () => {
    const { wrapper } = await mountPages()
    const search = wrapper.find('.comp-search input')

    await search.setValue('beta.example')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('tbody tr').text()).toContain('Beta Internal')
    expect(wrapper.find('[role="status"].sr-only').text()).toBe('1 Treffer')

    await search.setValue('gibtesnicht')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.find('.empty-state').text()).toContain('Keine Statusseite gefunden.')
  })

  it('benennt das Bearbeiten-Formular mit dem Namen statt der ID, mit Pflichtfeld-Hinweis und Schließen-Knopf', async () => {
    const { wrapper } = await mountPages()

    await rowAction(wrapper, 0, 'Bearbeiten')
    const panel = wrapper.find('aside.form-panel')
    expect(panel.find('h3').text()).toBe('Statusseite „Acme Corp“ bearbeiten')
    expect(panel.find('h3').text()).not.toContain('ID')
    expect(panel.find('.form-panel__hint').text()).toBe('Felder mit * sind Pflichtfelder.')
    // The list stays next to the form.
    expect(wrapper.find('.split').classes()).toContain('split--open')
    expect(wrapper.find('section.comp-card table').exists()).toBe(true)

    await panel.find('.form-panel__close').trigger('click')
    expect(wrapper.find('aside').exists()).toBe(false)
    expect(wrapper.find('.split').classes()).not.toContain('split--open')
  })

  it('zeigt das Formular zum Anlegen mit eigenem Titel', async () => {
    const { wrapper } = await mountPages()

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    expect(wrapper.find('aside.form-panel h3').text()).toBe('Neue Statusseite anlegen')
  })

  it('wechselt zwischen Formular und Benutzerverwaltung, ohne beides gleichzeitig zu zeigen', async () => {
    const { wrapper } = await mountPages()

    await rowAction(wrapper, 0, 'Bearbeiten')
    await rowAction(wrapper, 1, 'Benutzer')
    await flushPromises()
    expect(wrapper.findAll('aside')).toHaveLength(1)
    expect(wrapper.find('aside h3').text()).toBe('Berechtigungen: Beta Internal')
    expect(wrapper.find('aside .form-panel__hint').exists()).toBe(false)

    await rowAction(wrapper, 0, 'Bearbeiten')
    expect(wrapper.findAll('aside')).toHaveLength(1)
    expect(wrapper.find('aside h3').text()).toContain('Acme Corp')
  })

  it('zeigt bei leerer Liste den Leerzustand statt der Tabelle', async () => {
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options) =>
      /\/api\/pages$/.test(url) ? jsonResponse([]) : base(url, options))
    const { wrapper } = await mountPages({ fetchImpl })

    expect(wrapper.find('.empty-state').exists()).toBe(true)
    expect(wrapper.find('table').exists()).toBe(false)
  })

  it('zeigt den Domain-Status als StatusBadge je Stufe und unbekannte Status neutral', async () => {
    const pages = [
      { ID: 1, Name: 'A', Slug: 'a', CustomDomain: 'a.example', CustomDomainStatus: 'pending' },
      { ID: 2, Name: 'B', Slug: 'b', CustomDomain: 'b.example', CustomDomainStatus: 'verified' },
      { ID: 3, Name: 'C', Slug: 'c', CustomDomain: 'c.example', CustomDomainStatus: 'active' },
      { ID: 4, Name: 'D', Slug: 'd', CustomDomain: 'd.example', CustomDomainStatus: 'seltsam' },
      { ID: 5, Name: 'E', Slug: 'e', CustomDomain: 'e.example', CustomDomainStatus: 'check_failed' },
    ]
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options) =>
      /\/api\/pages$/.test(url) ? jsonResponse(pages) : base(url, options))
    const { wrapper } = await mountPages({ fetchImpl })

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].find('.st-badge--degraded').exists()).toBe(true)
    expect(rows[1].find('.st-badge--maintenance').exists()).toBe(true)
    expect(rows[2].find('.st-badge--operational').exists()).toBe(true)
    expect(rows[3].find('.st-badge--unknown').exists()).toBe(true)
    // after a failed check no longer "DNS not checked yet".
    expect(rows[4].find('.st-badge--major').exists()).toBe(true)
    expect(rows[4].text()).toContain('Prüfung fehlgeschlagen')
    expect(rows[4].text()).not.toContain('DNS noch nicht geprüft')
    expect(rows[0].text()).toContain('DNS noch nicht geprüft')
  })

  it('zeigt nach fehlgeschlagener DNS-Prüfung Zustand, Zeitpunkt und Grund', async () => {
    const fetchMock = vi.fn(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'POST' && /\/custom-domain\/verify$/.test(url)) {
        return jsonResponse({
          custom_domain_status: 'check_failed', custom_domain_checked_at: '2026-08-01T10:00:00Z',
          custom_domain_check_error: 'txt_not_found', message: 'TXT fehlt', txt_record_value: 'txt-abc',
        })
      }
      return makeFetchRouter()(url, options)
    })
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })
    await rowAction(wrapper, 1, 'Bearbeiten')
    await wrapper.findAll('button').find(b => b.text().includes('DNS verifizieren')).trigger('click')
    await flushPromises()

    const line = wrapper.find('[data-test="domain-check-failed"]')
    expect(line.text()).toContain('Letzte Prüfung am')
    expect(line.text()).toContain('TXT-Eintrag nicht gefunden')
    expect(wrapper.find('.status-line').text()).toContain('Prüfung fehlgeschlagen')
    expect(wrapper.find('.status-line').text()).not.toContain('noch nicht geprüft')
    expect(wrapper.text()).toContain('txt-abc')
  })

  it('übernimmt beim Öffnen gespeicherten Prüfzeitpunkt und -grund, unbekannter Grund neutral', async () => {
    const pages = [{ ID: 1, Name: 'A', Slug: 'a', CustomDomain: 'a.example', CustomDomainStatus: 'check_failed',
      CustomDomainCheckedAt: '2026-08-01T10:00:00Z', CustomDomainCheckError: 'dns_lookup_failed' },
    { ID: 2, Name: 'B', Slug: 'b', CustomDomain: 'b.example', CustomDomainStatus: 'check_failed',
      CustomDomainCheckedAt: '2026-08-01T10:00:00Z', CustomDomainCheckError: 'neu' }]
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options) => /\/api\/pages$/.test(url) ? jsonResponse(pages) : base(url, options))
    const { wrapper } = await mountPages({ fetchImpl })
    await rowAction(wrapper, 0, 'Bearbeiten')
    expect(wrapper.find('[data-test="domain-check-failed"]').text()).toContain('DNS-Abfrage fehlgeschlagen')
    await rowAction(wrapper, 1, 'Bearbeiten')
    expect(wrapper.find('[data-test="domain-check-failed"]').text()).toContain('Grund unbekannt')
  })

  it('zeigt bei verified/active den letzten Prüffehler ohne Statuswechsel und lässt erneutes Prüfen zu', async () => {
    const pages = [
      { ID: 1, Name: 'A', Slug: 'a', CustomDomain: 'a.example', CustomDomainStatus: 'active',
        CustomDomainCheckedAt: '2026-08-01T10:00:00Z', CustomDomainCheckError: 'dns_lookup_failed' },
      { ID: 2, Name: 'B', Slug: 'b', CustomDomain: 'b.example', CustomDomainStatus: 'verified' },
    ]
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options) => /\/api\/pages$/.test(url) ? jsonResponse(pages) : base(url, options))
    const { wrapper } = await mountPages({ fetchImpl })
    await rowAction(wrapper, 0, 'Bearbeiten')
    const line = wrapper.find('[data-test="domain-check-failed"]')
    expect(line.text()).toContain('DNS-Abfrage fehlgeschlagen')
    expect(line.text()).toContain('Status bleibt unverändert')
    expect(wrapper.find('.status-line').text()).toContain('Aktiv')
    const verifyBtn = wrapper.findAll('button').find(b => b.text().includes('DNS verifizieren'))
    expect(verifyBtn.attributes('disabled')).toBeUndefined()
    // No row without a check error.
    await rowAction(wrapper, 1, 'Bearbeiten')
    expect(wrapper.find('[data-test="domain-check-failed"]').exists()).toBe(false)
  })

  it('nennt im Slug-Hinweis ohne konfigurierte Basisdomain den Pfad statt einer Subdomain', async () => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', '')
    const { wrapper } = await mountPages()

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-slug').setValue('acme')
    const hint = wrapper.find(`#${wrapper.find('#page-slug').attributes('aria-describedby')}`)
    expect(hint.text()).toContain('/status/acme')
    expect(hint.text()).not.toContain('.example.com')
  })

  it('markiert Name immer und Slug nur beim Bearbeiten als Pflichtfeld und hängt den Slug-Hinweis an', async () => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', 'example.com')
    const { wrapper } = await mountPages()

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    expect(wrapper.find('#page-name').attributes('required')).toBeDefined()
    expect(wrapper.find('#page-slug').attributes('required')).toBeUndefined()
    const hint = wrapper.find(`#${wrapper.find('#page-slug').attributes('aria-describedby')}`)
    expect(hint.text()).toContain('.example.com')

    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen').trigger('click')
    await rowAction(wrapper, 0, 'Bearbeiten')
    expect(wrapper.find('#page-slug').attributes('required')).toBeDefined()
    expect(wrapper.find('#page-slug').attributes('aria-describedby')).toBeUndefined()
  })

  it('zeigt in der Benutzerverwaltung den Leerzustand ohne zugewiesene Benutzer', async () => {
    const { wrapper } = await mountPages()

    await rowAction(wrapper, 0, 'Benutzer')
    await flushPromises()
    expect(wrapper.find('.empty-state').exists()).toBe(true)
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const { wrapper } = await mountPages({ fetchImpl: makeFetchRouter({ loadOk: false }) })

    expect(wrapper.find('.alert-warning').text()).toContain('kaputt')
  })

  // The address is now only a reply address (Reply-To); the sender address
  // belongs to the server. A mailbox invented from the slug is therefore no longer prefilled.
  it('generiert den Slug live aus dem Namen einer neuen Seite und lässt die Antwortadresse leer', async () => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', 'example.com')
    const { wrapper } = await mountPages()

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-name').setValue('Müller Statusseite')

    expect(wrapper.find('#page-slug').element.value).toBe('mueller-statusseite')
    expect(wrapper.find('#page-notification-from').element.value).toBe('')
  })

  it('generiert den Slug nicht mehr automatisch, sobald der Nutzer ihn selbst bearbeitet hat', async () => {
    const { wrapper } = await mountPages()

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-slug').setValue('custom-slug')
    await wrapper.find('#page-name').setValue('Anderer Name')

    expect(wrapper.find('#page-slug').element.value).toBe('custom-slug')
  })

  it('legt eine neue Statusseite per POST an', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-name').setValue('Neue Seite')
    await submitForm(wrapper)
    await flushPromises()

    const [url, options] = fetchMock.mock.calls.find(([u, o]) => o?.method === 'POST' && u === '/api/admin/pages')
    expect(url).toBe('/api/admin/pages')
    const payload = JSON.parse(options.body)
    expect(payload.Name).toBe('Neue Seite')
    expect(payload.Slug).toBe('neue-seite')
    expect(wrapper.find('.edit-card').exists()).toBe(false)
  })

  it('sendet für eine neue Statusseite alle Standardwerte unverändert', async () => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', 'example.com')
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-name').setValue('Test')
    await submitForm(wrapper)
    await flushPromises()

    const [, options] = fetchMock.mock.calls.find(([u, o]) => o?.method === 'POST' && u === '/api/admin/pages')
    expect(JSON.parse(options.body)).toEqual({
      Slug: 'test', Name: 'Test', Description: '', IsPrivate: false, DefaultRoleID: null,
      AutoJoinEmailDomains: '', LogoURL: '', PrimaryColor: '', SecondaryColor: '',
      HeaderStyle: 'classic', PageLayout: 'standard', NotificationFromAddress: '',
    })
  })

  // The submit guard (utils/formValidation.ts) would otherwise replace the view's
  // field message with the English browser hint.
  it('Formular meldet sich beim Submit-Wächter als selbstprüfend an', async () => {
    const { wrapper: attached } = await mountPages({ fetchImpl: makeFetchRouter(), attach: true })
    await attached.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    expect(attached.find('form.edit-form').attributes('data-self-validates')).toBeDefined()
  })

  // Direction C: the required-field error is at the field and focus jumps
  // there; no request goes to the server.
  it('meldet einen leeren Namen am Feld, fokussiert es und sendet nichts', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper: attached } = await mountPages({ fetchImpl: fetchMock, attach: true })
    await attached.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await submitForm(attached)
    await flushPromises()

    const input = attached.find('#page-name')
    expect(input.attributes('aria-invalid')).toBe('true')
    expect(attached.find('.form-field__message--error').text()).toContain('Bitte einen Namen eingeben.')
    expect(document.activeElement).toBe(input.element)
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'POST' && o.body)).toBe(false)

    await input.setValue('x')
    expect(attached.find('.form-field__message--error').exists()).toBe(false)
  })

  it('verlangt beim Bearbeiten auch einen Slug und meldet das am Feld', async () => {
    const { wrapper } = await mountPages()

    await rowAction(wrapper, 0, 'Bearbeiten')
    await wrapper.find('#page-slug').setValue('')
    await submitForm(wrapper)
    await flushPromises()

    expect(wrapper.find('#page-slug').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('#page-slug').attributes('aria-describedby')).toBe('page-slug-error')
    expect(wrapper.find('#page-slug-error').text()).toContain('Bitte einen Slug eingeben.')
  })

  // Stage 4d: all fields are in FormField; each must really write its value
  // into the form (description, logo, colours as colour picker
  // AND hex field, header style, default role).
  it('überträgt jedes Formularfeld in den POST', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-name').setValue('Voll')
    await wrapper.find('#page-description').setValue('Beschreibung')
    await wrapper.find('#page-logo-url').setValue('https://x.example/logo.png')
    await wrapper.find('#page-primary-color').setValue('#112233')
    await wrapper.find('input[aria-label="Primärfarbe als Hex-Wert"]').setValue('#445566')
    await wrapper.find('#page-secondary-color').setValue('#778899')
    await wrapper.find('input[aria-label="Sekundärfarbe als Hex-Wert"]').setValue('#aabbcc')
    await wrapper.find('#page-header-style').setValue('centered')
    await wrapper.find('#isPrivate').setValue(true)
    await wrapper.find('#page-default-role').setValue('3')
    await submitForm(wrapper)
    await flushPromises()

    const [, options] = fetchMock.mock.calls.find(([u, o]) => o?.method === 'POST' && u === '/api/admin/pages')
    expect(JSON.parse(options.body)).toMatchObject({
      Name: 'Voll', Description: 'Beschreibung', LogoURL: 'https://x.example/logo.png',
      PrimaryColor: '#445566', SecondaryColor: '#aabbcc', HeaderStyle: 'centered',
      IsPrivate: true, DefaultRoleID: 3,
    })
  })

  it('bietet das Seiten-Layout mit Vorschau je Variante an und speichert die Wahl', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    const options = wrapper.findAll('.layout-option')
    expect(options).toHaveLength(2)
    expect(wrapper.findAll('.layout-preview')).toHaveLength(2)
    expect(options[0].text()).toContain('Standard')
    expect(options[0].text()).toContain('Bisheriges Aussehen')
    expect(options[1].text()).toContain('Liste')
    expect(options[1].text()).toContain('Vorfall-Zeitleiste')
    expect(options[0].classes()).toContain('layout-option--active')
    expect(options[1].classes()).not.toContain('layout-option--active')

    await options[1].find('input').setValue(true)
    expect(options[1].classes()).toContain('layout-option--active')
    await wrapper.find('#page-name').setValue('Liste')
    await submitForm(wrapper)
    await flushPromises()

    const [, req] = fetchMock.mock.calls.find(([u, o]) => o?.method === 'POST' && u === '/api/admin/pages')
    expect(JSON.parse(req.body).PageLayout).toBe('liste')
  })

  it('übernimmt das gespeicherte Seiten-Layout beim Bearbeiten', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await rowAction(wrapper, 0, 'Bearbeiten')
    await wrapper.findAll('.layout-option')[1].find('input').setValue(true)
    await submitForm(wrapper)
    await flushPromises()

    const [, req] = fetchMock.mock.calls.find(([u, o]) => o?.method === 'PUT' && u === '/api/admin/pages/1')
    expect(JSON.parse(req.body).PageLayout).toBe('liste')
  })

  it('zeigt das Default-Rollen-Feld nur, wenn "Private" aktiviert ist', async () => {
    const { wrapper } = await mountPages()

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    expect(wrapper.find('#page-default-role').exists()).toBe(false)

    await wrapper.find('#isPrivate').setValue(true)
    expect(wrapper.find('#page-default-role').exists()).toBe(true)
    expect(wrapper.find('#page-autojoin-domains').exists()).toBe(true)
  })

  // "Global Admin" is a global property, not a page role.
  it('bietet "Global Admin" nicht als Default-Rolle einer Seite an', async () => {
    const { wrapper } = await mountPages()

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#isPrivate').setValue(true)
    const options = wrapper.find('#page-default-role').findAll('option').map(o => o.text())
    expect(options).toContain('Viewer')
    expect(options).not.toContain('Global Admin')
  })

  it('bietet "Global Admin" nicht als zuweisbare Seitenrolle an', async () => {
    const { wrapper } = await mountPages()

    await rowAction(wrapper, 0, 'Benutzer')
    await flushPromises()
    const options = wrapper.find('#assign-role').findAll('option').map(o => o.text())
    expect(options).toContain('Viewer')
    expect(options).not.toContain('Global Admin')
  })

  it('übersetzt den Fehlercode beim Zuweisen (global_admin_role_not_assignable)', async () => {
    const fetchImpl = makeFetchRouter({ writeOk: false, writeStatus: 400, writeError: { error: 'x', code: 'global_admin_role_not_assignable' } })
    const { wrapper } = await mountPages({ fetchImpl })

    await rowAction(wrapper, 0, 'Benutzer')
    await flushPromises()
    await wrapper.find('#assign-user').setValue('1')
    await wrapper.find('#assign-role').setValue('3')
    await wrapper.findAll('button').find(b => b.text().includes('Hinzufügen')).trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert').text()).toContain('ist keine Seitenrolle')
  })

  it('sendet die Auto-Join-Domains mit', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-name').setValue('Intern')
    await wrapper.find('#isPrivate').setValue(true)
    await wrapper.find('#page-autojoin-domains').setValue('acme.example')
    await submitForm(wrapper)
    await flushPromises()

    const [, options] = fetchMock.mock.calls.find(([u, o]) => o?.method === 'POST' && u === '/api/admin/pages')
    expect(JSON.parse(options.body).AutoJoinEmailDomains).toBe('acme.example')
  })

  it('lädt eine bestehende Seite zum Bearbeiten, ohne den Slug beim Namensänderung zu überschreiben', async () => {
    const { wrapper } = await mountPages()

    await rowAction(wrapper, 0, 'Bearbeiten')
    expect(wrapper.find('#page-name').element.value).toBe('Acme Corp')
    expect(wrapper.find('#page-slug').element.value).toBe('acme')

    await wrapper.find('#page-name').setValue('Acme Corp Renamed')
    expect(wrapper.find('#page-slug').element.value).toBe('acme')
  })

  it('speichert eine bestehende Statusseite per PUT und schließt das Formular', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await rowAction(wrapper, 0, 'Bearbeiten')
    await wrapper.find('#page-name').setValue('Acme Corp Renamed')
    await submitForm(wrapper)
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/admin/pages/1', expect.objectContaining({ method: 'PUT' }))
    expect(wrapper.find('.edit-card').exists()).toBe(false)
  })

  it('löscht eine Statusseite nach Bestätigung per DELETE', async () => {
    stubConfirm(true)
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await rowAction(wrapper, 0, 'Löschen')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/admin/pages/1', expect.objectContaining({ method: 'DELETE' }))
  })

  it('öffnet die öffentliche Seite in einem neuen Tab', async () => {
    vi.stubGlobal('open', vi.fn())
    const { wrapper } = await mountPages()

    await rowAction(wrapper, 0, 'Öffnen')

    expect(window.open).toHaveBeenCalledWith('/status/acme', '_blank')
  })

  it('öffnet die Benutzerverwaltung, weist einen Benutzer zu und entfernt ihn wieder', async () => {
    const fetchMock = vi.fn(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'GET' && /\/api\/pages$/.test(url)) return jsonResponse(pagesFixture())
      if (method === 'GET' && /\/api\/users$/.test(url)) return jsonResponse(usersFixture())
      if (method === 'GET' && /\/api\/roles$/.test(url)) return jsonResponse(rolesFixture())
      if (method === 'GET' && /\/invitations$/.test(url)) return jsonResponse([])
      if (method === 'GET' && /\/pages\/1\/users$/.test(url)) {
        return jsonResponse(fetchMock.assigned ? [{ ID: 1, User: { ID: 1, Email: 'a@b.de' }, Role: { Name: 'Viewer' } }] : [])
      }
      if (method === 'POST' && /\/pages\/1\/users$/.test(url)) {
        fetchMock.assigned = true
        return jsonResponse({})
      }
      if (method === 'DELETE' && /\/pages\/1\/users\/1$/.test(url)) {
        fetchMock.assigned = false
        return jsonResponse({})
      }
      throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
    })
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await rowAction(wrapper, 0, 'Benutzer')
    await flushPromises()
    expect(wrapper.find('.empty-state').text()).toContain('Noch keine Benutzer zugewiesen.')

    await wrapper.find('#assign-user').setValue('1')
    await wrapper.find('#assign-role').setValue('3')
    await wrapper.findAll('button').find(b => b.text().includes('Hinzufügen')).trigger('click')
    await flushPromises()

    expect(wrapper.find('aside tbody tr').text()).toContain('a@b.de')

    stubConfirm(true)
    await wrapper.find('aside .delete-btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('aside .empty-state').exists()).toBe(true)
  })

  it('zeigt für Zuweisungen ohne Benutzer- oder Rolleninfo einen Gedankenstrich', async () => {
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options = {}) =>
      /\/pages\/1\/users$/.test(url) && (options.method || 'GET') === 'GET'
        ? jsonResponse([{ ID: 9, User: null, Role: null }])
        : base(url, options))
    const { wrapper } = await mountPages({ fetchImpl })

    await rowAction(wrapper, 0, 'Benutzer')
    await flushPromises()

    const cells = wrapper.findAll('aside tbody tr td')
    expect(cells[0].text()).toBe('–')
    expect(cells[1].text()).toBe('–')
  })

  it('generiert, kopiert und widerruft einen Monitoring-Webhook-Token', async () => {
    stubConfirm(true)
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockResolvedValue() } })
    const fetchMock = vi.fn(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'GET' && /\/api\/pages$/.test(url)) return jsonResponse(pagesFixture())
      if (method === 'GET' && /\/api\/users$/.test(url)) return jsonResponse(usersFixture())
      if (method === 'GET' && /\/api\/roles$/.test(url)) return jsonResponse(rolesFixture())
      if (method === 'GET' && /\/invitations$/.test(url)) return jsonResponse([])
      if (method === 'POST' && /\/webhook-token$/.test(url)) return jsonResponse({ token: 'whk-secret-123' })
      if (method === 'DELETE' && /\/webhook-token$/.test(url)) return jsonResponse({})
      throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
    })
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await rowAction(wrapper, 0, 'Bearbeiten')
    await wrapper.findAll('button').find(b => b.text().includes('Token erzeugen')).trigger('click')
    await flushPromises()

    expect(wrapper.find('#webhook-token-display').element.value).toBe('whk-secret-123')
    // Focus selects the token for copying.
    const select = vi.spyOn(wrapper.find('#webhook-token-display').element, 'select')
    await wrapper.find('#webhook-token-display').trigger('focus')
    expect(select).toHaveBeenCalled()

    await wrapper.findAll('button').find(b => b.text().includes('Kopieren') || b.text().includes('Copy')).trigger('click')
    await flushPromises()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('whk-secret-123')

    await wrapper.findAll('button').find(b => b.text().includes('widerrufen')).trigger('click')
    await flushPromises()

    expect(wrapper.find('#webhook-token-display').exists()).toBe(false)
  })

  // The CNAME target comes from the backend before anything is saved (it used to be guessed as
  // edge.<app domain> or edge.example.com), the TXT value reappears when the form is reopened,
  // apex domains get a hint, and a successful check says whether the domain already points to the target.
  describe('custom domain DNS hints', () => {
    const domainRouter = ({ info, verify } = {}) => {
      const base = makeFetchRouter()
      return vi.fn(async (url, options = {}) => {
        const method = options.method || 'GET'
        if (method === 'GET' && /\/custom-domain$/.test(url)) {
          return info ? jsonResponse(info) : jsonResponse({ error: 'kaputt' }, 500)
        }
        if (method === 'POST' && /\/custom-domain\/verify$/.test(url)) return jsonResponse(verify)
        return base(url, options)
      })
    }

    it('shows the real CNAME target and the open TXT value when the form opens', async () => {
      const fetchImpl = domainRouter({ info: { cname_target: 'edge.statushafen.test', custom_domain: 'status.beta.example', custom_domain_status: 'pending', apex_domain: false, txt_record_value: 'statushafen-verify=open' } })
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 1, 'Bearbeiten')
      await flushPromises()
      expect(fetchImpl).toHaveBeenCalledWith('/api/admin/pages/2/custom-domain', expect.anything())
      const desc = wrapper.find('[data-test="custom-domain-desc"]')
      expect(desc.text()).toContain('edge.statushafen.test')
      expect(wrapper.text()).toContain('statushafen-verify=open')
      expect(wrapper.find('[data-test="apex-domain-hint"]').exists()).toBe(false)
    })

    it('ignores a late answer for a page that is no longer open', async () => {
      const base = makeFetchRouter()
      let answerBeta
      const fetchImpl = vi.fn(async (url, options = {}) => {
        const method = options.method || 'GET'
        if (method === 'GET' && /\/pages\/2\/custom-domain$/.test(url)) {
          return new Promise(resolve => { answerBeta = () => resolve(jsonResponse({ cname_target: 'edge.beta.test', custom_domain_status: 'pending', txt_record_value: 'beta-txt' })) })
        }
        if (method === 'GET' && /\/pages\/1\/custom-domain$/.test(url)) {
          return jsonResponse({ cname_target: 'edge.acme.test', custom_domain_status: '' })
        }
        return base(url, options)
      })
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 1, 'Bearbeiten')
      await rowAction(wrapper, 0, 'Bearbeiten')
      await flushPromises()
      answerBeta()
      await flushPromises()
      expect(wrapper.find('[data-test="custom-domain-desc"]').text()).toContain('edge.acme.test')
      expect(wrapper.text()).not.toContain('beta-txt')
    })

    it('does not guess a target when it cannot be loaded', async () => {
      const { wrapper } = await mountPages({ fetchImpl: domainRouter() })
      await rowAction(wrapper, 1, 'Bearbeiten')
      await flushPromises()
      const desc = wrapper.find('[data-test="custom-domain-desc"]')
      expect(desc.text()).not.toContain('edge.')
      expect(desc.text()).toContain('…')
    })

    it('warns about apex domains', async () => {
      const fetchImpl = domainRouter({ info: { cname_target: 'edge.statushafen.test', custom_domain: 'beta.example', custom_domain_status: 'pending', apex_domain: true } })
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 1, 'Bearbeiten')
      await flushPromises()
      expect(wrapper.find('[data-test="apex-domain-hint"]').text()).toContain('CNAME')
    })

    it('says after a successful check whether the domain points to the target yet', async () => {
      const info = { cname_target: 'edge.statushafen.test', custom_domain: 'status.beta.example', custom_domain_status: 'pending', apex_domain: false }
      for (const [points, shown] of [[false, true], [true, false]]) {
        const fetchImpl = domainRouter({ info, verify: { custom_domain_status: 'verified', verified: true, points_to_target: points, apex_domain: false, cname_target: 'edge.statushafen.test', message: 'ok', verified_at: '2026-11-26T10:00:00Z' } })
        const { wrapper } = await mountPages({ fetchImpl })
        await rowAction(wrapper, 1, 'Bearbeiten')
        await flushPromises()
        await wrapper.findAll('button').find(b => b.text().includes('DNS verifizieren')).trigger('click')
        await flushPromises()
        const warning = wrapper.find('[data-test="domain-not-pointing"]')
        expect(warning.exists(), `points_to_target=${points}`).toBe(shown)
        if (shown) expect(warning.text()).toContain('edge.statushafen.test')
        wrapper.unmount()
        activeWrapper = null
      }
    })
  })

  it('speichert und verifiziert eine Custom Domain', async () => {
    const fetchMock = vi.fn(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'GET' && /\/api\/pages$/.test(url)) return jsonResponse(pagesFixture())
      if (method === 'GET' && /\/api\/users$/.test(url)) return jsonResponse(usersFixture())
      if (method === 'GET' && /\/api\/roles$/.test(url)) return jsonResponse(rolesFixture())
      if (method === 'GET' && /\/invitations$/.test(url)) return jsonResponse([])
      if (method === 'PUT' && /\/custom-domain$/.test(url)) {
        return jsonResponse({ custom_domain_status: 'pending', cname_target: 'edge.example.com', txt_record_value: 'txt-abc' })
      }
      if (method === 'POST' && /\/custom-domain\/verify$/.test(url)) {
        return jsonResponse({ custom_domain_status: 'verified', message: 'DNS ok', verified_at: '2026-08-01T10:00:00Z' })
      }
      throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
    })
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await rowAction(wrapper, 0, 'Bearbeiten')
    await wrapper.find('#custom-domain-input').setValue('status.acme.example')
    // .action-btn distinguishes it from the main form's OnyxButton "Save",
    // which carries the same label text (t('common.save')).
    await wrapper.findAll('.action-btn').find(b => b.text() === 'Speichern').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Ausstehend')
    expect(wrapper.text()).toContain('txt-abc')

    await wrapper.findAll('button').find(b => b.text().includes('DNS verifizieren')).trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('DNS ok')
  })

  it('reports failing custom domain and webhook token requests with the server message', async () => {
    stubConfirm(true)
    const { wrapper } = await mountPages({ fetchImpl: makeFetchRouter({ writeOk: false }) })
    const clickButton = async (text) => {
      await wrapper.findAll('.action-btn').find(b => b.text().includes(text)).trigger('click')
      await flushPromises()
    }

    // Beta Internal already has a pending domain, so verify and remove are offered.
    await rowAction(wrapper, 1, 'Bearbeiten')
    await wrapper.find('#custom-domain-input').setValue('status.beta.example')
    await clickButton('Speichern')
    expect(wrapper.text()).toContain('Domain konnte nicht gespeichert werden: Fehlgeschlagen')
    await clickButton('DNS verifizieren')
    expect(wrapper.text()).toContain('Verifikation fehlgeschlagen: Fehlgeschlagen')
    await clickButton('Domain entfernen')
    expect(wrapper.text()).toContain('Domain konnte nicht entfernt werden: Fehlgeschlagen')
    await clickButton('Token erzeugen')
    expect(wrapper.text()).toContain('Token konnte nicht erzeugt werden: Fehlgeschlagen')
    await clickButton('Token widerrufen')
    expect(wrapper.text()).toContain('Token konnte nicht widerrufen werden: Fehlgeschlagen')
  })

  it('reports a failed copy of the webhook token and the TXT record', async () => {
    stubConfirm(true)
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'POST' && /\/webhook-token$/.test(url)) return jsonResponse({ token: 'whk-secret-123' })
      if (method === 'PUT' && /\/custom-domain$/.test(url)) {
        return jsonResponse({ custom_domain_status: 'pending', cname_target: 'edge.example.com', txt_record_value: 'txt-abc' })
      }
      return base(url, options)
    })
    const { wrapper } = await mountPages({ fetchImpl })

    await rowAction(wrapper, 0, 'Bearbeiten')
    await wrapper.findAll('button').find(b => b.text().includes('Token erzeugen')).trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text().includes('Kopieren')).trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Kopieren fehlgeschlagen: denied')

    await wrapper.find('.alert-close').trigger('click')
    expect(wrapper.text()).not.toContain('Kopieren fehlgeschlagen')
    await wrapper.find('#custom-domain-input').setValue('status.acme.example')
    await wrapper.findAll('.action-btn').find(b => b.text() === 'Speichern').trigger('click')
    await flushPromises()
    await wrapper.find('.txt-box .action-btn').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Kopieren fehlgeschlagen: denied')
    vi.unstubAllGlobals()
  })

  it('versteckt Neu-Button, Edit/Delete für Nicht-Global-Admins, zeigt aber Öffnen/Benutzer', async () => {
    const { wrapper } = await mountPages({ isGlobalAdmin: false })

    expect(wrapper.findAll('button').some(b => b.text().includes('Neue Statusseite'))).toBe(false)
    const labels = wrapper.findAll('tbody tr')[0].findAll('[role="menuitem"]').map(b => b.text())
    expect(labels).toEqual(['Öffnen', 'Benutzer'])
  })

  it('hat keine axe-Funde in Liste und Bearbeitungsformular', async () => {
    const { wrapper } = await mountPages()
    expect(await axe(wrapper)).toHaveNoViolations()

    await rowAction(wrapper, 0, 'Bearbeiten')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('hat keine axe-Funde mit Webhook-Token und ausstehender Custom Domain', async () => {
    stubConfirm(true)
    const fetchMock = vi.fn(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'GET' && /\/api\/pages$/.test(url)) return jsonResponse(pagesFixture())
      if (method === 'GET' && /\/api\/users$/.test(url)) return jsonResponse(usersFixture())
      if (method === 'GET' && /\/api\/roles$/.test(url)) return jsonResponse(rolesFixture())
      if (method === 'GET' && /\/invitations$/.test(url)) return jsonResponse([])
      if (method === 'POST' && /\/webhook-token$/.test(url)) return jsonResponse({ token: 'whk-secret-123' })
      if (method === 'PUT' && /\/custom-domain$/.test(url)) {
        return jsonResponse({ custom_domain_status: 'pending', cname_target: 'edge.example.com', txt_record_value: 'txt-abc' })
      }
      throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
    })
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await rowAction(wrapper, 0, 'Bearbeiten')
    await wrapper.findAll('button').find(b => b.text().includes('Token erzeugen')).trigger('click')
    await wrapper.find('#custom-domain-input').setValue('status.acme.example')
    await wrapper.findAll('.action-btn').find(b => b.text() === 'Speichern').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('txt-abc')
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('hat keine axe-Funde in der Benutzerverwaltung', async () => {
    const fetchMock = vi.fn(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'GET' && /\/api\/pages$/.test(url)) return jsonResponse(pagesFixture())
      if (method === 'GET' && /\/api\/users$/.test(url)) return jsonResponse(usersFixture())
      if (method === 'GET' && /\/api\/roles$/.test(url)) return jsonResponse(rolesFixture())
      if (method === 'GET' && /\/invitations$/.test(url)) return jsonResponse([])
      if (method === 'GET' && /\/pages\/1\/users$/.test(url)) {
        return jsonResponse([{ ID: 1, User: { ID: 1, Email: 'a@b.de' }, Role: { Name: 'Viewer' } }])
      }
      throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
    })
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })

    await rowAction(wrapper, 0, 'Benutzer')
    await flushPromises()

    expect(wrapper.find('aside tbody tr').text()).toContain('a@b.de')
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('übernimmt eine selbst getippte Absenderadresse und leitet sie danach nicht mehr aus dem Slug ab', async () => {
    const { wrapper } = await mountPages()

    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-notification-from').setValue('mein@absender.example')
    await wrapper.find('#page-name').setValue('Neu')

    expect(wrapper.find('#page-notification-from').element.value).toBe('mein@absender.example')
  })

  it('kopiert den TXT-Eintrag und entfernt eine Custom Domain nach Bestätigung', async () => {
    stubConfirm(true)
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockResolvedValue() } })
    const base = makeFetchRouter()
    const fetchImpl = vi.fn(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'PUT' && /\/custom-domain$/.test(url)) {
        return jsonResponse({ custom_domain_status: 'pending', cname_target: 'edge.example.com', txt_record_value: 'txt-abc' })
      }
      if (method === 'DELETE' && /\/custom-domain$/.test(url)) return jsonResponse({})
      return base(url, options)
    })
    const { wrapper } = await mountPages({ fetchImpl })

    await rowAction(wrapper, 0, 'Bearbeiten')
    await wrapper.find('#custom-domain-input').setValue('status.acme.example')
    await wrapper.findAll('.action-btn').find(b => b.text() === 'Speichern').trigger('click')
    await flushPromises()
    await wrapper.find('.txt-box .action-btn').trigger('click')
    await flushPromises()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('txt-abc')
    expect(wrapper.find('.txt-box .app-icon').exists()).toBe(true)

    await wrapper.findAll('button').find(b => b.text().includes('Domain entfernen')).trigger('click')
    await flushPromises()
    expect(fetchImpl).toHaveBeenCalledWith('/api/admin/pages/1/custom-domain', expect.objectContaining({ method: 'DELETE' }))
    expect(wrapper.find('#custom-domain-input').element.value).toBe('')
    vi.unstubAllGlobals()
  })

  it('schließt die Benutzerverwaltung über den Schließen-Knopf', async () => {
    const { wrapper } = await mountPages()

    await rowAction(wrapper, 0, 'Benutzer')
    await flushPromises()
    expect(wrapper.find('aside').exists()).toBe(true)
    await wrapper.find('aside .form-panel__close').trigger('click')
    expect(wrapper.find('aside').exists()).toBe(false)
  })

  // Backend 400 slug_reserved / name_required appear at the field.
  it('zeigt slug_reserved vom Server am Slug-Feld statt als Sammelmeldung', async () => {
    const fetchMock = makeFetchRouter({ writeOk: false, writeStatus: 400, writeError: { error: 'x', code: 'slug_reserved' } })
    const { wrapper } = await mountPages({ fetchImpl: fetchMock, attach: true })
    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-name').setValue('WWW')
    await submitForm(wrapper)
    await flushPromises()

    expect(wrapper.find('#page-slug').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('.form-field__message--error').text()).toContain('reserviert')
    expect(document.activeElement).toBe(wrapper.find('#page-slug').element)
  })

  // 409 slug_taken (also from the race when creating in parallel) at the slug field.
  it('zeigt slug_taken vom Server am Slug-Feld', async () => {
    const fetchMock = makeFetchRouter({ writeOk: false, writeStatus: 409, writeError: { error: 'x', code: 'slug_taken' } })
    const { wrapper } = await mountPages({ fetchImpl: fetchMock, attach: true })
    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-name').setValue('Acme')
    await submitForm(wrapper)
    await flushPromises()
    expect(wrapper.find('#page-slug').attributes('aria-invalid')).toBe('true')
    expect(wrapper.find('.form-field__message--error').text()).toContain('bereits')
  })

  it('zeigt name_required vom Server am Namensfeld', async () => {
    const fetchMock = makeFetchRouter({ writeOk: false, writeStatus: 400, writeError: { error: 'x', code: 'name_required' } })
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-name').setValue('Acme')
    await submitForm(wrapper)
    await flushPromises()
    expect(wrapper.find('#page-name').attributes('aria-invalid')).toBe('true')
  })

  it('zeigt andere Serverfehler weiter als Sammelmeldung', async () => {
    const fetchMock = makeFetchRouter({ writeOk: false, writeStatus: 500, writeError: { error: 'kaputt' } })
    const { wrapper } = await mountPages({ fetchImpl: fetchMock })
    await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
    await wrapper.find('#page-name').setValue('Acme')
    await submitForm(wrapper)
    await flushPromises()
    expect(wrapper.find('#page-name').attributes('aria-invalid')).toBeUndefined()
    expect(wrapper.text()).toContain('kaputt')
  })
  // "Remove" in the user assignment deleted without confirmation and without a message.
  describe('Zuweisung entfernen', () => {
    const assignment = { ID: 9, User: { ID: 7, Email: 'x@y.de' }, Role: { ID: 3, Name: 'Viewer' } }
    const mountWithAssignment = async () => {
      let removed = false
      const base = makeFetchRouter()
      const fetchImpl = vi.fn(async (url, options = {}) => {
        const method = options.method || 'GET'
        if (method === 'DELETE' && /\/pages\/1\/users\/7$/.test(url)) { removed = true; return jsonResponse({}) }
        if (method === 'GET' && /\/pages\/1\/users$/.test(url)) return jsonResponse(removed ? [] : [assignment])
        return base(url, options)
      })
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Benutzer')
      await flushPromises()
      return { wrapper, fetchImpl }
    }

    it('fragt nach und löscht nichts, wenn abgelehnt wird', async () => {
      const confirm = stubConfirm(false)
      const { wrapper, fetchImpl } = await mountWithAssignment()

      await wrapper.find('aside .delete-btn').trigger('click')
      await flushPromises()

      expect(confirm).toHaveBeenCalledWith(expect.stringContaining('x@y.de'))
      expect(fetchImpl.mock.calls.some(([, o]) => o?.method === 'DELETE')).toBe(false)
    })

    it('zeigt einen Fehler als Warnung, wenn das Entfernen fehlschlägt, und nennt ohne Benutzerinfo einen Gedankenstrich', async () => {
      const confirm = stubConfirm(true)
      const base = makeFetchRouter()
      const fetchImpl = vi.fn(async (url, options = {}) => {
        const method = options.method || 'GET'
        if (method === 'DELETE' && /\/pages\/1\/users\//.test(url)) return jsonResponse({ error: 'kaputt' }, 500)
        if (method === 'GET' && /\/pages\/1\/users$/.test(url)) return jsonResponse([{ ID: 9, User: null, Role: null }])
        return base(url, options)
      })
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Benutzer')
      await flushPromises()

      await wrapper.find('aside .delete-btn').trigger('click')
      await flushPromises()

      expect(confirm).toHaveBeenCalledWith(expect.stringContaining('–'))
      expect(wrapper.find('.alert-warning').text()).toContain('kaputt')
    })

    it('meldet nach der Bestätigung „Zuweisung entfernt.“ als Erfolg', async () => {
      stubConfirm(true)
      const { wrapper } = await mountWithAssignment()

      await wrapper.find('aside .delete-btn').trigger('click')
      await flushPromises()

      expect(wrapper.find('.alert-success').text()).toContain('Zuweisung entfernt.')
    })
  })

  // open invitations (invited by email, not yet accepted).
  describe('Offene Einladungen', () => {
    const invitation = { ID: 5, Email: 'neu@x.de', RoleID: 3, RoleName: 'Viewer', ExpiresAt: '2099-01-01T00:00:00Z' }
    const mountWithInvitation = async ({ deleteStatus = 200, roleName = 'Viewer' } = {}) => {
      let revoked = false
      const base = makeFetchRouter()
      const fetchImpl = vi.fn(async (url, options = {}) => {
        const method = options.method || 'GET'
        if (method === 'DELETE' && /\/pages\/1\/invitations\/5$/.test(url)) {
          if (deleteStatus !== 200) return jsonResponse({ error: 'weg' }, deleteStatus)
          revoked = true
          return jsonResponse({})
        }
        if (method === 'GET' && /\/pages\/1\/invitations$/.test(url)) return jsonResponse(revoked ? [] : [{ ...invitation, RoleName: roleName }])
        return base(url, options)
      })
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Benutzer')
      await flushPromises()
      return { wrapper, fetchImpl }
    }

    // H6: if only the invitation list fails, the member list stays visible.
    it('zeigt die Mitglieder auch dann, wenn die Einladungsliste fehlschlägt', async () => {
      const base = makeFetchRouter()
      const fetchImpl = vi.fn(async (url, options = {}) => {
        const method = options.method || 'GET'
        if (method === 'GET' && /\/pages\/1\/invitations$/.test(url)) return jsonResponse({ error: 'kaputt' }, 500)
        if (method === 'GET' && /\/pages\/1\/users$/.test(url)) return jsonResponse([{ ID: 9, User: { ID: 7, Email: 'mitglied@x.de' }, Role: { ID: 3, Name: 'Viewer' } }])
        return base(url, options)
      })
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Benutzer')
      await flushPromises()
      expect(wrapper.find('aside').text()).toContain('mitglied@x.de')
      expect(wrapper.find('aside').text()).toContain('Keine offenen Einladungen.')
    })

    // H6: a late response of the previously opened page does not overwrite the lists of the current one.
    it('verwirft die späte Antwort einer zuvor geöffneten Seite', async () => {
      let releaseFirst
      const slowFirst = new Promise((resolve) => { releaseFirst = resolve })
      const base = makeFetchRouter()
      const member = (email) => [{ ID: 9, User: { ID: 7, Email: email }, Role: { ID: 3, Name: 'Viewer' } }]
      const fetchImpl = vi.fn(async (url, options = {}) => {
        const method = options.method || 'GET'
        if (method === 'GET' && /\/pages\/1\/users$/.test(url)) { await slowFirst; return jsonResponse(member('seite-eins@x.de')) }
        if (method === 'GET' && /\/pages\/2\/users$/.test(url)) return jsonResponse(member('seite-zwei@x.de'))
        return base(url, options)
      })
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Benutzer')
      await flushPromises()
      await rowAction(wrapper, 1, 'Benutzer')
      await flushPromises()
      releaseFirst()
      await flushPromises()
      expect(wrapper.find('aside').text()).toContain('seite-zwei@x.de')
      expect(wrapper.find('aside').text()).not.toContain('seite-eins@x.de')
    })

    // H6: if the member list fails, the caller reports the error (no silent empty state).
    it('meldet einen Fehler, wenn die Mitgliederliste nach dem Entfernen nicht lädt', async () => {
      stubConfirm(true)
      let removed = false
      const base = makeFetchRouter()
      const fetchImpl = vi.fn(async (url, options = {}) => {
        const method = options.method || 'GET'
        if (method === 'DELETE' && /\/pages\/1\/users\/7$/.test(url)) { removed = true; return jsonResponse({}) }
        if (method === 'GET' && /\/pages\/1\/users$/.test(url)) {
          return removed ? jsonResponse({ error: 'kaputt' }, 500) : jsonResponse([{ ID: 9, User: { ID: 7, Email: 'x@y.de' }, Role: { ID: 3, Name: 'Viewer' } }])
        }
        return base(url, options)
      })
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Benutzer')
      await flushPromises()
      await wrapper.find('aside .delete-btn').trigger('click')
      await flushPromises()
      expect(wrapper.find('.alert-warning').text()).toContain('kaputt')
    })

    it('zeigt ohne Einladungen einen leeren Zustand', async () => {
      const { wrapper } = await mountPages()
      await rowAction(wrapper, 0, 'Benutzer')
      await flushPromises()
      expect(wrapper.find('aside').text()).toContain('Offene Einladungen')
      expect(wrapper.find('aside').text()).toContain('Keine offenen Einladungen.')
      expect(wrapper.find('.invitations-table').exists()).toBe(false)
    })

    it('listet Adresse, Rolle und Ablauf der offenen Einladungen', async () => {
      const { wrapper, fetchImpl } = await mountWithInvitation()
      expect(fetchImpl).toHaveBeenCalledWith('/api/admin/pages/1/invitations', expect.anything())
      const row = wrapper.find('.invitations-table tbody tr')
      expect(row.text()).toContain('neu@x.de')
      expect(row.text()).toContain('Viewer')
      expect(row.find('time').exists()).toBe(true)
      expect(row.find('.revoke-invitation-btn').attributes('aria-label')).toBe('Einladung für „neu@x.de“ zurückziehen')
    })

    it('zeigt ohne Rollennamen einen Gedankenstrich', async () => {
      const { wrapper } = await mountWithInvitation({ roleName: '' })
      expect(wrapper.find('.invitations-table .meta-tag').text()).toBe('–')
    })

    it('zieht nach Rückfrage zurück und meldet Erfolg', async () => {
      const confirm = stubConfirm(true)
      const { wrapper, fetchImpl } = await mountWithInvitation()
      await wrapper.find('.revoke-invitation-btn').trigger('click')
      await flushPromises()
      expect(confirm).toHaveBeenCalledWith(expect.stringContaining('neu@x.de'))
      expect(fetchImpl).toHaveBeenCalledWith('/api/admin/pages/1/invitations/5', expect.objectContaining({ method: 'DELETE' }))
      expect(wrapper.find('.alert-success').text()).toContain('Einladung zurückgezogen.')
      expect(wrapper.find('.invitations-table').exists()).toBe(false)
    })

    it('sperrt den Knopf, solange das Zurückziehen läuft', async () => {
      stubConfirm(true)
      const base = makeFetchRouter()
      const fetchImpl = vi.fn(async (url, options = {}) => {
        const method = options.method || 'GET'
        if (method === 'DELETE' && /\/invitations\/5$/.test(url)) return new Promise(() => {})
        if (method === 'GET' && /\/pages\/1\/invitations$/.test(url)) return jsonResponse([invitation])
        return base(url, options)
      })
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Benutzer')
      await flushPromises()
      await wrapper.find('.revoke-invitation-btn').trigger('click')
      await flushPromises()
      const btn = wrapper.find('.revoke-invitation-btn')
      expect(btn.attributes('disabled')).toBeDefined()
      expect(btn.text()).toContain(i18n.global.t('common.removing'))
    })

    it('ruft nichts auf, wenn die Rückfrage abgelehnt wird', async () => {
      stubConfirm(false)
      const { wrapper, fetchImpl } = await mountWithInvitation()
      await wrapper.find('.revoke-invitation-btn').trigger('click')
      await flushPromises()
      expect(fetchImpl.mock.calls.some(([, o]) => o?.method === 'DELETE')).toBe(false)
      expect(wrapper.find('.invitations-table').exists()).toBe(true)
    })

    it('zeigt einen Fehler als Warnung und gibt den Knopf wieder frei', async () => {
      stubConfirm(true)
      const { wrapper } = await mountWithInvitation({ deleteStatus: 500 })
      await wrapper.find('.revoke-invitation-btn').trigger('click')
      await flushPromises()
      expect(wrapper.find('.alert-warning').text()).toContain('Zurückziehen fehlgeschlagen: weg')
      expect(wrapper.find('.revoke-invitation-btn').attributes('disabled')).toBeUndefined()
      expect(wrapper.find('.revoke-invitation-btn').text()).toContain('Zurückziehen')
    })
  })

  // declined confirmations trigger no call; the target is captured before the dialog.
  describe('Rückfragen für Domain und Webhook-Token', () => {
    function tokenAndDomainFetch() {
      const base = makeFetchRouter()
      return vi.fn(async (url, options = {}) => {
        const method = options.method || 'GET'
        if (method === 'POST' && /\/webhook-token$/.test(url)) return jsonResponse({ token: 'whk-1' })
        if (method === 'DELETE' && /\/webhook-token$/.test(url)) return jsonResponse({})
        if (method === 'PUT' && /\/custom-domain$/.test(url)) {
          return jsonResponse({ custom_domain_status: 'pending', cname_target: 'edge.example.com', txt_record_value: 'txt-abc' })
        }
        if (method === 'DELETE' && /\/custom-domain$/.test(url)) return jsonResponse({})
        return base(url, options)
      })
    }
    const deletes = (f) => f.mock.calls.filter(([, o]) => o?.method === 'DELETE')
    const posts = (f) => f.mock.calls.filter(([, o]) => o?.method === 'POST')
    const clickButton = async (wrapper, text) => {
      await wrapper.findAll('button').find(b => b.text().includes(text)).trigger('click')
      await flushPromises()
    }

    it('Token erzeugen: abgelehnt -> kein POST', async () => {
      stubConfirm(false)
      const fetchImpl = tokenAndDomainFetch()
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Bearbeiten')
      await clickButton(wrapper, 'Token erzeugen')
      expect(posts(fetchImpl)).toHaveLength(0)
    })

    it('Token widerrufen: abgelehnt -> kein DELETE', async () => {
      stubConfirm(true)
      const fetchImpl = tokenAndDomainFetch()
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Bearbeiten')
      await clickButton(wrapper, 'Token erzeugen')
      stubConfirm(false)
      await clickButton(wrapper, 'widerrufen')
      expect(deletes(fetchImpl)).toHaveLength(0)
    })

    it('Domain entfernen: abgelehnt -> kein DELETE', async () => {
      stubConfirm(false)
      const fetchImpl = tokenAndDomainFetch()
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Bearbeiten')
      await wrapper.find('#custom-domain-input').setValue('status.acme.example')
      await wrapper.findAll('.action-btn').find(b => b.text() === 'Speichern').trigger('click')
      await flushPromises()
      await clickButton(wrapper, 'Domain entfernen')
      expect(deletes(fetchImpl)).toHaveLength(0)
    })

    it('trifft die Seite, bei der gefragt wurde, auch wenn die Auswahl während des Dialogs wechselt', async () => {
      let answer
      setConfirmHandler(() => new Promise((resolve) => { answer = resolve }))
      const fetchImpl = tokenAndDomainFetch()
      const { wrapper } = await mountPages({ fetchImpl })
      await rowAction(wrapper, 0, 'Bearbeiten')
      await clickButton(wrapper, 'Token erzeugen')
      wrapper.vm.currentEditItem = 2
      answer(true)
      await flushPromises()
      expect(posts(fetchImpl).map(([u]) => u)).toEqual(['/api/admin/pages/1/webhook-token'])
    })
  })

  describe('Design-Vorschau', () => {
    // The real public page is covered by StatusPagePreview.test.js; here only the wiring.
    const StatusPageStub = {
      name: 'StatusPage',
      props: ['slug', 'preview'],
      template: '<div class="status-page-stub" :data-slug="slug">{{ JSON.stringify(preview) }}</div>',
    }
    async function mountWithStub() {
      useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: true })
      const fetchImpl = makeFetchRouter()
      global.fetch = fetchImpl
      const wrapper = mount(AdminStatusPages, { attachTo: document.body, global: { plugins: [i18n], stubs: { StatusPage: StatusPageStub } } })
      await flushPromises()
      activeWrapper = wrapper
      return { wrapper, fetchImpl }
    }
    const openButton = (wrapper) => wrapper.find('[data-test="design-preview-open"]')
    const modal = (wrapper) => wrapper.findComponent({ name: 'OnyxModal' })
    const previewData = (wrapper) => JSON.parse(wrapper.find('.status-page-stub').text())
    const editFirst = async (wrapper) => {
      await rowAction(wrapper, 0, 'Bearbeiten')
      await flushPromises()
    }

    it('öffnet die Vorschau als Modal mit der Seite und dem Hinweis, dass nichts live ist', async () => {
      const { wrapper } = await mountWithStub()
      await editFirst(wrapper)

      expect(modal(wrapper).props('open')).toBe(false)
      expect(wrapper.find('.status-page-stub').exists()).toBe(false)

      await openButton(wrapper).trigger('click')
      await flushPromises()

      expect(modal(wrapper).props('open')).toBe(true)
      expect(modal(wrapper).props('label')).toBe('Vorschau: Acme Corp')
      expect(wrapper.find('.status-page-stub').attributes('data-slug')).toBe('acme')
      expect(wrapper.find('.design-preview-frame').classes()).toContain('theme-public')
      expect(wrapper.find('[data-test="design-preview-hint"]').text()).toContain('Speichern')
    })

    it('zeigt die geänderten, noch nicht gespeicherten Design-Felder, ohne zu speichern', async () => {
      const { wrapper, fetchImpl } = await mountWithStub()
      await editFirst(wrapper)

      await wrapper.find('#page-logo-url').setValue('https://acme.example/logo.svg')
      await wrapper.find('#page-primary-color').setValue('#336699')
      await wrapper.find('#page-header-style').setValue('minimal')
      await wrapper.find('input[name="page-layout"][value="liste"]').setValue(true)
      await openButton(wrapper).trigger('click')
      await flushPromises()

      expect(previewData(wrapper)).toEqual({
        LogoURL: 'https://acme.example/logo.svg', PrimaryColor: '#336699', SecondaryColor: '',
        HeaderStyle: 'minimal', PageLayout: 'liste',
      })
      const writes = fetchImpl.mock.calls.filter(([, o]) => o?.method && o.method !== 'GET')
      expect(writes).toHaveLength(0)
    })

    it('schließt das Modal und lässt das Formular mit den Änderungen offen', async () => {
      const { wrapper } = await mountWithStub()
      await editFirst(wrapper)
      await wrapper.find('#page-header-style').setValue('centered')
      await openButton(wrapper).trigger('click')
      await flushPromises()

      modal(wrapper).vm.$emit('update:open', false)
      await flushPromises()

      expect(modal(wrapper).props('open')).toBe(false)
      expect(wrapper.find('.status-page-stub').exists()).toBe(false)
      expect(wrapper.find('#page-header-style').element.value).toBe('centered')
    })

    it('bietet für eine neue Seite noch keine Vorschau an', async () => {
      const { wrapper } = await mountWithStub()
      await wrapper.findAll('button').find(b => b.text().includes('Neue Statusseite')).trigger('click')
      await flushPromises()

      expect(openButton(wrapper).exists()).toBe(false)
      expect(wrapper.find('[data-test="design-preview-new"]').text()).toContain('ersten Speichern')
    })

    it('schließt die Vorschau mit dem Formular', async () => {
      const { wrapper } = await mountWithStub()
      await editFirst(wrapper)
      await openButton(wrapper).trigger('click')
      await flushPromises()
      await wrapper.findAll('button').find(b => b.text() === 'Abbrechen').trigger('click')
      await rowAction(wrapper, 1, 'Bearbeiten')
      await flushPromises()

      expect(modal(wrapper).props('open')).toBe(false)
    })

    it('hat mit geöffneter Vorschau keine axe-Verstöße', async () => {
      const { wrapper } = await mountWithStub()
      await editFirst(wrapper)
      await openButton(wrapper).trigger('click')
      await flushPromises()

      expect(await axe(wrapper)).toHaveNoViolations()
    })
  })
})
