// Richtung C, Betriebsseiten: Suche und Segment-Filter für die
// Listen im Kartenkopf (SearchBox, SegmentControl). Die Suche vergleicht ohne
// Groß-/Kleinschreibung gegen textOf(item); der Filter wählt ein Prädikat
// aus `predicates` (Schlüssel = Wert des Umschalters, „all“ filtert nicht).
import { computed, ref } from 'vue'

export function useListQuery(items, { textOf, predicates = {} }) {
  const query = ref('')
  const filter = ref('all')
  const searching = computed(() => query.value.trim() !== '')
  const filtered = computed(() => filter.value !== 'all')
  const rows = computed(() => {
    const needle = query.value.trim().toLowerCase()
    const matchesFilter = predicates[filter.value] ?? (() => true)
    return items.value.filter(item =>
      matchesFilter(item) && (!needle || textOf(item).toLowerCase().includes(needle)))
  })
  return { query, filter, rows, searching, filtered }
}
