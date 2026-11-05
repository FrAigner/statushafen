import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import ListStateRow from './ListStateRow.vue'
import { axe } from '../test-support/axe.js'

// Lade-, Fehler- und Leerzustand einer Tabellenliste in einer Zeile.
function mountRow(props) {
  return mount(ListStateRow, {
    props: { colspan: 3, emptyText: 'Nichts da', count: 0, ...props },
    global: { plugins: [i18n] },
    attachTo: document.createElement('tbody'),
  })
}

describe('ListStateRow', () => {
  beforeEach(() => { i18n.global.locale.value = 'de' })

  it('zeigt beim Laden einen Ladeindikator und nicht den Leerzustand', () => {
    const w = mountRow({ loading: true })
    const cell = w.find('[role="status"]')
    expect(cell.attributes('aria-busy')).toBe('true')
    expect(cell.text()).toBe('Lädt…')
    expect(w.text()).not.toContain('Nichts da')
  })

  it('zeigt bei einem Fehler den Fehlerzustand und nicht den Leerzustand', () => {
    const w = mountRow({ error: true })
    expect(w.find('[role="alert"]').text()).toBe('Die Liste konnte nicht geladen werden.')
    expect(w.text()).not.toContain('Nichts da')
  })

  it('zeigt den Leerzustand, wenn nichts lädt und kein Fehler vorliegt', () => {
    const w = mountRow({ cellClass: 'table-empty' })
    expect(w.text()).toBe('Nichts da')
    expect(w.find('td').classes()).toContain('table-empty')
    expect(w.find('td').attributes('colspan')).toBe('3')
    expect(w.find('[role="status"]').exists()).toBe(false)
  })

  it('rendert nichts, wenn Daten vorhanden sind, auch nicht beim Laden oder bei einem Fehler', () => {
    expect(mountRow({ count: 2 }).find('tr').exists()).toBe(false)
    expect(mountRow({ count: 2, loading: true }).find('tr').exists()).toBe(false)
    expect(mountRow({ count: 2, error: true }).find('tr').exists()).toBe(false)
  })

  it('bevorzugt den Ladezustand vor dem Fehlerzustand', () => {
    const w = mountRow({ loading: true, error: true })
    expect(w.find('[role="status"]').exists()).toBe(true)
    expect(w.find('[role="alert"]').exists()).toBe(false)
  })

  it('zeigt die englischen Texte', () => {
    i18n.global.locale.value = 'en'
    expect(mountRow({ loading: true }).text()).toBe('Loading…')
    expect(mountRow({ error: true }).text()).toBe('The list could not be loaded.')
  })

  it('hat keine axe-Funde in Lade-, Fehler- und Leerzustand', async () => {
    for (const props of [{ loading: true }, { error: true }, {}]) {
      const w = mountRow(props)
      expect(await axe(w)).toHaveNoViolations()
      w.unmount()
    }
  })
})
