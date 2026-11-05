import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { useResourcePerms } from '../../composables/useResourcePerms'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminNotices from './AdminNotices.vue'
import AdminSubscribers from './AdminSubscribers.vue'

// Knöpfe, Zeilenmenü und Formular richten sich je Aktion nach dem Recht
// des Tabs: "Neu" nur mit create, Bearbeiten nur mit update (sonst "Ansehen" mit
// gesperrtem Formular), Löschen nur mit delete.
const VIEWS = [
  { name: 'Ankündigungen', component: AdminNotices, resource: 'notices', item: { ID: 1, Title: 'Info', Message: 'm', Level: 'info', IsActive: true } },
  { name: 'Abonnenten', component: AdminSubscribers, resource: 'subscribers', item: { ID: 1, Channel: 'email', Email: 'a@b.de', SubscribedComponentIDs: 'all', CreatedAt: '2026-01-01T00:00:00Z' } },
]

let wrapper = null
async function mountView(view, perms) {
  setAuthAsPageRole('jwt-1', { permissions: perms, is_global_admin: false })
  useTenant().setActiveTenant('1')
  global.fetch = vi.fn(async (url) => ({ ok: true, status: 200, json: async () => (/components$/.test(url) ? [] : [view.item]) }))
  wrapper = mount(view.component, { global: { plugins: [i18n], stubs: { ...onyxFormStubs, RouterLink: RouterLinkStub } } })
  await flushPromises()
  return wrapper
}
// „Übersetzungen“ ist ein Link (Leseaktion) und hier nicht Thema.
const entries = () => [...new Set(wrapper.findAll('[role="menuitem"]').map(i => i.text()))].filter(t => t !== 'Übersetzungen')
const newButton = () => wrapper.find('.page-head__actions button, .page-head button')

describe.each(VIEWS)('Rechte je Aktion: $name', (view) => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setMyPages([])
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })
  afterEach(() => { wrapper?.unmount(); wrapper = null; vi.restoreAllMocks() })
  const p = (...actions) => actions.map(a => `${view.resource}:${a}`).join(',')

  it('nur read: kein Neu-Knopf, im Menü nur Ansehen', async () => {
    await mountView(view, p('read'))
    expect(newButton().exists()).toBe(false)
    expect(entries()).toEqual(['Ansehen'])
  })

  it('nur create: Neu-Knopf da, aber kein Bearbeiten/Löschen (Lesen durch Implikation)', async () => {
    await mountView(view, p('create'))
    expect(newButton().exists()).toBe(true)
    expect(entries()).toEqual(['Ansehen'])
  })

  it('nur update: Bearbeiten statt Ansehen, kein Neu-Knopf, kein Löschen', async () => {
    await mountView(view, p('update'))
    expect(newButton().exists()).toBe(false)
    expect(entries()).toEqual(['Bearbeiten'])
  })

  it('nur delete: Löschen und Ansehen, kein Bearbeiten', async () => {
    await mountView(view, p('delete'))
    expect(entries()).toEqual(['Ansehen', 'Löschen'])
  })

  it('Ansehen öffnet das Formular gesperrt und ohne Speichern-Knopf', async () => {
    await mountView(view, p('read'))
    await wrapper.findAll('[role="menuitem"]').find(i => i.text() === 'Ansehen').trigger('click')
    await flushPromises()
    expect(wrapper.find('fieldset.form-lock').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.form-lock-hint').text()).toContain('Nur Lesezugriff')
    expect(wrapper.findAll('button[type="submit"]').length).toBe(0)
  })

  it('mit update ist das Formular offen und speicherbar', async () => {
    await mountView(view, p('update'))
    await wrapper.findAll('[role="menuitem"]').find(i => i.text() === 'Bearbeiten').trigger('click')
    await flushPromises()
    expect(wrapper.find('fieldset.form-lock').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('.form-lock-hint').exists()).toBe(false)
    expect(wrapper.findAll('button[type="submit"]').length).toBe(1)
  })
})

describe('useResourcePerms', () => {
  it('liefert die Aktionen eines Tabs mit Implikation', () => {
    useAuth().setAuth('jwt-1', { is_global_admin: false })
    useTenant().setMyPages([{ ID: 1, Permissions: 'notices:update,tags:delete' }])
    useTenant().setActiveTenant(1)
    const n = useResourcePerms('notices')
    expect([n.canRead.value, n.canCreate.value, n.canUpdate.value, n.canDelete.value]).toEqual([true, false, true, false])
    const tags = useResourcePerms('tags')
    expect(tags.canDelete.value).toBe(false) // globale Ressource: nur Global Admins
  })
})
