import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import OrderConfirmPage from './OrderConfirmPage.vue'
import { axe } from '../../test-support/axe.js'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/order/confirm', component: OrderConfirmPage },
      { path: '/order/set-password', component: { template: '<div/>' } },
    ],
  })
}

async function mountConfirmPage({ token = 'a-real-token', fetchImpl } = {}) {
  const router = makeRouter()
  router.push(token !== null ? `/order/confirm?token=${token}` : '/order/confirm')
  await router.isReady()

  global.fetch = fetchImpl || vi.fn().mockResolvedValue(jsonResponse({ set_password_token: 'spw-token', slug: 'acme' }, 200))

  const wrapper = mount(OrderConfirmPage, {
    global: { plugins: [i18n, router] },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('OrderConfirmPage', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('shows an error immediately when no token is present in the URL', async () => {
    const { wrapper } = await mountConfirmPage({ token: null })
    expect(wrapper.find('.order-confirm-error').exists()).toBe(true)
    expect(wrapper.find('.order-confirm-error').attributes('role')).toBe('alert')
  })

  it('does not call the backend until the confirm button is clicked (prefetch-safe)', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ set_password_token: 'spw-token', slug: 'acme' }, 200))
    const { wrapper } = await mountConfirmPage({ fetchImpl: fetchMock })

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('.btn-confirm').exists()).toBe(true)
  })

  it('confirms on click and shows a link to set the password', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ set_password_token: 'spw-token', slug: 'acme' }, 200))
    const { wrapper } = await mountConfirmPage({ token: 'my-confirm-token', fetchImpl: fetchMock })

    await wrapper.find('.btn-confirm').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toContain('/api/public/orders/confirm')
    expect(JSON.parse(options.body)).toMatchObject({ token: 'my-confirm-token' })

    expect(wrapper.find('.order-confirm-success').exists()).toBe(true)
    const link = wrapper.find('a[href*="/order/set-password"]')
    expect(link.exists()).toBe(true)
    expect(link.attributes('href')).toContain('spw-token')
  })

  it('shows the server error message when confirmation fails', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ error: 'Der Bestätigungslink ist abgelaufen.' }, 410))
    const { wrapper } = await mountConfirmPage({ fetchImpl: fetchMock })

    await wrapper.find('.btn-confirm').trigger('click')
    await flushPromises()

    expect(wrapper.find('.order-confirm-error').exists()).toBe(true)
    expect(wrapper.find('.order-confirm-error').attributes('role')).toBe('alert')
    expect(wrapper.text()).toContain('Der Bestätigungslink ist abgelaufen.')
  })

  it('hat keine axe-Funde vor und nach der Bestätigung', async () => {
    const { wrapper } = await mountConfirmPage()
    expect(await axe(wrapper)).toHaveNoViolations()

    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
