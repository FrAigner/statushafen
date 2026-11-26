import { ref, computed, nextTick } from 'vue'

// Platzhalter ({{.Title}} & Co.) in Textfelder einfügen, per Klick
// auf die Platzhalterliste oder per Autovervollständigung beim Tippen von `{`.
//
// Die reinen Funktionen (insertText, findPlaceholderPrefix, filterPlaceholders)
// sind getrennt exportiert und ohne DOM testbar. usePlaceholderInsert hält den
// Zustand (zuletzt fokussiertes Feld, offene Vorschlagsliste, aktiver Eintrag)
// und arbeitet auf den nativen <input>/<textarea>-Elementen, die die View über
// `fields[name].element()` liefert. Die View bindet die Handler per
// Event-Delegation an einen Wrapper um die sit-onyx-Felder, weil OnyxInput/
// OnyxTextarea das native Element nicht direkt herausgeben.

export const placeholderToken = (name) => `{{.${name}}}`

// Ersetzt value[start, end) durch text; cursor steht danach hinter text.
// start === end ist ein reines Einfügen, sonst wird die Auswahl ersetzt.
export function insertText(value, start, end, text) {
  return {
    value: value.slice(0, start) + text + value.slice(end),
    cursor: start + text.length,
  }
}

// Erkennt ein angefangenes Platzhalter-Präfix direkt vor dem Cursor: `{`, `{{`,
// `{{.` oder `{{.Sta`. Liefert Startindex des Präfixes und den bisher
// getippten Namensteil, sonst null.
const PREFIX_PATTERN = /\{\{?(?:\.(\w*))?$/

export function findPlaceholderPrefix(value, cursor) {
  const match = PREFIX_PATTERN.exec(value.slice(0, cursor))
  if (!match) return null
  return { start: match.index, query: match[1] ?? '' }
}

// Filtert nach Namensanfang, ohne Groß-/Kleinschreibung: `sta` findet Status
// und StatusLabel.
export function filterPlaceholders(placeholders, query) {
  const needle = query.toLowerCase()
  return placeholders.filter((p) => p.name.toLowerCase().startsWith(needle))
}

/**
 * @param {object} options
 * @param {import('vue').Ref<Array<{name: string}>>} options.placeholders
 * @param {Record<string, {model: import('vue').Ref<string>, element: () => HTMLInputElement|HTMLTextAreaElement}>} options.fields
 * @param {string} options.defaultField Ziel eines Klicks, solange noch kein Feld fokussiert war
 * @param {string} options.idPrefix Präfix für die IDs von Listbox und Optionen
 */
export function usePlaceholderInsert({ placeholders, fields, defaultField, idPrefix }) {
  const lastField = ref(defaultField)
  const openField = ref(null)
  const query = ref('')
  const activeIndex = ref(0)
  // Felder, die schon einmal fokussiert waren: Nur dort ist selectionStart
  // eine echte Cursorposition. Ein nie fokussiertes Feld bekommt den
  // Platzhalter ans Ende statt an Position 0.
  const focusedFields = new Set()
  let prefixStart = 0

  const suggestions = computed(() => filterPlaceholders(placeholders.value, query.value))
  const isOpen = (field) => openField.value === field && suggestions.value.length > 0
  const listboxId = (field) => `${idPrefix}-${field}-listbox`
  const optionId = (field, index) => `${idPrefix}-${field}-option-${index}`

  const close = () => {
    openField.value = null
  }

  const replaceRange = async (field, start, end, text) => {
    const { model, element } = fields[field]
    const result = insertText(model.value, start, end, text)
    model.value = result.value
    lastField.value = field
    await nextTick()
    const el = element()
    el.focus()
    el.setSelectionRange(result.cursor, result.cursor)
  }

  const insertPlaceholder = (item) => {
    const field = lastField.value
    const el = fields[field].element()
    const end = fields[field].model.value.length
    const [start, stop] = focusedFields.has(field) ? [el.selectionStart, el.selectionEnd] : [end, end]
    close()
    return replaceRange(field, start, stop, placeholderToken(item.name))
  }

  const accept = (field, item) => {
    const cursor = fields[field].element().selectionStart
    close()
    return replaceRange(field, prefixStart, cursor, placeholderToken(item.name))
  }

  const evaluate = (field, el) => {
    const collapsed = el.selectionStart === el.selectionEnd
    const prefix = collapsed ? findPlaceholderPrefix(el.value, el.selectionStart) : null
    if (!prefix) {
      close()
      return
    }
    if (openField.value !== field || query.value !== prefix.query) activeIndex.value = 0
    openField.value = field
    query.value = prefix.query
    prefixStart = prefix.start
  }

  const isOwnTarget = (field, event) => event.target === fields[field].element()

  const onFocus = (field) => {
    lastField.value = field
    focusedFields.add(field)
  }

  const onInput = (field, event) => {
    if (isOwnTarget(field, event)) evaluate(field, event.target)
  }

  // Klick oder Pfeiltaste links/rechts verschiebt den Cursor: Liegt er danach
  // nicht mehr hinter einem Präfix, schließt die Liste.
  const onCaretMove = (field, event) => {
    if (openField.value === field && isOwnTarget(field, event)) evaluate(field, event.target)
  }

  const onKeydown = (field, event) => {
    if (!isOpen(field)) return
    const count = suggestions.value.length
    const actions = {
      ArrowDown: () => { activeIndex.value = (activeIndex.value + 1) % count },
      ArrowUp: () => { activeIndex.value = (activeIndex.value - 1 + count) % count },
      Enter: () => accept(field, suggestions.value[activeIndex.value]),
      Tab: () => accept(field, suggestions.value[activeIndex.value]),
      Escape: () => {
        // Escape gehört der Liste, nicht einem umgebenden Dialog o. ä.
        event.stopPropagation()
        close()
      },
    }
    const action = actions[event.key]
    if (!action) return
    // Verhindert u. a., dass Enter im Betreff-Feld das Formular absendet.
    event.preventDefault()
    action()
  }

  // ARIA-Combobox-Muster: Das Feld behält den Fokus, die aktive Option wird
  // per aria-activedescendant angesagt. role=combobox/aria-expanded nur am
  // einzeiligen <input>: Ein <textarea> darf laut ARIA in HTML keine andere
  // Rolle tragen, und aria-expanded ist an role=textbox nicht erlaubt.
  const ariaFor = (field, combobox) => ({
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
