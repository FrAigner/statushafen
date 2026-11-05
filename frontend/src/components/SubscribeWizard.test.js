import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, DOMWrapper } from '@vue/test-utils'
import i18n from '../i18n'
import SubscribeWizard from './SubscribeWizard.vue'
import { useAuth } from '../composables/useAuth'
import { axe } from '../test-support/axe.js'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Die komplette Wizard-Vorlage liegt in <Teleport to="body">, ist also
// außerhalb des von wrapper.find() durchsuchten Baums (gleiches Muster wie
// StatusPage.test.js für das Incident-History-Modal) - alle Queries laufen
// daher gegen document.body statt gegen den Mount-Wrapper.
const body = () => new DOMWrapper(document.body)

const components = [
  { ID: 1, Name: 'API', Tags: [{ Name: 'EU' }] },
  { ID: 2, Name: 'Web', Tags: [{ Name: 'US' }] },
  { ID: 3, Name: 'Gruppe', Status: 'group', Children: [{ ID: 4, Name: 'DB', Tags: [] }] },
]

let activeWrapper = null

function mountWizard(props, global = {}) {
  activeWrapper = mount(SubscribeWizard, {
    props: { components, slug: 'acme', ...props },
    global: { plugins: [i18n], ...global },
  })
  return activeWrapper
}

