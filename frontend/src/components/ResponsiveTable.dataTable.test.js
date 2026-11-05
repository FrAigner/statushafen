import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import ResponsiveTable from './ResponsiveTable.vue'
import { axe } from '../test-support/axe.js'

// Stufe 3: Tabellenstil (dataTable) auf Basis von ResponsiveTable.
// Der JS-Umschalter (isMobile) spielt hier keine Rolle: im Tabellenstil gibt
// es nur eine Tabelle, die Kartenansicht kommt aus CSS (siehe patternsCss.test.js).

beforeEach(() => {
  i18n.global.locale.value = 'de'
})

const columns = [
  { key: 'name', label: 'Name' },
  { key: 'count', label: 'Anzahl', numeric: true },
]
const items = [
  { id: 1, name: 'API', count: 3 },
  { id: 2, name: 'Datenbank', count: 12 },
]

function mountTable(props = {}, slots = {}) {
  return mount(ResponsiveTable, {
    props: { columns, items, dataTable: true, ...props },
    slots,
    global: { plugins: [i18n] },
  })
}

describe('ResponsiveTable dataTable', () => {
  it('behält die Klassen .data-table-wrapper und .data-table (e2e-Specs)', () => {
    const w = mountTable()
    expect(w.find('.data-table-wrapper > table.data-table').exists()).toBe(true)
    expect(w.find('.card-grid').exists()).toBe(false)
  })

  it('rendert Kopf mit scope und eine Zeile pro Item', () => {
    const w = mountTable()
    expect(w.findAll('thead th').map(th => th.attributes('scope'))).toEqual(['col', 'col'])
    expect(w.findAll('tbody tr')).toHaveLength(2)
    expect(w.findAll('tbody tr')[1].text()).toContain('Datenbank')
  })

  it('beschriftet jede Zelle mit data-label (Kartenansicht unter 860 px)', () => {
    const cells = mountTable().findAll('tbody tr')[0].findAll('td')
    expect(cells.map(td => td.attributes('data-label'))).toEqual(['Name', 'Anzahl'])
  })

  it('richtet numerische Spalten rechts aus (Kopf und Zellen)', () => {
    const w = mountTable()
    expect(w.findAll('thead th')[0].classes()).not.toContain('is-numeric')
    expect(w.findAll('thead th')[1].classes()).toContain('is-numeric')
    const cells = w.findAll('tbody tr')[0].findAll('td')
    expect(cells[0].classes()).not.toContain('is-numeric')
    expect(cells[1].classes()).toContain('is-numeric')
  })

  it('nutzt Zellen-Slots wie die Standardansicht', () => {
    const w = mountTable({}, { 'cell-name': '<template #cell-name="{ item }"><b class="n">{{ item.name }}!</b></template>' })
    expect(w.find('.n').text()).toBe('API!')
  })

  it('ohne actions-Slot gibt es keine Aktionsspalte', () => {
    const w = mountTable()
    expect(w.find('.actions-col').exists()).toBe(false)
    expect(w.findAll('thead th')).toHaveLength(2)
  })

  it('mit actions-Slot kommt eine letzte Spalte, ihr Kopf nur für Screenreader', () => {
    const w = mountTable({}, { actions: '<template #actions="{ item }"><button type="button">Bearbeiten {{ item.name }}</button></template>' })
    const heads = w.findAll('thead th')
    expect(heads).toHaveLength(3)
    expect(heads[2].classes()).toContain('actions-col')
    expect(heads[2].find('.sr-only').text()).toBe('Aktionen')
    const cells = w.findAll('tbody tr')[0].findAll('td')
    expect(cells).toHaveLength(3)
    expect(cells[2].classes()).toContain('actions-col')
    expect(cells[2].attributes('data-label')).toBeUndefined()
    expect(cells[2].find('button').text()).toBe('Bearbeiten API')
  })

  it('rendert Gruppenzeilen über die ganze Breite mit Text aus der ersten Spalte', () => {
    const w = mountTable({ items: [{ id: 'g', isGroup: true, name: 'Backend' }, ...items] })
    const group = w.find('tr.data-table__group')
    expect(group.text()).toBe('Backend')
    expect(group.find('th').attributes('colspan')).toBe('2')
    expect(group.find('th').attributes('scope')).toBe('colgroup')
    expect(group.find('td').exists()).toBe(false)
    expect(w.findAll('tbody tr')).toHaveLength(3)
  })

  it('zählt die Aktionsspalte in die Breite der Gruppenzeile', () => {
    const w = mountTable(
      { items: [{ id: 'g', isGroup: true, name: 'Backend' }] },
      { actions: '<template #actions><span /></template>' },
    )
    expect(w.find('tr.data-table__group th').attributes('colspan')).toBe('3')
  })

  it('zeigt Lade- und Leerzustand statt der Tabelle', () => {
    expect(mountTable({ loading: true }).text()).toContain('Lädt…')
    expect(mountTable({ loading: true }).find('table').exists()).toBe(false)
    const empty = mountTable({ items: [] })
    expect(empty.text()).toContain('Keine Einträge gefunden.')
    expect(empty.find('table').exists()).toBe(false)
    expect(mountTable({ items: [], emptyText: 'Nichts da' }).text()).toContain('Nichts da')
  })

  it('ohne dataTable bleibt die bisherige Darstellung ohne .data-table', () => {
    const w = mountTable({ dataTable: false })
    expect(w.find('.data-table').exists()).toBe(false)
  })

  it('hat keine axe-Funde (mit Aktionen und Gruppe)', async () => {
    const w = mountTable(
      { items: [{ id: 'g', isGroup: true, name: 'Backend' }, ...items] },
      { actions: '<template #actions="{ item }"><button type="button">Bearbeiten {{ item.name }}</button></template>' },
    )
    expect(await axe(w)).toHaveNoViolations()
  })
})
