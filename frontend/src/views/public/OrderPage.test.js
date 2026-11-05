import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import OrderPage from './OrderPage.vue'
import { axe } from '../../test-support/axe.js'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// OrderPage lädt beim Mounten das Flag /api/public/orders/enabled.
// Der Standard-Mock liefert dafür enabled:true und für POST die Erfolgsantwort;
// einzelne Tests überschreiben die POST-Antwort bzw. das Flag gezielt.
function routedFetch({ enabled = true, postResponse = jsonResponse({ message: 'ok' }, 201) } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && String(url).includes('/api/public/orders/enabled')) {
      return jsonResponse({ enabled })
    }
    return postResponse
  })
}

function mountOrderPage(fetchImpl) {
  global.fetch = fetchImpl || routedFetch()
  return mount(OrderPage, {
    global: { plugins: [i18n] },
  })
}

describe('OrderPage', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    // Tests assertieren auf deutschen Wortlaut; ohne gespeicherte Präferenz
    // fiele detectInitialLocale() in happy-dom (navigator.language en-US) auf 'en'.
    i18n.global.locale.value = 'de'
  })

  it('nennt im Slug-Hinweis die konfigurierte Basisdomain', async () => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', 'example.com')
    const wrapper = mountOrderPage()
    await flushPromises()
    expect(wrapper.find('#order-slug').element.parentElement.querySelector('.entry-hint').textContent).toContain('deine-wahl.example.com')
    vi.unstubAllEnvs()
  })

  it('nennt im Slug-Hinweis ohne Basisdomain den Pfad /status/<Kennung>', async () => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', '')
    const wrapper = mountOrderPage()
    await flushPromises()
    expect(wrapper.find('#order-slug').element.parentElement.querySelector('.entry-hint').textContent).toContain('/status/deine-wahl')
    vi.unstubAllEnvs()
  })

  it('renders the order form fields', async () => {
    const wrapper = mountOrderPage()
    await flushPromises()
    expect(wrapper.find('#order-company').exists()).toBe(true)
    expect(wrapper.find('#order-email').exists()).toBe(true)
    expect(wrapper.find('#order-slug').exists()).toBe(true)
  })

  it('keeps the honeypot field out of sight for real users', async () => {
    const wrapper = mountOrderPage()
    await flushPromises()
    const honeypotWrap = wrapper.find('.honeypot-field')
    expect(honeypotWrap.exists()).toBe(true)
    expect(honeypotWrap.attributes('aria-hidden')).toBe('true')
    const honeypotInput = wrapper.find('#order-website')
    expect(honeypotInput.exists()).toBe(true)
    expect(honeypotInput.attributes('tabindex')).toBe('-1')
  })

  it('submits company name, email, slug and empty honeypot, then shows the success message', async () => {
    const fetchMock = routedFetch()
    const wrapper = mountOrderPage(fetchMock)
    await flushPromises()

    await wrapper.find('#order-company').setValue('Acme Testing GmbH')
    await wrapper.find('#order-email').setValue('bestellung@example.com')
    await wrapper.find('#order-slug').setValue('acme-testing')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    const postCall = fetchMock.mock.calls.find(([, opts]) => (opts?.method) === 'POST')
    expect(postCall).toBeTruthy()
    const [url, options] = postCall
    expect(url).toContain('/api/public/orders')
    const body = JSON.parse(options.body)
    expect(body).toMatchObject({
      company_name: 'Acme Testing GmbH',
      email: 'bestellung@example.com',
      slug: 'acme-testing',
      website: '',
    })

    expect(wrapper.find('.order-success').exists()).toBe(true)
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('shows the server error message when the order is rejected', async () => {
    const fetchMock = routedFetch({
      postResponse: jsonResponse({ error: 'Slug wird bereits von einer anderen Statusseite verwendet' }, 409),
    })
    const wrapper = mountOrderPage(fetchMock)
    await flushPromises()

    await wrapper.find('#order-company').setValue('Acme')
    await wrapper.find('#order-email').setValue('bestellung@example.com')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.order-error').exists()).toBe(true)
    expect(wrapper.find('.order-error').attributes('role')).toBe('alert')
    expect(wrapper.text()).toContain('Slug wird bereits von einer anderen Statusseite verwendet')
    // Nach einem Fehler bleibt das Formular sichtbar, damit direkt korrigiert werden kann.
    expect(wrapper.find('form').exists()).toBe(true)
  })

  // ist die Bestellung deaktiviert, zeigt die Seite statt des
  // Formulars einen Hinweis.
  it('shows a disabled notice and no form when ordering is turned off', async () => {
    const wrapper = mountOrderPage(routedFetch({ enabled: false }))
    await flushPromises()

    expect(wrapper.find('.order-disabled').exists()).toBe(true)
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.text()).toContain('Bestellung derzeit nicht verfügbar')
  })

  // scheitert das Laden des Flags, bleibt die Seite optimistisch
  // bedienbar (Formular sichtbar), der Fehler wird nur geloggt.
  it('zeigt das Formular weiter, wenn das Flag nicht geladen werden kann', async () => {
    const fetchMock = vi.fn(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'GET' && String(url).includes('/api/public/orders/enabled')) {
        throw new Error('Netzwerkfehler')
      }
      return jsonResponse({ message: 'ok' }, 201)
    })
    const wrapper = mountOrderPage(fetchMock)
    await flushPromises()

    expect(wrapper.find('form').exists()).toBe(true)
    expect(wrapper.find('.order-disabled').exists()).toBe(false)
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = mountOrderPage()
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Firma und E-Mail sind Pflicht, der optionale Slug nicht.
  it('kennzeichnet Firma und E-Mail als Pflichtfelder, den Slug nicht', async () => {
    const wrapper = mountOrderPage()
    await flushPromises()
    for (const id of ['order-company', 'order-email']) {
      expect(wrapper.find(`label[for="${id}"]`).classes()).toContain('onyx-required-marker')
      expect(wrapper.find(`#${id}`).attributes('aria-required')).toBe('true')
    }
    expect(wrapper.find('label[for="order-slug"]').classes()).not.toContain('onyx-required-marker')
    expect(wrapper.find('[data-test="required-hint"]').text()).toBe('Felder mit * sind Pflichtfelder.')
  })

  // reservierter Slug wird mit übersetztem Text gemeldet.
  it('übersetzt slug_reserved vom Server', async () => {
    const wrapper = mountOrderPage(routedFetch({
      postResponse: jsonResponse({ error: 'Dieser Slug ist reserviert und kann nicht verwendet werden', code: 'slug_reserved' }, 400),
    }))
    await flushPromises()
    await wrapper.find('#order-company').setValue('Acme')
    await wrapper.find('#order-email').setValue('a@example.com')
    await wrapper.find('#order-slug').setValue('www')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('.order-error').text()).toContain('Dieser Slug ist reserviert. Bitte einen anderen wählen.')
  })
})
