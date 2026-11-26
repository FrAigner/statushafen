// Direction C, operations pages: search and segment filter for the lists in
// the card header (SearchBox, SegmentControl). The search compares
// case-insensitively against textOf(item); the filter picks a predicate from
// `predicates` (key = value of the switch, "all" does not filter).
import { computed, ref } from 'vue'
import type { Ref } from 'vue'

export interface ListQueryOptions<T> {
  textOf: (item: T) => string
  predicates?: Record<string, (item: T) => boolean>
}

export function useListQuery<T>(items: Readonly<Ref<T[]>>, { textOf, predicates = {} }: ListQueryOptions<T>) {
  const query = ref('')
  const filter = ref('all')
  const searching = computed(() => query.value.trim() !== '')
  const filtered = computed(() => filter.value !== 'all')
  const rows = computed<T[]>(() => {
    const needle = query.value.trim().toLowerCase()
    const matchesFilter = predicates[filter.value] ?? (() => true)
    return items.value.filter(item =>
      matchesFilter(item) && (!needle || textOf(item).toLowerCase().includes(needle)))
  })
  return { query, filter, rows, searching, filtered }
}
