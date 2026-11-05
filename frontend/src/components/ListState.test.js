import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import ListState from './ListState.vue'
import { axe } from '../test-support/axe.js'

let wrapper = null
function mountState(props) {
  i18n.global.locale.value = 'de'
  wrapper = mount(ListState, { props: { emptyText: 'Nichts da.', icon: 'info', ...props }, global: { plugins: [i18n] } })
  return wrapper
}
afterEach(() => { wrapper?.unmount(); wrapper = null })

describe('ListState (Stufe 4b)', () => {
  it('zeigt beim Laden einen Statusbereich, weder Fehler noch Leerzustand', () => {
    const w = mountState({ count: 0, loading: true, error: true })
    const status = w.find('[role="status"][aria-busy="true"]')
    expect(status.text()).toBe('Lädt…')
    expect(w.find('[role="alert"]').exists()).toBe(false)
    expect(w.text()).not.toContain('Nichts da.')
  })

  it('zeigt bei einem Ladefehler die Fehlermeldung statt des Leerzustands', () => {
    const w = mountState({ count: 0, error: true })
    expect(w.find('[role="alert"]').text()).toBe('Die Liste konnte nicht geladen werden.')
    expect(w.text()).not.toContain('Nichts da.')
  })

  it('zeigt sonst den Leerzustand mit Titel und Symbol', () => {
    const w = mountState({ count: 0 })
    expect(w.find('.empty-state__title').text()).toBe('Nichts da.')
    expect(w.find('.empty-state svg').exists()).toBe(true)
  })

  it('zeigt bei vorhandenen Zeilen gar nichts', () => {
    const w = mountState({ count: 2, loading: true, error: true })
    expect(w.find('[role="status"]').exists()).toBe(false)
    expect(w.find('[role="alert"]').exists()).toBe(false)
    expect(w.find('.empty-state').exists()).toBe(false)
  })

  it('hat keine axe-Funde', async () => {
    expect(await axe(mountState({ count: 0 }))).toHaveNoViolations()
  })
})
