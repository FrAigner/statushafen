import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../i18n'
import HelpTooltipsToggle from './HelpTooltipsToggle.vue'
import { useAuth } from '../composables/useAuth'
import { helpTooltipsEnabled, useHelpTooltips } from '../composables/useHelpTooltips'
import { axe } from '../test-support/axe.js'

// Schalter „Hilfe-Tooltips anzeigen“, Default an, wirkt sofort,
// gespeichert per PATCH /api/me.

let wrapper = null

function mountToggle() {
  wrapper = mount(HelpTooltipsToggle, { attachTo: document.body, global: { plugins: [i18n] } })
  return wrapper
}

const checkbox = () => wrapper.find('input[type="checkbox"]')

describe('HelpTooltipsToggle / useHelpTooltips', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    useAuth().setAuth('jwt-1', { email: 'a@b.de' })
    global.fetch = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ help_tooltips: false }) }))
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.restoreAllMocks()
    useAuth().logout()
  })

  it('ist ohne gespeicherte Einstellung eingeschaltet (Default an)', () => {
    mountToggle()
    expect(wrapper.text()).toContain('Hilfe-Tooltips anzeigen')
    expect(checkbox().element.checked).toBe(true)
    expect(helpTooltipsEnabled.value).toBe(true)
  })

  it('zeigt die gespeicherte Einstellung', () => {
    useAuth().setAuth('jwt-1', { email: 'a@b.de', help_tooltips: false })
    mountToggle()
    expect(checkbox().element.checked).toBe(false)
    expect(helpTooltipsEnabled.value).toBe(false)
  })

  it('schaltet sofort lokal und speichert per PATCH /api/me', async () => {
    let resolve
    global.fetch = vi.fn(() => new Promise(r => { resolve = r }))
    mountToggle()

    await checkbox().setValue(false)
    // Noch vor der Server-Antwort wirksam.
    expect(helpTooltipsEnabled.value).toBe(false)
    expect(useAuth().currentUser.value).toMatchObject({ email: 'a@b.de', help_tooltips: false })
    expect(global.fetch).toHaveBeenCalledWith('/api/me', expect.objectContaining({
      method: 'PATCH', body: JSON.stringify({ help_tooltips: false }),
    }))

    resolve({ ok: true, status: 200, json: async () => ({ help_tooltips: false }) })
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('Fehler beim Speichern: Wahl bleibt, Meldung erscheint und verschwindet beim nächsten Versuch', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    global.fetch = vi.fn(async () => ({ ok: false, status: 500, json: async () => ({ error: 'kaputt' }) }))
    mountToggle()

    await checkbox().setValue(false)
    await flushPromises()
    expect(helpTooltipsEnabled.value).toBe(false)
    expect(wrapper.find('[role="alert"]').text()).toContain('konnte nicht gespeichert werden')
    expect(error).toHaveBeenCalled()

    global.fetch = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
    await checkbox().setValue(true)
    await flushPromises()
    expect(helpTooltipsEnabled.value).toBe(true)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('ohne angemeldeten Benutzer passiert nichts', async () => {
    useAuth().logout()
    let api
    mount({ template: '<div />', setup() { api = useHelpTooltips() } }, { global: { plugins: [i18n] } })
    await api.setHelpTooltips(false)
    expect(global.fetch).not.toHaveBeenCalled()
    expect(helpTooltipsEnabled.value).toBe(true)
  })

  it('hat keine axe-Funde, auch mit Fehlermeldung', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mountToggle()
    expect(await axe(wrapper)).toEqual([])
    global.fetch = vi.fn(async () => { throw new Error('offline') })
    await checkbox().setValue(false)
    await flushPromises()
    expect(await axe(wrapper)).toEqual([])
  })
})
