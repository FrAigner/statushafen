import { describe, it, expect, vi } from 'vitest'
import { ref, nextTick } from 'vue'
import {
  placeholderToken,
  insertText,
  findPlaceholderPrefix,
  filterPlaceholders,
  usePlaceholderInsert,
} from './usePlaceholderInsert'

// Platzhalter einfügen und Autovervollständigung bei `{`.

const list = [
  { name: 'Title' },
  { name: 'Status' },
  { name: 'StatusLabel' },
  { name: 'Message' },
]

describe('placeholderToken', () => {
  it('baut den Go-Template-Platzhalter', () => {
    expect(placeholderToken('Title')).toBe('{{.Title}}')
  })
})

describe('insertText', () => {
  it('fügt in der Mitte ein und setzt den Cursor dahinter', () => {
    expect(insertText('Hallo Welt', 6, 6, '{{.Title}} ')).toEqual({ value: 'Hallo {{.Title}} Welt', cursor: 17 })
  })

  it('ersetzt eine Auswahl', () => {
    expect(insertText('Hallo Welt', 6, 10, '{{.Title}}')).toEqual({ value: 'Hallo {{.Title}}', cursor: 16 })
  })

  it('fügt am Anfang ein', () => {
    expect(insertText('Welt', 0, 0, 'X')).toEqual({ value: 'XWelt', cursor: 1 })
  })

  it('fügt am Ende ein', () => {
    expect(insertText('Welt', 4, 4, 'X')).toEqual({ value: 'WeltX', cursor: 5 })
  })
})

describe('findPlaceholderPrefix', () => {
  it.each([
    ['{', 0, ''],
    ['Text {{', 5, ''],
    ['Text {{.', 5, ''],
    ['Text {{.Sta', 5, 'Sta'],
    ['{.Me', 0, 'Me'],
  ])('erkennt %j', (value, start, query) => {
    expect(findPlaceholderPrefix(value, value.length)).toEqual({ start, query })
  })

  it('wertet nur den Text vor dem Cursor aus', () => {
    expect(findPlaceholderPrefix('{{.Sta rest', 6)).toEqual({ start: 0, query: 'Sta' })
  })

  it.each([
    ['ohne Klammer', 'Hallo', 5],
    ['nach fertigem Platzhalter', '{{.Title}}', 10],
    ['nach Leerzeichen', '{{ ', 3],
    ['Cursor vor der Klammer', '{{.Sta', 0],
  ])('liefert null %s', (_, value, cursor) => {
    expect(findPlaceholderPrefix(value, cursor)).toBeNull()
  })
})

describe('filterPlaceholders', () => {
  it('filtert nach Namensanfang ohne Groß-/Kleinschreibung', () => {
    expect(filterPlaceholders(list, 'sta').map(p => p.name)).toEqual(['Status', 'StatusLabel'])
  })

  it('liefert bei leerem Präfix alle', () => {
    expect(filterPlaceholders(list, '')).toEqual(list)
  })

  it('liefert nichts ohne Treffer', () => {
    expect(filterPlaceholders(list, 'xyz')).toEqual([])
  })
})

// Minimales Feld-Double: value, Auswahl, focus/setSelectionRange.
function fakeElement(value = '', start = value.length, end = start) {
  return {
    value,
    selectionStart: start,
    selectionEnd: end,
    focus: vi.fn(),
    setSelectionRange: vi.fn(function (s, e) { this.selectionStart = s; this.selectionEnd = e }),
  }
}

function setup({ subjectValue = '', bodyValue = '' } = {}) {
  const subject = ref(subjectValue)
  const body = ref(bodyValue)
  const els = { subject: fakeElement(subjectValue), body: fakeElement(bodyValue) }
  const models = { subject, body }
  const api = usePlaceholderInsert({
    placeholders: ref(list),
    fields: {
      subject: { model: subject, element: () => els.subject },
      body: { model: body, element: () => els.body },
    },
    defaultField: 'body',
    idPrefix: 'tpl',
  })
  // Simuliert Tippen: Wert und Cursor setzen, dann input-Event.
  const type = (field, value, cursor = value.length) => {
    els[field].value = value
    models[field].value = value
    els[field].selectionStart = cursor
    els[field].selectionEnd = cursor
    api.onInput(field, { target: els[field] })
  }
  const key = (field, k) => {
    const event = { key: k, preventDefault: vi.fn(), stopPropagation: vi.fn() }
    api.onKeydown(field, event)
    return event
  }
  return { api, subject, body, els, type, key }
}

