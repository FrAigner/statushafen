import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminComponents from './AdminComponents.vue'
import AdminIncidents from './AdminIncidents.vue'
import AdminMaintenance from './AdminMaintenance.vue'

// Auf Mandanten ohne Daten lieferte die API früher "null" statt
// "[]" - die Views müssen beide Antworten als Leerzustand behandeln.
function emptyFetch(body) {
  return vi.fn(async () => ({ ok: true, status: 200, json: async () => body }))
}

let wrapper = null
async function mountView(view, body) {
  setAuthAsPageRole('jwt-1', { permissions: 'components:create,components:update,components:delete,incidents:create,incidents:update,incidents:delete,incident_templates:read,maintenance:create,maintenance:update,maintenance:delete' })
  useTenant().setActiveTenant('1')
  global.fetch = emptyFetch(body)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div/>' } }],
  })
  wrapper = mount(view, { global: { plugins: [i18n, router], stubs: onyxFormStubs } })
  await flushPromises()
  return wrapper
}

describe.each([
  ['null', null],
  ['[]', []],
])('Admin-Views bei leerer API-Antwort (%s)', (_label, body) => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.restoreAllMocks()
    useAuth().logout()
  })

  it('AdminComponents zeigt Leerzustand und Button "Neue Komponente"', async () => {
    const w = await mountView(AdminComponents, body)
    expect(w.text()).toContain('Keine Komponenten vorhanden')
    expect(w.text()).toContain('Neue Komponente')
  })

  it('AdminIncidents zeigt Leerzustand und öffnet das Formular ohne Absturz', async () => {
    const w = await mountView(AdminIncidents, body)
    expect(w.text()).toContain('Keine Vorfälle vorhanden')
    await w.find('.page-head button').trigger('click')
    expect(w.find('.edit-form').exists()).toBe(true)
    expect(w.findAll('.onyx-select-stub-option')).toHaveLength(0)
  })

  it('AdminMaintenance zeigt Leerzustand und öffnet das Formular ohne Absturz', async () => {
    const w = await mountView(AdminMaintenance, body)
    expect(w.text()).toContain('Keine Wartungsfenster geplant')
    await w.find('.page-head button').trigger('click')
    expect(w.find('.edit-form').exists()).toBe(true)
    expect(w.findAll('.onyx-select-stub-option')).toHaveLength(0)
  })
})