describe('SubscribeWizard', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
  })

  it('beschriftet den Dialog über die Überschrift des aktuellen Schritts', async () => {
    mountWizard()

    const dialog = body().find('dialog.wizard-modal')
    const labelId = dialog.attributes('aria-labelledby')
    expect(labelId).toBeTruthy()
    const title = document.getElementById(labelId)
    expect(title).not.toBeNull()
    expect(title.tagName).toBe('H2')
    expect(title.textContent).toContain(i18n.global.t('components.subscribeWizard.step1Title'))
  })

  // Die drei Schritte stecken in getrennten
  // <Transition>s; beim Wechsel stehen alte und neue Überschrift kurz
  // gleichzeitig im DOM. Mit echter Transition (kein Stub) darf deshalb keine
  // ID doppelt vorkommen, und aria-labelledby muss auf den NEUEN Titel zeigen.
  describe('aria-labelledby beim Schrittwechsel', () => {
    const t = (key) => i18n.global.t(key)
    const labelText = () => {
      const labelId = body().find('dialog.wizard-modal').attributes('aria-labelledby')
      return document.getElementById(labelId)?.textContent
    }
    const expectUniqueTitleIds = () => {
      const ids = [...document.querySelectorAll('[id^="wizard-title"]')].map((el) => el.id)
      expect(new Set(ids).size).toBe(ids.length)
    }

    it('zeigt nach dem Wechsel zu Schritt 2 und zurück zu Schritt 1 jeweils den neuen Titel', async () => {
      mountWizard({}, { stubs: { transition: false } })
      await body().findAll('.channel-card')[0].trigger('click')
      await body().find('#subscribe-email').setValue('a@b.de')

      await body().find('.btn-primary').trigger('click')
      expectUniqueTitleIds()
      expect(labelText()).toBe(t('components.subscribeWizard.step2Title'))

      await body().findAll('.wizard-body .btn-ghost').at(-1).trigger('click')
      expectUniqueTitleIds()
      expect(labelText()).toBe(t('components.subscribeWizard.step1Title'))
    })

    it('zeigt nach dem Abonnieren den Titel von Schritt 3', async () => {
      global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
      mountWizard({}, { stubs: { transition: false } })
      await body().findAll('.channel-card')[0].trigger('click')
      await body().find('#subscribe-email').setValue('a@b.de')
      await body().find('.btn-primary').trigger('click')
      await body().findAll('.comp-list-item input')[0].setValue(true)
      // Während der Leave-Animation steht noch Schritt 1 im DOM: den neuesten Button nehmen.
      await body().findAll('.wizard-body .btn-primary').at(-1).trigger('click')
      await vi.waitFor(() => expect(body().find('.wizard-success').exists()).toBe(true))

      expectUniqueTitleIds()
      expect(labelText()).toBe(t('components.subscribeWizard.successTitle'))
    })
  })

  it('startet bei Schritt 1 mit bedienbarem Weiter-Button', () => {
    mountWizard()

    expect(body().findAll('.channel-card')).toHaveLength(2)
    expect(body().find('.btn-primary').attributes('disabled')).toBeUndefined()
  })

  // eingeloggte Nutzer sollen ihre Konto-Adresse nicht
  // erneut eintippen müssen, dürfen sie aber weiterhin ändern (z.B. um eine
  // andere Adresse zu abonnieren).
  describe('E-Mail-Vorbelegung für eingeloggte Nutzer', () => {
    afterEach(() => {
      useAuth().logout()
    })

    it('füllt die E-Mail mit der Konto-Adresse vor, bleibt aber änderbar', async () => {
      useAuth().setAuth('jwt-1', { email: 'konto@beispiel.de' })
      mountWizard()

      await body().findAll('.channel-card')[0].trigger('click')
      expect(body().find('#subscribe-email').element.value).toBe('konto@beispiel.de')

      await body().find('#subscribe-email').setValue('andere@beispiel.de')
      expect(body().find('#subscribe-email').element.value).toBe('andere@beispiel.de')
    })

    it('lässt das E-Mail-Feld bei ausgeloggten Nutzern leer', async () => {
      mountWizard()

      await body().findAll('.channel-card')[0].trigger('click')
      expect(body().find('#subscribe-email').element.value).toBe('')
    })
  })

  // Vorher blieb „Weiter“ stumm deaktiviert; jetzt nennt der Assistent
  // den Fehler am Feld (role="alert", aria-invalid, aria-describedby) und setzt den Fokus.
  describe('Validierungshinweise in Schritt 1', () => {
    const t = (key) => i18n.global.t(key)
    const next = () => body().find('.btn-primary').trigger('click')
    const title = () => document.getElementById(body().find('dialog.wizard-modal').attributes('aria-labelledby')).textContent

    it('nennt ohne Kanal den Fehler und springt auf die erste Kanal-Karte', async () => {
      mountWizard()
      await next()

      const alert = body().find('#subscribe-channel-error')
      expect(alert.attributes('role')).toBe('alert')
      expect(alert.text()).toContain(t('components.subscribeWizard.errors.channelRequired'))
      expect(body().find('.channel-cards').attributes('aria-describedby')).toBe('subscribe-channel-error')
      expect(document.activeElement).toBe(body().findAll('.channel-card')[0].element)
      expect(title()).toBe(t('components.subscribeWizard.step1Title'))

      await body().findAll('.channel-card')[0].trigger('click')
      expect(body().find('#subscribe-channel-error').exists()).toBe(false)
    })

    it.each([
      ['', 'emailRequired'],
      ['not-an-email', 'emailInvalid'],
      ['a@b', 'emailInvalid'],
    ])('E-Mail %j: Fehler %s am Feld, Fokus im Feld, Schritt bleibt', async (value, key) => {
      mountWizard()
      await body().findAll('.channel-card')[0].trigger('click')
      await body().find('#subscribe-email').setValue(value)
      await next()

      const input = body().find('#subscribe-email')
      const alert = body().find('#subscribe-email-error')
      expect(alert.attributes('role')).toBe('alert')
      expect(alert.text()).toContain(t(`components.subscribeWizard.errors.${key}`))
      expect(input.attributes('aria-invalid')).toBe('true')
      expect(input.attributes('aria-describedby')).toBe('subscribe-email-error')
      expect(document.activeElement).toBe(input.element)
      expect(title()).toBe(t('components.subscribeWizard.step1Title'))
    })

    it('Fehler verschwindet beim Tippen, danach geht es mit gültiger E-Mail weiter', async () => {
      mountWizard()
      await body().findAll('.channel-card')[0].trigger('click')
      await next()
      expect(body().find('#subscribe-email-error').exists()).toBe(true)

      await body().find('#subscribe-email').setValue('a@b.de')
      expect(body().find('#subscribe-email-error').exists()).toBe(false)
      expect(body().find('#subscribe-email').attributes('aria-invalid')).toBeUndefined()
      await next()
      expect(body().find('.comp-list').exists()).toBe(true)
    })

    it('Enter im Feld löst dieselbe Prüfung aus', async () => {
      mountWizard()
      await body().findAll('.channel-card')[0].trigger('click')
      await body().find('#subscribe-email').trigger('keyup.enter')
      expect(body().find('#subscribe-email-error').exists()).toBe(true)
    })

    it.each([
      ['', 'webhookRequired'],
      ['not-a-url', 'webhookInvalid'],
      ['http://chat.googleapis.com/x', 'webhookInvalid'],
    ])('Webhook %j: Fehler %s am Feld, Hinweis entfällt', async (value, key) => {
      mountWizard()
      await body().findAll('.channel-card')[1].trigger('click')
      await body().find('#subscribe-webhook-url').setValue(value)
      expect(body().find('#subscribe-webhook-url-hint').exists()).toBe(true)
      await next()

      const input = body().find('#subscribe-webhook-url')
      expect(body().find('#subscribe-webhook-url-error').text()).toContain(t(`components.subscribeWizard.errors.${key}`))
      expect(input.attributes('aria-invalid')).toBe('true')
      expect(input.attributes('aria-describedby')).toBe('subscribe-webhook-url-error')
      expect(document.activeElement).toBe(input.element)
    })

    it('akzeptiert eine https-Webhook-URL und geht zu Schritt 2', async () => {
      mountWizard()
      await body().findAll('.channel-card')[1].trigger('click')
      await body().find('#subscribe-webhook-url').setValue('https://chat.googleapis.com/x')
      await next()
      expect(body().find('.comp-list').exists()).toBe(true)
    })

    it('ein Kanalwechsel räumt alte Fehler ab', async () => {
      mountWizard()
      await body().findAll('.channel-card')[0].trigger('click')
      await next()
      await body().findAll('.channel-card')[1].trigger('click')
      expect(body().find('[role="alert"]').exists()).toBe(false)
    })
  })

  // „0 / 8 ausgewählt“ las sich wie „nichts gewählt“, leer heißt aber „alle“.
  it('beschreibt die Auswahl verständlich: leer = alle Komponenten, sonst „n von m gewählt“', async () => {
    mountWizard()
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')

    const t = (key, p) => i18n.global.t(key, p)
    const count = () => body().find('.selected-count').text()
    expect(count()).toBe(t('components.subscribeWizard.selectedNone'))
    expect(count()).not.toMatch(/0\s*\/\s*3/)
    expect(body().find('.wizard-subtitle').text()).toBe(t('components.subscribeWizard.step2Subtitle'))

    await body().find('.comp-list-item input').setValue(true)
    expect(count()).toBe(t('components.subscribeWizard.selectedCount', { selected: 1, total: 3 }))
    expect(count()).toContain('1 von 3')
  })

  // .perm-checkbox hatte keine Stil-Regel (Kästchen ohne Abstand zum Text).
  it('stylt .perm-checkbox in style.css: Zeile mit Abstand, 44 px Zielhöhe, Fokus sichtbar', () => {
    const css = readFileSync(resolve(__dirname, '../style.css'), 'utf8')
    const rule = css.match(/\.perm-checkbox\s*\{([^}]*)\}/)?.[1] ?? ''
    expect(rule).toMatch(/display:\s*inline-flex/)
    expect(rule).toMatch(/gap:\s*var\(--/)
    expect(rule).toMatch(/min-height:\s*44px/)
    expect(css).toMatch(/\.perm-checkbox:focus-within\s*\{[^}]*outline:/)
  })

  it('flacht die Komponenten ab (inkl. verschachtelter Gruppen) und filtert nach Suche/Tag', async () => {
    mountWizard()
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')

    expect(body().findAll('.comp-list-item')).toHaveLength(3)

    await body().find('.comp-search-input').setValue('api')
    expect(body().findAll('.comp-list-item')).toHaveLength(1)
    expect(body().find('.comp-list-name').text()).toBe('API')
  })

  it('toggleAll wählt alle gefilterten Komponenten aus bzw. wieder ab', async () => {
    mountWizard()
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')

    const selectAllCheckbox = body().find('.select-all-row input[type="checkbox"]')
    await selectAllCheckbox.setValue(true)
    expect(body().findAll('.comp-list-item.checked')).toHaveLength(3)

    await selectAllCheckbox.setValue(false)
    expect(body().findAll('.comp-list-item.checked')).toHaveLength(0)
  })

  // Ohne Token verknüpft das Backend das neue Abo
  // nicht mit dem Konto, GET /api/me/subscription findet es dann nicht.
  describe.each([
    ['eingeloggt', 'tok123', 'tok123'],
    ['ausgeloggt', '', undefined],
  ])('X-Auth-Token-Header (%s)', (_name, tokenValue, expected) => {
    afterEach(() => { useAuth().token.value = '' })

    it('POST /api/subscribe enthält den Header nur mit Token', async () => {
      useAuth().token.value = tokenValue
      global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
      mountWizard()
      await body().findAll('.channel-card')[0].trigger('click')
      await body().find('#subscribe-email').setValue('a@b.de')
      await body().find('.btn-primary').trigger('click')
      await body().find('.wizard-body .btn-primary').trigger('click')
      await vi.waitFor(() => expect(global.fetch).toHaveBeenCalled())

      const headers = global.fetch.mock.calls[0][1].headers
      // Authorization bleibt frei für Caddys Basic Auth
      expect(headers['X-Auth-Token']).toBe(expected)
      expect(headers.Authorization).toBeUndefined()
      expect(headers['Content-Type']).toBe('application/json')
    })
  })

  it('sendet die Auswahl per POST /api/subscribe und geht bei Erfolg zu Schritt 3', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
    mountWizard()
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')

    await body().findAll('.comp-list-item input')[0].setValue(true)
    await body().find('.wizard-body .btn-primary').trigger('click')
    await vi.waitFor(() => expect(body().find('.wizard-success').exists()).toBe(true))

    expect(global.fetch).toHaveBeenCalledWith('/api/subscribe', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({
        channel: 'email',
        email: 'a@b.de',
        webhook_url: '',
        component_ids: '1',
        slug: 'acme',
        language: 'de',
      }),
    }))
    expect(activeWrapper.emitted('subscribed')).toBeTruthy()
  })

  // Die Sprache des Besuchers wandert als Mailsprache mit dem Abo.
  it('sendet die gewählte Besuchersprache als language', async () => {
    i18n.global.locale.value = 'en'
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
    mountWizard()
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')
    await body().find('.wizard-body .btn-primary').trigger('click')
    await vi.waitFor(() => expect(global.fetch).toHaveBeenCalled())
    expect(JSON.parse(global.fetch.mock.calls[0][1].body).language).toBe('en')
  })

  // Ein unbestätigtes Abo (Double-Opt-in) zeigt statt "Erfolgreich
  // abonniert" den Hinweis auf die Bestätigungsmail.
  it('zeigt bei status=pending den Hinweis auf die Bestätigungsmail', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ status: 'pending' }) })
    mountWizard()
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')
    await body().find('.wizard-body .btn-primary').trigger('click')
    await vi.waitFor(() => expect(body().find('.wizard-success').exists()).toBe(true))

    const success = body().find('.wizard-success')
    expect(success.find('h2').text()).toBe('Bitte bestätige deine E-Mail-Adresse')
    // Neutral formuliert: Für bereits abonnierte Adressen antwortet der Server genauso.
    expect(success.text()).toContain('Wenn für a@b.de noch kein Abonnement besteht, haben wir einen Bestätigungslink dorthin geschickt.')
    expect(success.text()).toContain('Besteht das Abonnement bereits, ändert sich nichts.')
    expect(success.text()).not.toContain('Erfolgreich abonniert!')
    expect(activeWrapper.emitted('subscribed')).toBeTruthy()
  })

  it('zeigt ohne pending-Status weiter "Erfolgreich abonniert!"', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ status: 'active' }) })
    mountWizard()
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')
    await body().find('.wizard-body .btn-primary').trigger('click')
    await vi.waitFor(() => expect(body().find('.wizard-success').exists()).toBe(true))

    expect(body().find('.wizard-success h2').text()).toBe('Erfolgreich abonniert!')
    expect(body().find('.wizard-success').text()).not.toContain('Bestätigungslink')
  })

  it('zeigt eine Fehlermeldung, wenn das Abonnieren fehlschlägt, und bleibt auf Schritt 2', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false, status: 500, json: async () => ({ error: 'Serverfehler' }),
    })
    mountWizard()
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')

    await body().find('.wizard-body .btn-primary').trigger('click')
    await vi.waitFor(() => expect(body().find('.wizard-alert').exists()).toBe(true))

    expect(body().find('.wizard-alert').text()).toContain('Serverfehler')
    expect(body().find('.wizard-success').exists()).toBe(false)
  })

  // Kaputter Body (z. B. HTML-Fehlerseite vom Proxy) zeigte den
  // rohen JSON-Parserfehler ("Unexpected token '<' ...").
  it('zeigt bei unlesbarem Fehler-Body eine verständliche Meldung statt des Parserfehlers', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false, status: 502, json: async () => { throw new SyntaxError("Unexpected token '<'") },
    })
    mountWizard()
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')
    await body().find('.wizard-body .btn-primary').trigger('click')
    await vi.waitFor(() => expect(body().find('.wizard-alert').exists()).toBe(true))

    expect(body().find('.wizard-alert').text()).toBe(i18n.global.t('api.errors.server_error'))
    expect(body().find('.wizard-alert').text()).not.toContain('Unexpected')
  })

  // Netzwerkfehler und unlesbarer Erfolgs-Body zeigten englische Browser-/Parsertexte.
  it.each([
    ['Netzwerkfehler', () => vi.fn().mockRejectedValue(new TypeError('Failed to fetch')), 'api.networkError', 'Failed to fetch'],
    ['unlesbarer Erfolgs-Body', () => vi.fn().mockResolvedValue({
      ok: true, status: 200, json: async () => { throw new SyntaxError("Unexpected token '<'") },
    }), 'api.errors.server_error', 'Unexpected'],
  ])('zeigt bei %s eine übersetzte Meldung', async (_name, makeFetch, key, rawText) => {
    global.fetch = makeFetch()
    mountWizard()
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')
    await body().find('.wizard-body .btn-primary').trigger('click')
    await vi.waitFor(() => expect(body().find('.wizard-alert').exists()).toBe(true))

    expect(body().find('.wizard-alert').text()).toBe(i18n.global.t(key))
    expect(body().find('.wizard-alert').text()).not.toContain(rawText)
  })

  // Der Serverfehler in Schritt 2 war eine freie Zeile ohne role=alert
  // und Fokus und zeigte den rohen Servertext statt der Übersetzung des Fehlercodes.
  describe('Serverfehler beim Abonnieren', () => {
    const subscribeWithResponse = async (res) => {
      global.fetch = vi.fn().mockResolvedValue(res)
      mountWizard()
      await body().findAll('.channel-card')[0].trigger('click')
      await body().find('#subscribe-email').setValue('a@b.de')
      await body().find('.btn-primary').trigger('click')
      await body().find('.wizard-body .btn-primary').trigger('click')
      await vi.waitFor(() => expect(body().find('.wizard-alert').exists()).toBe(true))
    }

    it('übersetzt den Fehlercode über api.errors statt den Servertext zu zeigen', async () => {
      await subscribeWithResponse({
        ok: false, status: 409, json: async () => ({ error: 'Diese E-Mail ist bereits abonniert (roh)', code: 'subscription_exists_email' }),
      })
      const alert = body().find('.wizard-alert')
      expect(alert.text()).toBe(i18n.global.t('api.errors.subscription_exists_email'))
      expect(alert.text()).not.toContain('roh')
    })

    it('zeigt den Servertext, wenn der Code unbekannt ist', async () => {
      await subscribeWithResponse({
        ok: false, status: 400, json: async () => ({ error: 'Klartext vom Server', code: 'gibt_es_nicht' }),
      })
      expect(body().find('.wizard-alert').text()).toBe('Klartext vom Server')
    })

    it('ist ein Alert (role=alert) und erhält den Fokus', async () => {
      await subscribeWithResponse({
        ok: false, status: 500, json: async () => ({ error: 'x', code: 'server_error' }),
      })
      const alert = body().find('.wizard-alert')
      expect(alert.attributes('role')).toBe('alert')
      await vi.waitFor(() => expect(document.activeElement).toBe(alert.element))
    })

    it('hat keine Barrierefreiheitsverstöße im Fehlerzustand', async () => {
      await subscribeWithResponse({
        ok: false, status: 500, json: async () => ({ error: 'x', code: 'server_error' }),
      })
      expect(await axe(document.body)).toHaveNoViolations()
    })
  })

  it('emittiert close beim Klick auf den Schließen-Button', async () => {
    mountWizard()

    await body().find('.wizard-close').trigger('click')

    expect(activeWrapper.emitted('close')).toBeTruthy()
  })

  // 4a: Leerfall der Komponentensuche über EmptyState statt Inline-Styles.
  it('zeigt bei einer Suche ohne Treffer den Leerzustand und keine Inline-Styles', async () => {
    mountWizard({}, { stubs: { transition: false } })
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')
    expect(body().find('.empty-state').exists()).toBe(false)

    await body().find('.comp-search-input').setValue('gibt-es-nicht')

    const empty = body().find('.empty-state')
    expect(empty.exists()).toBe(true)
    expect(empty.text()).toContain(i18n.global.t('components.subscribeWizard.noMatches'))
    expect(document.body.querySelector('dialog.wizard-modal [style]')).toBeNull()
  })

  it('hat keine axe-Funde in Schritt 1 und 2', async () => {
    mountWizard({}, { stubs: { transition: false } })
    const dialog = () => document.body.querySelector('dialog.wizard-modal')
    expect(await axe(dialog())).toHaveNoViolations()

    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    expect(await axe(dialog())).toHaveNoViolations()

    await body().find('.btn-primary').trigger('click')
    expect(await axe(dialog())).toHaveNoViolations()
  })

  it('hat keine axe-Funde mit Validierungsfehlern in Schritt 1', async () => {
    mountWizard({}, { stubs: { transition: false } })
    const dialog = () => document.body.querySelector('dialog.wizard-modal')
    await body().find('.btn-primary').trigger('click')
    expect(await axe(dialog())).toHaveNoViolations()

    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('.btn-primary').trigger('click')
    expect(await axe(dialog())).toHaveNoViolations()
  })
  // 4a: Zweige der geänderten Zeilen.
  it('zeigt Komponenten ohne Tags ohne Fehler und schließt mit „Fertig“', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
    mountWizard({ components: [{ ID: 1, Name: 'Ohne Tags' }] })
    await body().findAll('.channel-card')[0].trigger('click')
    await body().find('#subscribe-email').setValue('a@b.de')
    await body().find('.btn-primary').trigger('click')
    expect(body().find('.comp-list-item .wizard-comp-tag').text()).toBe('')

    await body().find('.wizard-body .btn-primary').trigger('click')
    await vi.waitFor(() => expect(body().find('.wizard-success').exists()).toBe(true))
    await body().find('.wizard-done-btn').trigger('click')
    expect(activeWrapper.emitted('close')).toBeTruthy()
  })
})
