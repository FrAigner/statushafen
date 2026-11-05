import { describe, it, expect } from 'vitest'
import { flattenComponentTree, componentOptions, descendantIds, visibleComponents } from './componentTree'

// Baum aus der Komponenten-API, wie ihn der Seed liefert.
const tree = [
  {
    ID: 1, Name: 'Core', Children: [
      { ID: 4, Name: 'API Services', Children: [{ ID: 7, Name: 'API Gateway (US)', Children: [] }] },
      { ID: 5, Name: 'Datenbanken' },
    ],
  },
  { ID: 2, Name: 'Extern', Children: [] },
]

describe('componentTree', () => {
  it('flacht den Baum in Baumreihenfolge mit Tiefe ab, inklusive Unterkomponenten', () => {
    const flat = flattenComponentTree(tree)
    expect(flat.map(c => [c.ID, c.depth])).toEqual([[1, 0], [4, 1], [7, 2], [5, 1], [2, 0]])
  })

  it('verträgt null/undefined', () => {
    expect(flattenComponentTree(null)).toEqual([])
    expect(componentOptions(undefined)).toEqual([])
  })

  it('rückt die Optionen nach Tiefe ein und liefert IDs als String', () => {
    const opts = componentOptions(flattenComponentTree(tree))
    expect(opts[0]).toEqual({ label: 'Core', value: '1' })
    expect(opts[2]).toEqual({ label: '  API Gateway (US)', value: '7' })
  })

  // archivierte Komponenten sind in keiner Auswahl wählbar.
  it('lässt archivierte Komponenten aus den Optionen weg', () => {
    const opts = componentOptions([
      { ID: 1, Name: 'Aktiv' },
      { ID: 2, Name: 'Alt', ArchivedAt: '2026-11-01T10:00:00Z' },
      { ID: 3, Name: 'Nie archiviert', ArchivedAt: null },
    ])
    expect(opts.map(o => o.value)).toEqual(['1', '3'])
  })

  it('behält archivierte Komponenten, die ausdrücklich gewählt sind (keepIds)', () => {
    const flat = [{ ID: 1, Name: 'Alt', ArchivedAt: 'x' }, { ID: 2, Name: 'Alt2', ArchivedAt: 'x' }]
    expect(componentOptions(flat, [2]).map(o => o.value)).toEqual(['2'])
    // die Formulare halten IDs als Text ("2"), die Liste als Zahl
    expect(componentOptions(flat, ['2']).map(o => o.value)).toEqual(['2'])
    // null (z. B. noch nicht gesetzte Auswahl) darf nicht abstürzen
    expect(componentOptions(flat, null).map(o => o.value)).toEqual([])
  })

  it('behandelt Einträge ohne depth als oberste Ebene', () => {
    expect(componentOptions([{ ID: 3, Name: 'X' }])).toEqual([{ label: 'X', value: '3' }])
  })
})

// einklappbare Gruppen und Suche in der flachen Liste.
describe('descendantIds und visibleComponents', () => {
  const flat = flattenComponentTree(tree) // IDs in Reihenfolge: 1, 4, 7, 5, 2
  const textOf = c => `${c.Name} ${c.Description || ''}`

  it('liefert die Nachfahren eines Eintrags, nicht seine Geschwister', () => {
    expect(descendantIds(flat, 1)).toEqual([4, 7, 5])
    expect(descendantIds(flat, 4)).toEqual([7])
    expect(descendantIds(flat, 7)).toEqual([])
    expect(descendantIds(flat, 2)).toEqual([])
    expect(descendantIds(flat, 999)).toEqual([])
  })

  it('zeigt ohne Suche und Einklappen alles', () => {
    expect(visibleComponents(flat, { textOf }).map(c => c.ID)).toEqual([1, 4, 7, 5, 2])
  })

  it('blendet die Nachfahren eingeklappter Gruppen aus, die Gruppe selbst bleibt', () => {
    expect(visibleComponents(flat, { collapsed: new Set([1]), textOf }).map(c => c.ID)).toEqual([1, 2])
    expect(visibleComponents(flat, { collapsed: new Set([4]), textOf }).map(c => c.ID)).toEqual([1, 4, 5, 2])
  })

  it('klappt nach einer eingeklappten Gruppe die folgenden Geschwister wieder auf', () => {
    const rows = visibleComponents(flat, { collapsed: new Set([1, 2]), textOf })
    expect(rows.map(c => c.ID)).toEqual([1, 2])
  })

  it('zeigt bei Suche Treffer samt Vorfahren, ohne Geschwister, und ignoriert Einklappen', () => {
    const rows = visibleComponents(flat, { query: ' gateway ', collapsed: new Set([1, 4]), textOf })
    expect(rows.map(c => c.ID)).toEqual([1, 4, 7])
  })

  it('findet auch Treffer in der Beschreibung, unabhängig von der Schreibweise', () => {
    const withDesc = flat.map(c => (c.ID === 2 ? { ...c, Description: 'Drittanbieter' } : c))
    expect(visibleComponents(withDesc, { query: 'DRITT', textOf }).map(c => c.ID)).toEqual([2])
    expect(visibleComponents(withDesc, { query: 'gibtesnicht', textOf })).toEqual([])
  })

  it('nimmt bei einem Treffer auf oberster Ebene keine Vorgänger mit', () => {
    expect(visibleComponents(flat, { query: 'Extern', textOf }).map(c => c.ID)).toEqual([2])
  })
})

