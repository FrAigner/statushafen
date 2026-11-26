import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import OrderPage from './OrderPage.vue'
import { axe } from '../../test-support/axe'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// OrderPage loads the flag /api/public/orders/enabled on mount.
// The default mock returns enabled:true for it and the success answer for POST;
// individual tests override the POST answer or the flag specifically.
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
    // Tests assert on German wording; without a stored preference
    // detectInitialLocale() would fall back to 'en' in happy-dom (navigator.language en-US).
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
    // After an error the form stays visible, so it can be corrected right away.
    expect(wrapper.find('form').exists()).toBe(true)
  })

  // If ordering is disabled, the page shows a notice instead of the
  // form.
  it('shows a disabled notice and no form when ordering is turned off', async () => {
    const wrapper = mountOrderPage(routedFetch({ enabled: false }))
    await flushPromises()

    expect(wrapper.find('.order-disabled').exists()).toBe(true)
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.text()).toContain('Bestellung derzeit nicht verfügbar')
  })

  // If loading the flag fails, the page stays optimistically
  // usable (form visible), the error is only logged.
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

  // Company and email are required, the optional slug is not.
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

  // A reserved slug is reported with a translated text.
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
