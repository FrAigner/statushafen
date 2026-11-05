import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'
import DataExport from './DataExport.vue'
import ProfileView from '../views/admin/ProfileView.vue'
import { onyxFormStubs } from '../test-support/onyxStubs'
import { axe } from '../test-support/axe.js'

// Knopf für den Datenexport (GET /api/me/export) im Profil.

const EXPORT = { profile: { email: 'a@b.de' }, subscriptions: [] }

let wrapper = null

beforeEach(() => {
  i18n.global.locale.value = 'de'
  useAuth().setAuth('jwt-1', { email: 'a@b.de' })
  URL.createObjectURL = vi.fn(() => 'blob:export')
  URL.revokeObjectURL = vi.fn()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
  useAuth().logout()
})

describe('DataExport', () => {
  it('lädt die Antwort von /api/me/export als Datei me-export.json herunter', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => EXPORT })
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    wrapper = mount(DataExport, { global: { plugins: [i18n] } })

    await wrapper.find('[data-test="data-export-button"]').trigger('click')
    await flushPromises()

    expect(global.fetch.mock.calls[0][0]).toBe('/api/me/export')
    expect(global.fetch.mock.calls[0][1].headers['X-Auth-Token']).toBe('jwt-1')
    const clicked = clickSpy.mock.contexts[0]
    expect(clicked.download).toBe('me-export.json')
    expect(clicked.href).toBe('blob:export')
    const blob = URL.createObjectURL.mock.calls[0][0]
    expect(blob.type).toBe('application/json')
    expect(JSON.parse(await blob.text())).toEqual(EXPORT)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:export')
    expect(wrapper.find('.alert').exists()).toBe(false)
    expect(wrapper.find('[data-test="data-export-button"]').attributes('disabled')).toBeUndefined()
  })

  it('sperrt den Knopf während des Abrufs', async () => {
    let resolve
    global.fetch = vi.fn().mockReturnValue(new Promise((r) => { resolve = r }))
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    wrapper = mount(DataExport, { global: { plugins: [i18n] } })

    await wrapper.find('[data-test="data-export-button"]').trigger('click')
    expect(wrapper.find('[data-test="data-export-button"]').attributes('disabled')).toBeDefined()

    resolve({ ok: true, status: 200, json: async () => EXPORT })
    await flushPromises()
    expect(wrapper.find('[data-test="data-export-button"]').attributes('disabled')).toBeUndefined()
  })

  it('zeigt bei einem Fehler eine Meldung und speichert keine Datei', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({}) })
    wrapper = mount(DataExport, { global: { plugins: [i18n] } })

    await wrapper.find('[data-test="data-export-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.alert').text()).toBe(i18n.global.t('dataExport.error'))
    expect(URL.createObjectURL).not.toHaveBeenCalled()
  })

  it('ist ohne axe-Verstöße', async () => {
    wrapper = mount(DataExport, { global: { plugins: [i18n] } })
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('ist auf der Profilseite eingebunden', () => {
    wrapper = mount(ProfileView, {
      global: { plugins: [i18n], stubs: { ...onyxFormStubs, SSOLinks: true } },
    })
    expect(wrapper.find('[data-test="data-export-button"]').exists()).toBe(true)
  })
})
