import { describe, it, expect } from 'vitest'
import { ref } from 'vue'
import { useListQuery } from './useListQuery'

const items = () => ref([
  { id: 1, name: 'Alpha', open: true },
  { id: 2, name: 'Beta', open: false },
  { id: 3, name: 'alphabet', open: false },
])
const options = { textOf: i => i.name, predicates: { open: i => i.open, closed: i => !i.open } }

describe('useListQuery', () => {
  it('zeigt ohne Suche und Filter alle Einträge', () => {
    const { rows, searching, filtered } = useListQuery(items(), options)
    expect(rows.value).toHaveLength(3)
    expect(searching.value).toBe(false)
    expect(filtered.value).toBe(false)
  })

  it('sucht ohne Groß-/Kleinschreibung und ignoriert Leerraum um den Suchbegriff', () => {
    const { rows, query, searching } = useListQuery(items(), options)
    query.value = '  ALPHA '
    expect(rows.value.map(r => r.id)).toEqual([1, 3])
    expect(searching.value).toBe(true)
  })

  it('wendet das Prädikat des gewählten Filters an und kombiniert es mit der Suche', () => {
    const { rows, query, filter, filtered } = useListQuery(items(), options)
    filter.value = 'closed'
    expect(rows.value.map(r => r.id)).toEqual([2, 3])
    expect(filtered.value).toBe(true)
    query.value = 'alpha'
    expect(rows.value.map(r => r.id)).toEqual([3])
  })

  it('behandelt einen unbekannten Filter wie „alle“ und reagiert auf neue Daten', () => {
    const list = items()
    const { rows, filter } = useListQuery(list, options)
    filter.value = 'unbekannt'
    expect(rows.value).toHaveLength(3)
    list.value = [...list.value, { id: 4, name: 'Gamma', open: true }]
    expect(rows.value).toHaveLength(4)
  })

  it('braucht keine Prädikate', () => {
    const { rows, query } = useListQuery(items(), { textOf: i => i.name })
    query.value = 'beta'
    expect(rows.value.map(r => r.id)).toEqual([2])
  })
})
