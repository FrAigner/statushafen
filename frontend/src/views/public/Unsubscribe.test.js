import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import Unsubscribe from './Unsubscribe.vue'
import { axe } from '../../test-support/axe.js'

// Die Seite beendet das Abo erst nach Klick per
// POST. Vorher feuerte sie beim Mounten einen GET, der direkt löschte -
// Link-Scanner/Prefetcher beendeten so Abos ungewollt.

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/unsubscribe', component: Unsubscribe }],
  })
}

async function mountUnsubscribe(token, fetchImpl = vi.fn().mockResolvedValue(jsonResponse({}))) {
  const router = makeRouter()
  router.push(token !== null ? `/unsubscribe?token=${token}` : '/unsubscribe')
  await router.isReady()

  global.fetch = fetchImpl
  const wrapper = mount(Unsubscribe, { global: { plugins: [i18n, router] } })
  await flushPromises()
  return wrapper
}

describe('Unsubscribe', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('zeigt einen Fehler, wenn kein token-Query-Parameter vorhanden ist', async () => {
    const fetchMock = vi.fn()
    const wrapper = await mountUnsubscribe(null, fetchMock)

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Kein Abbestell-Token in diesem Link gefunden.')
    expect(wrapper.find('.btn-unsubscribe').exists()).toBe(false)
    // Stufe 4e: Der Fehler wird Screenreadern als Alert angesagt.
    expect(wrapper.find('[role="alert"]').text()).toContain('Kein Abbestell-Token')
  })

  it('ruft beim Öffnen nichts auf und wartet auf den Klick (Prefetch-sicher)', async () => {
    const fetchMock = vi.fn()
    const wrapper = await mountUnsubscribe('unsub-token-1', fetchMock)

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('h1').text()).toBe('Abonnement beenden')
    expect(wrapper.find('.btn-unsubscribe').text()).toBe('Jetzt abbestellen')
    expect(wrapper.find('.btn-unsubscribe').attributes('disabled')).toBeUndefined()
  })

  it('sendet den Token nach Klick per POST und zeigt die Erfolgsmeldung', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}))
    const wrapper = await mountUnsubscribe('unsub-token-1', fetchMock)

    await wrapper.find('.btn-unsubscribe').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toContain('/api/public/subscribers/unsubscribe')
    expect(url).not.toContain('token=')
    expect(options.method).toBe('POST')
    expect(JSON.parse(options.body)).toEqual({ token: 'unsub-token-1' })
    expect(wrapper.find('h1').text()).toBe('Erfolgreich abbestellt')
  })

  it('sperrt den Knopf während der Anfrage', async () => {
    let resolve
    const fetchMock = vi.fn().mockReturnValue(new Promise(r => { resolve = r }))
    const wrapper = await mountUnsubscribe('tok', fetchMock)

    await wrapper.find('.btn-unsubscribe').trigger('click')
    expect(wrapper.find('.btn-unsubscribe').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.btn-unsubscribe').text()).toBe('Abonnement wird beendet…')

    resolve(jsonResponse({}))
    await flushPromises()
    expect(wrapper.find('.btn-unsubscribe').exists()).toBe(false)
  })

  it('zeigt die Server-Fehlermeldung, wenn das Abmelden fehlschlägt', async () => {
    const wrapper = await mountUnsubscribe('expired-token', vi.fn().mockResolvedValue(jsonResponse({ error: 'Token abgelaufen' }, 400)))

    await wrapper.find('.btn-unsubscribe').trigger('click')
    await flushPromises()

    expect(wrapper.find('h1').text()).toBe('Abbestellen fehlgeschlagen')
    expect(wrapper.text()).toContain('Token abgelaufen')
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = await mountUnsubscribe('tok')
    expect(await axe(wrapper)).toHaveNoViolations()
    await wrapper.find('.btn-unsubscribe').trigger('click')
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
