import { ref, computed, nextTick } from 'vue'
import type { Ref } from 'vue'

/** A placeholder of the list; only the name is needed here. */
export interface Placeholder {
  name: string
}

export type PlaceholderTextElement = HTMLInputElement | HTMLTextAreaElement

/** A text field that placeholders can be inserted into. element() returns the native field (present once mounted). */
export interface PlaceholderField {
  model: Ref<string>
  element: () => PlaceholderTextElement | null
}

export interface PlaceholderInsertOptions<F extends string, P extends Placeholder> {
  placeholders: Readonly<Ref<P[]>>
  fields: Record<F, PlaceholderField>
  /** Target of a click as long as no field has been focused yet. */
  defaultField: NoInfer<F>
  /** Prefix for the IDs of listbox and options. */
  idPrefix: string
}

// Insert placeholders ({{.Title}} & co.) into text fields, by clicking the
// placeholder list or by autocompletion when typing `{`.
//
// The pure functions (insertText, findPlaceholderPrefix, filterPlaceholders)
// are exported separately and testable without a DOM. usePlaceholderInsert
// holds the state (last focused field, open suggestion list, active entry)
// and works on the native <input>/<textarea> elements the view provides via
// `fields[name].element()`. The view binds the handlers via event delegation
// to a wrapper around the sit-onyx fields, because OnyxInput/OnyxTextarea do
// not expose the native element directly.

export const placeholderToken = (name: string) => `{{.${name}}}`

// Replaces value[start, end) with text; the cursor then sits behind text.
// start === end is a plain insert, otherwise the selection is replaced.
export function insertText(value: string, start: number, end: number, text: string): { value: string; cursor: number } {
  return {
    value: value.slice(0, start) + text + value.slice(end),
    cursor: start + text.length,
  }
}

// Detects a started placeholder prefix directly before the cursor: `{`, `{{`,
// `{{.` or `{{.Sta`. Returns the start index of the prefix and the part of the
// name typed so far, otherwise null.
const PREFIX_PATTERN = /\{\{?(?:\.(\w*))?$/

export function findPlaceholderPrefix(value: string, cursor: number): { start: number; query: string } | null {
  const match = PREFIX_PATTERN.exec(value.slice(0, cursor))
  if (!match) return null
  return { start: match.index, query: match[1] ?? '' }
}

// Filters by the start of the name, case-insensitively: `sta` finds Status
// and StatusLabel.
export function filterPlaceholders<P extends Placeholder>(placeholders: P[], query: string): P[] {
  const needle = query.toLowerCase()
  return placeholders.filter((p) => p.name.toLowerCase().startsWith(needle))
}

export function usePlaceholderInsert<F extends string, P extends Placeholder = Placeholder>({ placeholders, fields, defaultField, idPrefix }: PlaceholderInsertOptions<F, P>) {
  const lastField = ref(defaultField) as Ref<F>
  const openField = ref(null) as Ref<F | null>
  const query = ref('')
  const activeIndex = ref(0)
  // Fields that have been focused before: only there is selectionStart a real
  // cursor position. A never focused field gets the placeholder at the end
  // instead of at position 0.
  const focusedFields = new Set<F>()
  let prefixStart = 0

  const suggestions = computed(() => filterPlaceholders(placeholders.value, query.value))
  const isOpen = (field: F) => openField.value === field && suggestions.value.length > 0
  const listboxId = (field: F) => `${idPrefix}-${field}-listbox`
  const optionId = (field: F, index: number) => `${idPrefix}-${field}-option-${index}`

  const close = () => {
    openField.value = null
  }

  const replaceRange = async (field: F, start: number, end: number, text: string) => {
    const { model, element } = fields[field]
    const result = insertText(model.value, start, end, text)
    model.value = result.value
    lastField.value = field
    await nextTick()
    const el = element()!
    el.focus()
    el.setSelectionRange(result.cursor, result.cursor)
  }

  const insertPlaceholder = (item: P) => {
    const field = lastField.value
    const el = fields[field].element()!
    const end = fields[field].model.value.length
    const [start, stop] = focusedFields.has(field) ? [el.selectionStart!, el.selectionEnd!] : [end, end]
    close()
    return replaceRange(field, start, stop, placeholderToken(item.name))
  }

  const accept = (field: F, item: P) => {
    const cursor = fields[field].element()!.selectionStart!
    close()
    return replaceRange(field, prefixStart, cursor, placeholderToken(item.name))
  }

  const evaluate = (field: F, el: PlaceholderTextElement) => {
    const collapsed = el.selectionStart === el.selectionEnd
    const prefix = collapsed ? findPlaceholderPrefix(el.value, el.selectionStart!) : null
    if (!prefix) {
      close()
      return
    }
    if (openField.value !== field || query.value !== prefix.query) activeIndex.value = 0
    openField.value = field
    query.value = prefix.query
    prefixStart = prefix.start
  }

  const isOwnTarget = (field: F, event: Event) => event.target === fields[field].element()

  const onFocus = (field: F) => {
    lastField.value = field
    focusedFields.add(field)
  }

  const onInput = (field: F, event: Event) => {
    if (isOwnTarget(field, event)) evaluate(field, event.target as PlaceholderTextElement)
  }

  // A click or arrow key left/right moves the cursor: if it no longer sits
  // behind a prefix afterwards, the list closes.
  const onCaretMove = (field: F, event: Event) => {
    if (openField.value === field && isOwnTarget(field, event)) evaluate(field, event.target as PlaceholderTextElement)
  }

  const onKeydown = (field: F, event: KeyboardEvent) => {
    if (!isOpen(field)) return
    const count = suggestions.value.length
    const actions: Record<string, () => unknown> = {
      ArrowDown: () => { activeIndex.value = (activeIndex.value + 1) % count },
      ArrowUp: () => { activeIndex.value = (activeIndex.value - 1 + count) % count },
      Enter: () => accept(field, suggestions.value[activeIndex.value]!),
      Tab: () => accept(field, suggestions.value[activeIndex.value]!),
      Escape: () => {
        // Escape belongs to the list, not to a surrounding dialog or the like.
        event.stopPropagation()
        close()
      },
    }
    const action = actions[event.key]
    if (!action) return
    // Prevents, among other things, Enter in the subject field from submitting the form.
    event.preventDefault()
    action()
  }

  // ARIA combobox pattern: the field keeps the focus, the active option is
  // announced via aria-activedescendant. role=combobox/aria-expanded only on
  // the single-line <input>: per ARIA in HTML a <textarea> must not carry
  // another role, and aria-expanded is not allowed on role=textbox.
  const ariaFor = (field: F, combobox: boolean) => ({
    role: combobox ? 'combobox' : undefined,
    'aria-expanded': combobox ? String(isOpen(field)) : undefined,
    'aria-autocomplete': 'list',
    'aria-controls': listboxId(field),
    'aria-activedescendant': isOpen(field) ? optionId(field, activeIndex.value) : undefined,
  })

  return {
    lastField,
    suggestions,
    activeIndex,
    isOpen,
    listboxId,
    optionId,
    close,
    insertPlaceholder,
    accept,
    onFocus,
    onInput,
    onCaretMove,
    onKeydown,
    ariaFor,
  }
}