describe('usePlaceholderInsert', () => {
  it('fügt ohne vorher fokussiertes Feld am Ende des Default-Felds ein', async () => {
    const { api, body, els } = setup({ bodyValue: 'Hallo' })
    els.body.selectionStart = 0
    els.body.selectionEnd = 0

    await api.insertPlaceholder({ name: 'Title' })

    expect(body.value).toBe('Hallo{{.Title}}')
    expect(els.body.focus).toHaveBeenCalled()
    expect(els.body.setSelectionRange).toHaveBeenCalledWith(15, 15)
  })

  it('fügt an der Cursorposition des zuletzt fokussierten Felds ein und ersetzt die Auswahl', async () => {
    const { api, subject, body, els } = setup({ subjectValue: 'A B C', bodyValue: 'x' })
    api.onFocus('subject')
    els.subject.selectionStart = 2
    els.subject.selectionEnd = 3

    await api.insertPlaceholder({ name: 'Status' })

    expect(subject.value).toBe('A {{.Status}} C')
    expect(body.value).toBe('x')
    expect(els.subject.setSelectionRange).toHaveBeenCalledWith(13, 13)
    expect(api.lastField.value).toBe('subject')
  })

  it('öffnet bei `{` die Liste, filtert beim Weitertippen und setzt ARIA-Attribute', () => {
    const { api, type } = setup()
    expect(api.isOpen('subject')).toBe(false)
    expect(api.ariaFor('subject', true)).toEqual({
      role: 'combobox', 'aria-expanded': 'false', 'aria-autocomplete': 'list',
      'aria-controls': 'tpl-subject-listbox', 'aria-activedescendant': undefined,
    })

    type('subject', 'X {')
    expect(api.isOpen('subject')).toBe(true)
    expect(api.isOpen('body')).toBe(false)
    expect(api.suggestions.value).toHaveLength(4)

    type('subject', 'X {{.Sta')
    expect(api.suggestions.value.map(p => p.name)).toEqual(['Status', 'StatusLabel'])
    expect(api.ariaFor('subject', true)).toMatchObject({ 'aria-expanded': 'true', 'aria-activedescendant': 'tpl-subject-option-0' })
    expect(api.ariaFor('body', false)).toEqual({
      role: undefined, 'aria-expanded': undefined, 'aria-autocomplete': 'list',
      'aria-controls': 'tpl-body-listbox', 'aria-activedescendant': undefined,
    })
    expect(api.optionId('body', 2)).toBe('tpl-body-option-2')
    expect(api.listboxId('body')).toBe('tpl-body-listbox')
  })

  it('schließt die Liste ohne Treffer, ohne Präfix und bei markiertem Text', () => {
    const { api, type, els } = setup()
    type('body', '{{.xyz')
    expect(api.isOpen('body')).toBe(false)

    type('body', '{')
    expect(api.isOpen('body')).toBe(true)
    type('body', 'abc')
    expect(api.isOpen('body')).toBe(false)

    type('body', '{{')
    els.body.selectionStart = 0
    api.onInput('body', { target: els.body })
    expect(api.isOpen('body')).toBe(false)
  })

  it('ignoriert Events fremder Elemente', () => {
    const { api } = setup()
    api.onInput('body', { target: fakeElement('{') })
    expect(api.isOpen('body')).toBe(false)
  })

  it('wechselt mit Pfeiltasten den aktiven Eintrag (mit Umlauf) und behält ihn bei gleichem Präfix', () => {
    const { api, type, key, els } = setup()
    type('body', '{{.')
    expect(api.activeIndex.value).toBe(0)

    expect(key('body', 'ArrowUp').preventDefault).toHaveBeenCalled()
    expect(api.activeIndex.value).toBe(3)
    key('body', 'ArrowDown')
    expect(api.activeIndex.value).toBe(0)
    key('body', 'ArrowDown')
    expect(api.activeIndex.value).toBe(1)

    // keyup/Klick ohne Cursorwechsel lässt die Auswahl stehen.
    api.onCaretMove('body', { target: els.body })
    expect(api.activeIndex.value).toBe(1)

    // Neues Präfix setzt den aktiven Eintrag zurück.
    type('body', '{{.S')
    expect(api.activeIndex.value).toBe(0)
  })

  it('schließt die Liste, wenn der Cursor das Präfix verlässt; geschlossen bleibt onCaretMove wirkungslos', () => {
    const { api, type, els } = setup()
    type('body', '{{ x')
    api.onCaretMove('body', { target: els.body })
    expect(api.isOpen('body')).toBe(false)

    type('body', 'a {{')
    els.body.selectionStart = 1
    els.body.selectionEnd = 1
    api.onCaretMove('body', { target: els.body })
    expect(api.isOpen('body')).toBe(false)
  })

  it.each(['Enter', 'Tab'])('übernimmt mit %s den aktiven Eintrag und ersetzt das Präfix', async (k) => {
    const { api, body, els, type, key } = setup()
    type('body', 'Hallo {{.Sta Welt', 12)
    key('body', 'ArrowDown')

    const event = key('body', k)
    await nextTick()
    await nextTick()

    expect(event.preventDefault).toHaveBeenCalled()
    expect(body.value).toBe('Hallo {{.StatusLabel}} Welt')
    expect(els.body.setSelectionRange).toHaveBeenCalledWith(22, 22)
    expect(api.isOpen('body')).toBe(false)
  })

  it('übernimmt per accept (Klick) und schließt mit Escape', async () => {
    const { api, subject, type, key } = setup()
    type('subject', '{')
    await api.accept('subject', { name: 'Message' })
    expect(subject.value).toBe('{{.Message}}')

    type('subject', '{{.Message}} {')
    const esc = key('subject', 'Escape')
    expect(esc.stopPropagation).toHaveBeenCalled()
    expect(api.isOpen('subject')).toBe(false)
  })

  it('lässt andere Tasten und Tasten bei geschlossener Liste durch', () => {
    const { type, key } = setup()
    expect(key('body', 'Enter').preventDefault).not.toHaveBeenCalled()
    type('body', '{')
    expect(key('body', 'a').preventDefault).not.toHaveBeenCalled()
    expect(key('subject', 'ArrowDown').preventDefault).not.toHaveBeenCalled()
  })

  it('close schließt die Liste', () => {
    const { api, type } = setup()
    type('body', '{')
    api.close()
    expect(api.isOpen('body')).toBe(false)
  })
})
