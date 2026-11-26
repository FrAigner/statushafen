import { describe, it, expect, beforeEach, afterEach, vi, type Mock } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../i18n'
import SubscriptionManager from './SubscriptionManager.vue'
import { axe } from '../test-support/axe'
import { usePublicPageLanguages } from '../composables/usePublicPageLanguages'
import { asResponse, mockedFetch, type FetchFn } from '../test-support/fetchStub'

function jsonResponse(body: unknown, status = 200): Response {
  return asResponse({ ok: status < 400, status, json: async () => body })
}

function mountManager(props?: Record<string, unknown>) {
  return mount(SubscriptionManager, {
    props: {
      isLoggedIn: true,
      currentUser: { email: 'a@b.de' },
      components: [],
      slug: 'acme',
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

describe('SubscriptionManager', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('rendert nichts, wenn der Besucher nicht eingeloggt ist', () => {
    const wrapper = mountManager({ isLoggedIn: false })

    expect(wrapper.find('.edit-abo-section').exists()).toBe(false)
  })

  it('lädt beim Aufklappen über die Überschrift automatisch, ohne "Mein Abo laden"', async () => {
    global.fetch = vi.fn().mockResolvedValue(
      jsonResponse({ channel: 'email', component_ids: '1,2' })
    )
    const wrapper = mountManager({
      components: [{ ID: 1, Name: 'API' }, { ID: 2, Name: 'Web' }],
    })
    await flushPromises()
    mockedFetch().mockClear()

    expect(wrapper.find('.edit-abo-body').exists()).toBe(false)
    await wrapper.find('.edit-abo-header').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))

    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/me/subscription?slug=acme'),
      expect.anything()
    )
    expect(wrapper.findAll('.perm-checkbox')).toHaveLength(2)
    expect(wrapper.find('.btn-abo-lookup').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Mein Abo laden')
  })

  it('klappt beim zweiten Klick auf die Überschrift wieder zu, ohne neu zu laden', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))
    const wrapper = mountManager()
    await flushPromises()
    await wrapper.find('.edit-abo-header').trigger('click')
    await flushPromises()
    mockedFetch().mockClear()

    await wrapper.find('.edit-abo-header').trigger('click')
    await flushPromises()

    expect(wrapper.find('.edit-abo-body').exists()).toBe(false)
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('lädt beim Öffnen per openEditAbo (Knopf/Menü)', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))
    const wrapper = mountManager()
    await flushPromises()

    await wrapper.vm.openEditAbo()

    expect(wrapper.find('.edit-abo-body').exists()).toBe(true)
    expect(wrapper.find('.edit-abo-result').exists()).toBe(true)
  })

  it('ignoriert openEditAbo, wenn der Besucher nicht eingeloggt ist', async () => {
    global.fetch = vi.fn()
    const wrapper = mountManager({ isLoggedIn: false })

    await wrapper.vm.openEditAbo()

    expect(global.fetch).not.toHaveBeenCalled()
    expect(wrapper.find('.edit-abo-body').exists()).toBe(false)
  })

  it('lädt nicht, solange der Besucher nicht eingeloggt ist', async () => {
    global.fetch = vi.fn()
    mountManager({ isLoggedIn: false })
    await flushPromises()

    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('prüft beim Einloggen still, ob ein Abonnement existiert, und meldet es', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))
    const wrapper = mountManager({ isLoggedIn: false })
    await wrapper.setProps({ isLoggedIn: true })
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('subscription-state')).toEqual([[true]])
    expect(wrapper.find('.edit-abo-body').exists()).toBe(false)
  })

  it('404 ist der Normalfall "kein Abonnement": neutral, keine rote Meldung, Zustand false', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'Kein Abonnement gefunden' }, 404))
    const wrapper = mountManager()
    await flushPromises()

    expect(wrapper.emitted('subscription-state')).toEqual([[false]])
    expect(wrapper.find('.edit-abo-section').exists()).toBe(false)
    expect(wrapper.find('.edit-abo-error').exists()).toBe(false)
    expect(wrapper.find('.edit-abo-msg').exists()).toBe(false)

    await wrapper.vm.openEditAbo()
    expect(wrapper.find('.edit-abo-none').text()).toContain('noch kein Abonnement')
    expect(wrapper.find('.edit-abo-error').exists()).toBe(false)
    expect(wrapper.find('.edit-abo-result').exists()).toBe(false)
  })

  // The expected 404 must not end up as an error in the console.
  it('loggt das erwartete 404 nicht per console.error, ein 500 aber schon', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'Kein Abonnement gefunden' }, 404))
    mountManager()
    await flushPromises()
    expect(console.error).not.toHaveBeenCalled()

    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'kaputt' }, 500))
    mountManager()
    await flushPromises()
    expect(console.error).toHaveBeenCalled()
  })

  it('500 zeigt eine echte Fehlermeldung mit "Erneut versuchen", nicht "Kein Abonnement"', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'boom' }, 500))
    const wrapper = mountManager()
    await flushPromises()
    // the silent pre-check reports nothing about the subscription state on a real error
    expect(wrapper.emitted('subscription-state')).toBeUndefined()

    await wrapper.find('.edit-abo-header').trigger('click')
    await flushPromises()

    const err = wrapper.find('.edit-abo-error')
    expect(err.text()).toContain('Abonnement konnte nicht geladen werden')
    expect(err.text()).not.toContain('Kein Abonnement')
    expect(wrapper.find('.btn-retry-abo').text()).toContain('Erneut versuchen')
    expect(wrapper.emitted('subscription-state')).toBeUndefined()
  })

  // First 404 (no subscription), then a real error -
  // must no longer appear as "no subscription".
  it.each([
    ['500', () => jsonResponse({ error: 'boom' }, 500)],
    ['Netzwerkfehler', () => Promise.reject(new TypeError('Failed to fetch'))],
  ])('Fehler (%s) nach einem 404 zeigt die Fehlermeldung statt "kein Abonnement"', async (_n, failure) => {
    global.fetch = vi.fn().mockResolvedValueOnce(jsonResponse({ error: 'none' }, 404))
    const wrapper = mountManager()
    await flushPromises()
    expect(wrapper.emitted('subscription-state')).toEqual([[false]])

    mockedFetch().mockImplementation(failure as unknown as FetchFn)
    await wrapper.vm.refresh()
    await wrapper.find('.edit-abo-header').trigger('click')
    await flushPromises()

    expect(wrapper.find('.edit-abo-none').exists()).toBe(false)
    expect(wrapper.find('.edit-abo-error').exists()).toBe(true)
    expect(wrapper.find('.btn-retry-abo').exists()).toBe(true)
  })

  it('Fehler nach einem 404 meldet den Zustand als unbekannt (null)', async () => {
    global.fetch = vi.fn().mockResolvedValueOnce(jsonResponse({ error: 'none' }, 404))
    const wrapper = mountManager()
    await flushPromises()
    mockedFetch().mockResolvedValue(jsonResponse({ error: 'boom' }, 500))
    await wrapper.vm.refresh()
    expect(wrapper.emitted('subscription-state')).toEqual([[false], [null]])
  })

  it('Netzwerkfehler zeigt ebenfalls die Fehlermeldung mit "Erneut versuchen"', async () => {
    global.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'))
    const wrapper = mountManager()
    await flushPromises()

    await wrapper.find('.edit-abo-header').trigger('click')
    await flushPromises()

    expect(wrapper.find('.edit-abo-error').exists()).toBe(true)
    expect(wrapper.find('.edit-abo-none').exists()).toBe(false)
    expect(wrapper.find('.btn-retry-abo').exists()).toBe(true)
  })

  it('"Erneut versuchen" lädt neu und zeigt danach das Abonnement', async () => {
    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: 'boom' }, 500))
      .mockResolvedValueOnce(jsonResponse({ error: 'boom' }, 500))
      .mockResolvedValueOnce(jsonResponse({ channel: 'email', component_ids: 'all' }))
    const wrapper = mountManager()
    await flushPromises()
    await wrapper.find('.edit-abo-header').trigger('click')
    await flushPromises()
    expect(wrapper.find('.edit-abo-error').exists()).toBe(true)

    await wrapper.find('.btn-retry-abo').trigger('click')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(3)
    expect(wrapper.find('.edit-abo-error').exists()).toBe(false)
    expect(wrapper.find('.edit-abo-result').exists()).toBe(true)
  })

  it('emittiert open-wizard beim Klick auf "Kanal ändern"', async () => {
    global.fetch = vi.fn().mockResolvedValue(
      jsonResponse({ channel: 'email', component_ids: 'all' })
    )
    const wrapper = mountManager()
    await wrapper.find('.edit-abo-header').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))

    await wrapper.find('.btn-ghost-sm').trigger('click')

    expect(wrapper.emitted('open-wizard')).toBeTruthy()
  })

  it('speichert per PUT; die grüne Bestätigung bleibt nach dem Zuklappen sichtbar', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))
    const wrapper = mountManager()
    await wrapper.find('.edit-abo-header').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
    mockedFetch().mockResolvedValue(jsonResponse({}))

    await wrapper.find('.btn-save-abo').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.edit-abo-body').exists()).toBe(false))

    expect(global.fetch).toHaveBeenLastCalledWith(
      expect.stringContaining('/api/me/subscription?slug=acme'),
      expect.objectContaining({ method: 'PUT' })
    )
    const msg = wrapper.find('.edit-abo-msg')
    expect(msg.text()).toContain('Gespeichert')
    expect(msg.attributes('role')).toBe('status')
    expect(msg.classes()).toContain('edit-abo-msg--ok')
  })

  it('blendet die Bestätigung beim erneuten Aufklappen aus', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))
    const wrapper = mountManager()
    await wrapper.find('.edit-abo-header').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
    await wrapper.find('.btn-save-abo').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.edit-abo-msg').exists()).toBe(true))

    await wrapper.find('.edit-abo-header').trigger('click')
    await flushPromises()

    expect(wrapper.find('.edit-abo-msg').exists()).toBe(false)
  })

  it('zeigt eine rote Fehlermeldung, wenn das Speichern fehlschlägt', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))
    const wrapper = mountManager()
    await wrapper.find('.edit-abo-header').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
    mockedFetch().mockResolvedValue(jsonResponse({ error: 'boom' }, 500))

    await wrapper.find('.btn-save-abo').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.edit-abo-msg').text()).toContain('Fehler beim Speichern'))

    expect(wrapper.find('.edit-abo-msg').classes()).not.toContain('edit-abo-msg--ok')
    expect(wrapper.find('.edit-abo-body').exists()).toBe(true)
  })

  // Accessibility - the heading is a real <button> element (native keyboard
  // operation via Enter/Space instead of custom keydown logic on a <div>),
  // reports its state via aria-expanded and moves the focus into the section
  // when opening.
  describe('Barrierefreiheit', () => {
    it('die Überschrift ist ein <button> mit aria-expanded', async () => {
      global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))
      const wrapper = mountManager()
      await flushPromises()

      const header = wrapper.find('.edit-abo-header')
      expect(header.element.tagName).toBe('BUTTON')
      expect(header.attributes('aria-expanded')).toBe('false')

      await header.trigger('click')
      await flushPromises()

      expect(header.attributes('aria-expanded')).toBe('true')
    })

    it('fokussiert den aufgeklappten Bereich (Tastaturbedienung)', async () => {
      global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))
      const wrapper = mountManager()
      await flushPromises()
      const focusSpy = vi.spyOn(HTMLElement.prototype, 'focus')

      await wrapper.find('.edit-abo-header').trigger('click')
      await flushPromises()

      expect(focusSpy).toHaveBeenCalled()
    })
  })

  // Header menu "Manage subscription": on the first click the section may not be rendered yet
  // (no subscription yet), so it has to open first and then scroll and move focus into it.
  describe('revealEditAbo (header menu)', () => {
    let scrollSpy: Mock<() => void>
    beforeEach(() => {
      scrollSpy = vi.fn()
      Element.prototype.scrollIntoView = scrollSpy
    })

    it('opens the section, scrolls it into view and focuses it on the first call, even without a subscription', async () => {
      global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'Kein Abonnement gefunden' }, 404))
      const wrapper = mountManager()
      await flushPromises()
      expect(wrapper.find('.edit-abo-section').exists()).toBe(false)
      const focusSpy = vi.spyOn(HTMLElement.prototype, 'focus')

      await wrapper.vm.revealEditAbo()
      await flushPromises()

      const section = wrapper.find('.edit-abo-section')
      expect(section.exists()).toBe(true)
      expect(wrapper.find('.edit-abo-header').attributes('aria-expanded')).toBe('true')
      expect(scrollSpy).toHaveBeenCalledTimes(1)
      expect(scrollSpy.mock.contexts[0]).toBe(section.element)
      expect(focusSpy.mock.contexts).toContain(wrapper.find('#edit-abo-body-panel').element)
      focusSpy.mockRestore()
    })

    it('does nothing when logged out', async () => {
      global.fetch = vi.fn()
      const wrapper = mountManager({ isLoggedIn: false })
      await wrapper.vm.revealEditAbo()
      await flushPromises()
      expect(scrollSpy).not.toHaveBeenCalled()
      expect(global.fetch).not.toHaveBeenCalled()
    })
  })

  // "End subscription" - the confirmation runs as a separate state on the
  // page (no window.confirm), deletes via DELETE and returns to state B
  // ("no subscription").
  describe('Abonnement beenden', () => {
    function mountWithSubscription() {
      global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))
      return mountManager()
    }

    it('zeigt statt window.confirm eine Bestätigung auf der Seite', async () => {
      const confirmSpy = vi.spyOn(window, 'confirm')
      const wrapper = mountWithSubscription()
      await wrapper.find('.edit-abo-header').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))

      expect(wrapper.find('.edit-abo-end-confirm').exists()).toBe(false)
      await wrapper.find('.btn-end-abo').trigger('click')

      expect(wrapper.find('.edit-abo-end-confirm').exists()).toBe(true)
      expect(confirmSpy).not.toHaveBeenCalled()
    })

    it('"Abbrechen" verwirft die Bestätigung, ohne zu löschen', async () => {
      const wrapper = mountWithSubscription()
      await wrapper.find('.edit-abo-header').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
      await wrapper.find('.btn-end-abo').trigger('click')
      mockedFetch().mockClear()

      await wrapper.find('.edit-abo-end-confirm .btn-ghost-sm').trigger('click')

      expect(wrapper.find('.edit-abo-end-confirm').exists()).toBe(false)
      expect(wrapper.find('.edit-abo-result').exists()).toBe(true)
      expect(global.fetch).not.toHaveBeenCalled()
    })

    it('löscht per DELETE und führt zurück in Zustand B ("kein Abonnement")', async () => {
      const wrapper = mountWithSubscription()
      await wrapper.find('.edit-abo-header').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
      await wrapper.find('.btn-end-abo').trigger('click')
      mockedFetch().mockResolvedValue(jsonResponse({ message: 'Abonnement beendet' }))

      await wrapper.find('.btn-end-confirm').trigger('click')
      await flushPromises()

      expect(global.fetch).toHaveBeenLastCalledWith(
        expect.stringContaining('/api/me/subscription?slug=acme'),
        expect.objectContaining({ method: 'DELETE' })
      )
      expect(wrapper.find('.edit-abo-result').exists()).toBe(false)
      expect(wrapper.find('.edit-abo-none').exists()).toBe(true)
      expect(wrapper.emitted('subscription-state')!.at(-1)).toEqual([false])
    })

    // The "Yes, end" button disappears from the DOM together with the whole
    // section on a successful end - endLoading must still be reset, otherwise
    // the button stays disabled for good after subscribing again ("disabled"
    // hangs on the same flag).
    it('setzt endLoading nach erfolgreichem Beenden zurück, damit ein erneutes Beenden nicht dauerhaft deaktiviert bleibt', async () => {
      const wrapper = mountWithSubscription()
      await wrapper.find('.edit-abo-header').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
      await wrapper.find('.btn-end-abo').trigger('click')
      mockedFetch().mockResolvedValue(jsonResponse({ message: 'Abonnement beendet' }))
      await wrapper.find('.btn-end-confirm').trigger('click')
      await flushPromises()
      expect(wrapper.find('.edit-abo-none').exists()).toBe(true)

      // Subscribed again (e.g. via the wizard) and the section reloaded.
      mockedFetch().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))
      await wrapper.vm.refresh()
      await wrapper.find('.btn-end-abo').trigger('click')

      expect(wrapper.find('.btn-end-confirm').attributes('disabled')).toBeUndefined()
    })

    it('zeigt eine Fehlermeldung, wenn das Beenden fehlschlägt, und kehrt zum Beenden-Knopf zurück', async () => {
      const wrapper = mountWithSubscription()
      await wrapper.find('.edit-abo-header').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
      await wrapper.find('.btn-end-abo').trigger('click')
      mockedFetch().mockResolvedValue(jsonResponse({ error: 'boom' }, 500))

      await wrapper.find('.btn-end-confirm').trigger('click')
      await flushPromises()

      expect(wrapper.find('.edit-abo-msg').text()).toContain('nicht beendet')
      expect(wrapper.find('.edit-abo-msg').classes()).not.toContain('edit-abo-msg--ok')
      expect(wrapper.find('.edit-abo-result').exists()).toBe(true)
      // After the failure the button again instead of the (answered) confirmation,
      // and operable again (no longer disabled by the request).
      expect(wrapper.find('.edit-abo-end-confirm').exists()).toBe(false)
      expect(wrapper.find('.btn-end-abo').exists()).toBe(true)
    })

    it('deaktiviert den Beenden-Knopf, während die Anfrage läuft', async () => {
      const wrapper = mountWithSubscription()
      await wrapper.find('.edit-abo-header').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
      await wrapper.find('.btn-end-abo').trigger('click')

      let resolveDelete!: (value: Response) => void
      mockedFetch().mockImplementation(() => new Promise<Response>((resolve) => { resolveDelete = resolve }))
      const clickPromise = wrapper.find('.btn-end-confirm').trigger('click')
      await flushPromises()

      expect(wrapper.find('.btn-end-confirm').attributes('disabled')).toBeDefined()
      expect(wrapper.find('.btn-end-confirm').attributes('aria-busy')).toBe('true')

      resolveDelete(jsonResponse({ error: 'boom' }, 500))
      await clickPromise
      await flushPromises()

      // After the answer (here: an error) the request is over - the end
      // button (not disabled) instead of the (answered) confirmation.
      expect(wrapper.find('.btn-end-abo').exists()).toBe(true)
      expect(wrapper.find('.btn-end-abo').attributes('disabled')).toBeUndefined()
    })
  })

  it('refresh() lädt neu und meldet den neuen Zustand (z. B. nach dem Abonnieren)', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ error: 'x' }, 404))
    const wrapper = mountManager()
    await flushPromises()
    mockedFetch().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: 'all' }))

    await wrapper.vm.refresh()

    expect(wrapper.emitted('subscription-state')).toEqual([[false], [true]])
    expect(wrapper.find('.edit-abo-section').exists()).toBe(true)
  })

  it('hat keine axe-Funde im aufgeklappten Zustand', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: '1' }))
    const wrapper = mountManager({ components: [{ ID: 1, Name: 'API' }, { ID: 2, Name: 'Web' }] })
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()

    await wrapper.find('.edit-abo-header').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
    expect(await axe(wrapper)).toHaveNoViolations()
    wrapper.unmount()
  })
  // 4a: a Google Chat subscription shows the webhook URL (class instead of inline style).
  it('zeigt bei einem Google-Chat-Abo die Webhook-URL', async () => {
    global.fetch = vi.fn().mockResolvedValue(
      jsonResponse({ channel: 'google_chat', webhook_url: 'https://chat.example/hook', component_ids: 'all' })
    )
    const wrapper = mountManager({ components: [{ ID: 1, Name: 'API' }] })
    await flushPromises()
    await wrapper.find('.edit-abo-header').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))

    expect(wrapper.find('.edit-abo-webhook').text()).toContain('https://chat.example/hook')
    expect(wrapper.html()).not.toContain('style=')
  })
  // Change the mail language in the subscription manager.
  describe('Mailsprache', () => {
    const pageLangs = [
      { code: 'de', name: 'Deutsch', is_default: true },
      { code: 'en', name: 'English', is_default: false },
    ]

    afterEach(() => usePublicPageLanguages().clearPageLanguages())

    async function openManager(subscription: Record<string, unknown>) {
      global.fetch = vi.fn().mockResolvedValue(jsonResponse(subscription))
      const wrapper = mountManager()
      await wrapper.find('.edit-abo-header').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
      return wrapper
    }

    it('zeigt die Sprachwahl mit Seitenstandard zuerst und der gespeicherten Sprache', async () => {
      usePublicPageLanguages().setPageLanguages(pageLangs)
      const wrapper = await openManager({ channel: 'email', component_ids: 'all', language: 'en' })

      const select = wrapper.find<HTMLSelectElement>('#edit-abo-language-select')
      expect(select.exists()).toBe(true)
      expect(wrapper.find('label[for="edit-abo-language-select"]').text()).toBe('Sprache der Benachrichtigungen')
      expect(select.findAll('option').map(o => o.text())).toEqual(['Seitenstandard (Deutsch)', 'English'])
      expect(select.element.value).toBe('en')
    })

    it('sendet die gewählte Sprache beim Speichern', async () => {
      usePublicPageLanguages().setPageLanguages(pageLangs)
      const wrapper = await openManager({ channel: 'email', component_ids: 'all', language: '' })
      expect(wrapper.find<HTMLSelectElement>('#edit-abo-language-select').element.value).toBe('')

      await wrapper.find('#edit-abo-language-select').setValue('en')
      mockedFetch().mockResolvedValue(jsonResponse({}))
      await wrapper.find('.btn-save-abo').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-body').exists()).toBe(false))

      const [, options] = mockedFetch().mock.calls.at(-1)!
      expect(options!.method).toBe('PUT')
      expect(JSON.parse(options!.body as string)).toEqual({ component_ids: 'all', language: 'en' })
    })

    it('bietet im Katalog deaktivierte Sprachen nicht an (is_active aus dem Dashboard)', async () => {
      usePublicPageLanguages().setPageLanguages([
        ...pageLangs.map(l => ({ ...l, is_active: true })),
        { code: 'hr', name: 'Hrvatski', is_default: false, is_active: false },
      ])
      const wrapper = await openManager({ channel: 'email', component_ids: 'all', language: 'hr' })

      const select = wrapper.find<HTMLSelectElement>('#edit-abo-language-select')
      expect(select.findAll('option').map(o => o.text())).toEqual(['Seitenstandard (Deutsch)', 'English'])
      expect(select.element.value).toBe('')
    })

    it('sendet Komponenten-IDs und Sprache gemeinsam', async () => {
      usePublicPageLanguages().setPageLanguages(pageLangs)
      global.fetch = vi.fn().mockResolvedValue(jsonResponse({ channel: 'email', component_ids: '1', language: 'en' }))
      const wrapper = mountManager({ components: [{ ID: 1, Name: 'API' }, { ID: 2, Name: 'Web' }] })
      await wrapper.find('.edit-abo-header').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-result').exists()).toBe(true))
      await wrapper.findAll('.perm-checkbox input')[1].setValue(true)

      mockedFetch().mockResolvedValue(jsonResponse({}))
      await wrapper.find('.btn-save-abo').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-body').exists()).toBe(false))
      expect(JSON.parse(mockedFetch().mock.calls.at(-1)![1]!.body as string)).toEqual({ component_ids: '1,2', language: 'en' })
    })

    it('zeigt eine nicht mehr angebotene Sprache als Seitenstandard und speichert sie als leer', async () => {
      usePublicPageLanguages().setPageLanguages(pageLangs)
      const wrapper = await openManager({ channel: 'email', component_ids: 'all', language: 'fr' })
      expect(wrapper.find<HTMLSelectElement>('#edit-abo-language-select').element.value).toBe('')

      mockedFetch().mockResolvedValue(jsonResponse({}))
      await wrapper.find('.btn-save-abo').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-body').exists()).toBe(false))
      expect(JSON.parse(mockedFetch().mock.calls.at(-1)![1]!.body as string).language).toBe('')
    })

    it('blendet die Wahl bei nur einer Sprache aus und sendet dann kein language-Feld', async () => {
      usePublicPageLanguages().setPageLanguages([pageLangs[0]])
      const wrapper = await openManager({ channel: 'email', component_ids: 'all', language: 'en' })
      expect(wrapper.find('#edit-abo-language-select').exists()).toBe(false)

      mockedFetch().mockResolvedValue(jsonResponse({}))
      await wrapper.find('.btn-save-abo').trigger('click')
      await vi.waitFor(() => expect(wrapper.find('.edit-abo-body').exists()).toBe(false))
      expect(JSON.parse(mockedFetch().mock.calls.at(-1)![1]!.body as string)).toEqual({ component_ids: 'all' })
    })

    it('hat keine axe-Funde mit Sprachwahl', async () => {
      usePublicPageLanguages().setPageLanguages(pageLangs)
      const wrapper = await openManager({ channel: 'email', component_ids: 'all', language: '' })
      expect(await axe(wrapper.element)).toEqual([])
    })
  })
})
