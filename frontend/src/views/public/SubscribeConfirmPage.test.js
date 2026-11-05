import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import SubscribeConfirmPage from './SubscribeConfirmPage.vue'
import { axe } from '../../test-support/axe.js'

// Bestätigungsseite des Double-Opt-in für E-Mail-Abos.

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

async function mountPage({ token = 'tok-123', fetchImpl } = {}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/subscribe/confirm', component: SubscribeConfirmPage },
      { path: '/status/:slug', component: { template: '<div/>' } },
    ],
  })
  router.push(token !== null ? `/subscribe/confirm?token=${token}` : '/subscribe/confirm')
  await router.isReady()
  global.fetch = fetchImpl || vi.fn().mockResolvedValue(jsonResponse({ slug: 'acme' }))
  const wrapper = mount(SubscribeConfirmPage, { global: { plugins: [i18n, router] } })
  await flushPromises()
  return wrapper
}

describe('SubscribeConfirmPage', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('zeigt sofort einen Fehler ohne Token und ruft das Backend nicht auf', async () => {
    const fetchMock = vi.fn()
    const wrapper = await mountPage({ token: null, fetchImpl: fetchMock })
    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('.subscribe-confirm-error').text()).toContain('Kein Bestätigungs-Token in diesem Link gefunden.')
    expect(wrapper.find('.btn-confirm').exists()).toBe(false)
    expect(wrapper.vm.state).toBe('error')
  })

  it('bestätigt erst nach Klick (Prefetch-sicher)', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ slug: 'acme' }))
    const wrapper = await mountPage({ fetchImpl: fetchMock })
    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('h1').text()).toBe('Abonnement bestätigen')
    expect(wrapper.find('.btn-confirm').attributes('disabled')).toBeUndefined()
  })

  it('sendet den Token per POST und zeigt Erfolg mit Link zur Statusseite', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ slug: 'acme' }))
    const wrapper = await mountPage({ token: 'mein-token', fetchImpl: fetchMock })

    await wrapper.find('.btn-confirm').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toContain('/api/public/subscribers/confirm')
    expect(options.method).toBe('POST')
    expect(JSON.parse(options.body)).toEqual({ token: 'mein-token' })
    expect(wrapper.find('.subscribe-confirm-success h1').text()).toBe('Abonnement bestätigt')
    expect(wrapper.find('a[href="/status/acme"]').exists()).toBe(true)
  })

  it('zeigt ohne Slug in der Antwort keinen Link zur Statusseite', async () => {
    const wrapper = await mountPage({ fetchImpl: vi.fn().mockResolvedValue(jsonResponse({})) })
    await wrapper.find('.btn-confirm').trigger('click')
    await flushPromises()
    expect(wrapper.find('.subscribe-confirm-success').exists()).toBe(true)
    expect(wrapper.find('a').exists()).toBe(false)
  })

  it('sperrt den Button während der Anfrage', async () => {
    let resolve
    const fetchMock = vi.fn().mockReturnValue(new Promise(r => { resolve = r }))
    const wrapper = await mountPage({ fetchImpl: fetchMock })
    await wrapper.find('.btn-confirm').trigger('click')
    expect(wrapper.find('.btn-confirm').attributes('disabled')).toBeDefined()
    resolve(jsonResponse({ slug: 'acme' }))
    await flushPromises()
  })

  it('zeigt die Server-Fehlermeldung (z. B. 410 abgelaufen)', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ error: 'Der Bestätigungslink ist abgelaufen.' }, 410))
    const wrapper = await mountPage({ fetchImpl: fetchMock })
    await wrapper.find('.btn-confirm').trigger('click')
    await flushPromises()
    expect(wrapper.find('.subscribe-confirm-error').text()).toContain('Der Bestätigungslink ist abgelaufen.')
    expect(wrapper.vm.state).toBe('error')
  })

  it('ist auf Englisch übersetzt', async () => {
    i18n.global.locale.value = 'en'
    const wrapper = await mountPage()
    expect(wrapper.find('h1').text()).toBe('Confirm subscription')
  })

  // Stufe 4e: alle Zustände ohne axe-Funde; der Fehler ist ein Alert.
  it('hat in allen Zuständen keine axe-Funde, der Fehler ist ein Alert', async () => {
    const idle = await mountPage()
    expect(await axe(idle)).toHaveNoViolations()
    await idle.find('.btn-confirm').trigger('click')
    await flushPromises()
    expect(await axe(idle)).toHaveNoViolations()

    const failed = await mountPage({ token: null })
    expect(failed.find('[role="alert"]').text()).toContain('Kein Bestätigungs-Token')
    expect(await axe(failed)).toHaveNoViolations()
  })
})
