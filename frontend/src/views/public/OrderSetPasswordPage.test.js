import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import OrderSetPasswordPage from './OrderSetPasswordPage.vue'
import { axe } from '../../test-support/axe.js'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/order/set-password', component: OrderSetPasswordPage },
      { path: '/login', component: { template: '<div/>' } },
    ],
  })
}

async function mountSetPasswordPage({ token = 'spw-token', fetchImpl } = {}) {
  const router = makeRouter()
  router.push(token !== null ? `/order/set-password?token=${token}` : '/order/set-password')
  await router.isReady()

  global.fetch = fetchImpl || vi.fn().mockResolvedValue(jsonResponse({ slug: 'acme' }, 200))

  const wrapper = mount(OrderSetPasswordPage, {
    global: { plugins: [i18n, router] },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('OrderSetPasswordPage', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('shows an error immediately when no token is present in the URL', async () => {
    const { wrapper } = await mountSetPasswordPage({ token: null })
    expect(wrapper.find('.order-setpw-error').exists()).toBe(true)
    expect(wrapper.find('.order-setpw-error').attributes('role')).toBe('alert')
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('blocks submission client-side when the passwords do not match', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ slug: 'acme' }, 200))
    const { wrapper } = await mountSetPasswordPage({ fetchImpl: fetchMock })

    await wrapper.find('#order-password').setValue('correct-horse-battery')
    await wrapper.find('#order-password-confirm').setValue('does-not-match')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Die Passwörter stimmen nicht überein.')
  })

  it('submits the token and password, then shows a login link on success', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ slug: 'acme' }, 200))
    const { wrapper } = await mountSetPasswordPage({ token: 'my-setpw-token', fetchImpl: fetchMock })

    await wrapper.find('#order-password').setValue('correct-horse-battery')
    await wrapper.find('#order-password-confirm').setValue('correct-horse-battery')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toContain('/api/public/orders/set-password')
    expect(JSON.parse(options.body)).toMatchObject({ token: 'my-setpw-token', password: 'correct-horse-battery' })

    expect(wrapper.find('.order-setpw-success').exists()).toBe(true)
    const link = wrapper.find('a[href*="/login"]')
    expect(link.exists()).toBe(true)
  })

  it('shows the server error message when setting the password fails', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ error: 'Passwort muss mindestens 8 Zeichen lang sein' }, 400))
    const { wrapper } = await mountSetPasswordPage({ fetchImpl: fetchMock })

    await wrapper.find('#order-password').setValue('short')
    await wrapper.find('#order-password-confirm').setValue('short')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.find('.order-setpw-form-error').exists()).toBe(true)
    expect(wrapper.find('.order-setpw-form-error').attributes('role')).toBe('alert')
    expect(wrapper.text()).toContain('Passwort muss mindestens 8 Zeichen lang sein')
  })

  it('hat keine axe-Funde', async () => {
    const { wrapper } = await mountSetPasswordPage()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Mindestlänge im Formular (minlength + Hinweis) und vor dem Senden.
  it('zeigt Hinweis und minlength und sendet ein zu kurzes Passwort nicht ab', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ slug: 'acme' }, 200))
    const { wrapper } = await mountSetPasswordPage({ fetchImpl: fetchMock })
    expect(wrapper.find('#order-password').attributes('minlength')).toBe('8')
    expect(wrapper.find('#order-password').attributes('aria-describedby')).toBe('order-password-hint')
    expect(wrapper.find('#order-password-hint').text()).toContain('Mindestens 8 Zeichen')

    await wrapper.find('#order-password').setValue('kurz')
    await wrapper.find('#order-password-confirm').setValue('kurz')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('.order-setpw-form-error').text()).toContain('mindestens 8 Zeichen')
  })

  it('sendet ein zu langes Passwort (mehr als 72 Byte) nicht ab', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ slug: 'acme' }, 200))
    const { wrapper } = await mountSetPasswordPage({ fetchImpl: fetchMock })
    // 40 Umlaute sind 80 Byte, aber nur 40 Zeichen.
    const long = 'ä'.repeat(40)
    await wrapper.find('#order-password').setValue(long)
    await wrapper.find('#order-password-confirm').setValue(long)
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('.order-setpw-form-error').text()).toContain('höchstens 72 Byte')
  })

  it('blendet das Passwort über den Umschalter ein und wieder aus', async () => {
    const { wrapper } = await mountSetPasswordPage()
    const toggle = wrapper.find('button.password-toggle')
    expect(wrapper.find('#order-password').attributes('type')).toBe('password')
    const labelHidden = toggle.attributes('aria-label')

    await toggle.trigger('click')
    expect(wrapper.find('#order-password').attributes('type')).toBe('text')
    expect(wrapper.find('#order-password-confirm').attributes('type')).toBe('text')
    expect(toggle.attributes('aria-label')).not.toBe(labelHidden)

    await toggle.trigger('click')
    expect(wrapper.find('#order-password').attributes('type')).toBe('password')
    expect(toggle.attributes('aria-label')).toBe(labelHidden)
  })

  it('übersetzt password_too_short vom Server', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ error: 'Passwort muss mindestens 8 Zeichen lang sein', code: 'password_too_short' }, 400))
    const { wrapper } = await mountSetPasswordPage({ fetchImpl: fetchMock })
    await wrapper.find('#order-password').setValue('lang-genug-123')
    await wrapper.find('#order-password-confirm').setValue('lang-genug-123')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('.order-setpw-form-error').text()).toContain('mindestens 8 Zeichen lang sein')
  })

  // Pflichtfelder tragen Stern (Klasse), aria-required und den Hinweis.
  it('kennzeichnet Pflichtfelder mit Stern, aria-required und Hinweis', async () => {
    const { wrapper } = await mountSetPasswordPage()
    expect(wrapper.find('label[for="order-password"]').classes()).toContain('onyx-required-marker')
    expect(wrapper.find('label[for="order-password-confirm"]').classes()).toContain('onyx-required-marker')
    expect(wrapper.find('#order-password').attributes('aria-required')).toBe('true')
    expect(wrapper.find('#order-password-confirm').attributes('aria-required')).toBe('true')
    expect(wrapper.find('[data-test="required-hint"]').text()).toBe('Felder mit * sind Pflichtfelder.')
  })

  it('zeigt bei anderen Serverfehlern die Meldung des Servers', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ error: 'Ungültiger oder bereits verwendeter Link' }, 400))
    const { wrapper } = await mountSetPasswordPage({ fetchImpl: fetchMock })
    await wrapper.find('#order-password').setValue('lang-genug-123')
    await wrapper.find('#order-password-confirm').setValue('lang-genug-123')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('.order-setpw-form-error').text()).toContain('Ungültiger oder bereits verwendeter Link')
  })
})
