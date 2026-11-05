import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'
import { onyxFormStubs } from '../test-support/onyxStubs'
import TranslationSettings from './TranslationSettings.vue'

// Abschnitt „Automatische Übersetzung“ der Systemeinstellungen.

const json = (body, status = 200) => ({ ok: status < 400, status, json: async () => body })
const BASE = { provider: '', url: '', api_key_set: false, env_url_set: true, env_api_key_set: false, effective_provider: 'libretranslate', configured: true }

let wrapper
const calls = (fetchMock, method) => fetchMock.mock.calls.filter(([, o]) => (o?.method || 'GET') === method)

async function mountSection(handler) {
  useAuth().setAuth('jwt', { email: 'a@x.de', is_global_admin: true })
  const fetchMock = vi.fn(handler)
  global.fetch = fetchMock
  wrapper = mount(TranslationSettings, { global: { plugins: [i18n], stubs: onyxFormStubs } })
  await flushPromises()
  return fetchMock
}

describe('TranslationSettings', () => {
  beforeEach(() => { i18n.global.locale.value = 'de'; vi.spyOn(console, 'error').mockImplementation(() => {}) })
  afterEach(() => { wrapper?.unmount(); vi.restoreAllMocks() })

  it('zeigt den Standarddienst des Betreibers und keine Schlüsselfelder', async () => {
    await mountSection(async () => json(BASE))
    expect(wrapper.find('[data-test="translation-status"]').text()).toContain('LibreTranslate')
    expect(wrapper.text()).toContain('LibreTranslate-Dienst des Betreibers')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
  })

  it('meldet fehlenden Standarddienst und „nicht eingerichtet“', async () => {
    await mountSection(async () => json({ ...BASE, env_url_set: false, configured: false, effective_provider: '' }))
    expect(wrapper.find('[data-test="translation-status"]').text()).toContain('kein Übersetzungsdienst eingerichtet')
    expect(wrapper.text()).toContain('LIBRETRANSLATE_URL')
  })

  it('speichert DeepL mit Schlüssel, zeigt Datenschutzhinweis und leert das Schlüsselfeld', async () => {
    const fetchMock = await mountSection(async (url, o = {}) => (o.method === 'PUT'
      ? json({ ...BASE, provider: 'deepl', api_key_set: true, effective_provider: 'deepl' })
      : json(BASE)))
    await wrapper.find('select').setValue('deepl')
    expect(wrapper.find('[data-test="deepl-info"]').text()).toContain('DeepL gesendet')
    expect(wrapper.find('input[aria-label="URL"]').exists()).toBe(false)
    await wrapper.find('input[type="password"], input[aria-label="API-Schlüssel"]').setValue('geheim')
    await wrapper.find('button.save-translation').trigger('click')
    await flushPromises()

    const put = calls(fetchMock, 'PUT')[0]
    expect(put[0]).toBe('/api/settings/translation')
    expect(JSON.parse(put[1].body)).toEqual({ provider: 'deepl', api_key: 'geheim' })
    expect(wrapper.emitted('message').at(-1)[1]).toBe('success')
    expect(wrapper.find('input[aria-label="API-Schlüssel"]').element.value).toBe('')
    expect(wrapper.find('[data-test="clear-key"]').exists()).toBe(true)
  })

  it('LibreTranslate: sendet die Adresse; „Schlüssel entfernen“ sendet einen leeren Schlüssel', async () => {
    const fetchMock = await mountSection(async (url, o = {}) => (o.method === 'PUT'
      ? json({ ...BASE, provider: 'libretranslate', url: 'https://t.example.org' })
      : json({ ...BASE, provider: 'libretranslate', url: 'https://t.example.org', api_key_set: true })))
    const url = wrapper.find('input[aria-label="Adresse des LibreTranslate-Servers"]')
    expect(url.element.value).toBe('https://t.example.org')
    await url.setValue('https://neu.example.org')
    await wrapper.find('[data-test="clear-key"]').setValue(true)
    await wrapper.find('button.save-translation').trigger('click')
    await flushPromises()
    expect(JSON.parse(calls(fetchMock, 'PUT')[0][1].body)).toEqual({ provider: 'libretranslate', url: 'https://neu.example.org', api_key: '' })
  })

  it('Standard ohne Änderung am Schlüssel sendet nur den Dienst', async () => {
    const fetchMock = await mountSection(async (url, o = {}) => json(BASE))
    await wrapper.find('button.save-translation').trigger('click')
    await flushPromises()
    expect(JSON.parse(calls(fetchMock, 'PUT')[0][1].body)).toEqual({ provider: '' })
  })

  it('meldet Ladefehler und Speicherfehler', async () => {
    await mountSection(async () => json({ error: 'kaputt' }, 500))
    expect(wrapper.emitted('message')[0][1]).toBe('error')
    wrapper.unmount()

    await mountSection(async (url, o = {}) => (o.method === 'PUT' ? json({ code: 'translation_url_invalid' }, 400) : json(BASE)))
    await wrapper.find('button.save-translation').trigger('click')
    await flushPromises()
    const [msg, type] = wrapper.emitted('message').at(-1)
    expect(type).toBe('error')
    expect(msg).toContain('https://')
  })

  it('Verbindungstest zeigt das Ergebnis bzw. die Fehlermeldung des Dienstes', async () => {
    const fetchMock = await mountSection(async (url, o = {}) => (o.method === 'POST'
      ? json({ ok: true, provider: 'libretranslate', sample: 'Hallo' })
      : json(BASE)))
    await wrapper.find('button.test-translation').trigger('click')
    await flushPromises()
    expect(calls(fetchMock, 'POST')[0][0]).toBe('/api/settings/translation/test')
    expect(wrapper.find('[data-test="translation-test-result"]').text()).toContain('Hallo')
    wrapper.unmount()

    await mountSection(async (url, o = {}) => (o.method === 'POST' ? json({ code: 'translation_unavailable' }, 502) : json(BASE)))
    await wrapper.find('button.test-translation').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('message').at(-1)[0]).toContain('nicht erreichbar')
    expect(wrapper.find('[data-test="translation-test-result"]').exists()).toBe(false)
  })
})
