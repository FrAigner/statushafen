// jsdom statt happy-dom: Die Vorschau bereinigt mit DOMPurify und braucht ein vollständiges DOM.
// @vitest-environment jsdom
import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { nextTick } from 'vue'
import i18n from '../i18n'
import RichTextEditor from './RichTextEditor.vue'
import { axe } from '../test-support/axe.js'

enableAutoUnmount(afterEach)
beforeEach(() => { i18n.global.locale.value = 'de' })

// v-model verdrahtet: Der Wert läuft in die Props zurück wie in den Formularen.
const make = (props = {}, attrs = {}) => {
  const w = mount(RichTextEditor, {
    props: { modelValue: '', ...props, 'onUpdate:modelValue': (v) => w.setProps({ modelValue: v }) },
    attrs,
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
  return w
}

// Prüft die Vorschau auf alles, was nie erscheinen darf.
const unsafe = (root) => root.querySelectorAll('script, img, iframe, svg, style, h1, h2, h3, form, input, [onerror], [onclick], [style]').length
  + [...root.querySelectorAll('a')].filter((a) => !/^https:/.test(a.getAttribute('href') ?? '')).length

async function select(w, start, end) {
  const el = w.find('textarea').element
  el.setSelectionRange(start, end)
}

describe('RichTextEditor', () => {
  it('reicht Attribute an das textarea und meldet Eingaben per v-model', async () => {
    const w = make({}, { id: 'msg', placeholder: 'Text', required: '' })
    const ta = w.find('textarea')
    expect(ta.attributes('id')).toBe('msg')
    expect(ta.attributes('placeholder')).toBe('Text')
    expect(ta.attributes('required')).toBeDefined()
    await ta.setValue('Hallo')
    expect(w.emitted('update:modelValue')[0]).toEqual(['Hallo'])
  })

  it('ein @input des Aufrufers (Fehler zurücksetzen) feuert weiterhin', async () => {
    let n = 0
    const w = make({}, { onInput: () => { n += 1 } })
    await w.find('textarea').setValue('x')
    expect(n).toBe(1)
  })

  it('Werkzeugleiste: Knöpfe mit Namen, Fett setzt Markdown um die Auswahl', async () => {
    const w = make({ modelValue: 'ab cd' })
    const bar = w.find('[role="toolbar"]')
    expect(bar.attributes('aria-label')).toBe('Formatierung')
    expect(bar.findAll('button').map((b) => b.attributes('aria-label'))).toEqual([
      'Fett (Strg+B)', 'Kursiv (Strg+I)', 'Aufzählung', 'Zitat', 'Code', 'Link (nur https)',
    ])
    await select(w, 3, 5)
    await w.find('.rte__btn--bold').trigger('click')
    expect(w.emitted('update:modelValue').at(-1)).toEqual(['ab **cd**'])
  })

  it.each([
    ['italic', 'ab', '*ab*'],
    ['code', 'ab', '`ab`'],
    ['list', 'ab', '- ab'],
    ['quote', 'ab', '> ab'],
    ['link', 'ab', '[ab](https://)'],
  ])('Werkzeug %s setzt die Syntax', async (kind, value, expected) => {
    const w = make({ modelValue: value })
    await select(w, 0, value.length)
    await w.find(`.rte__btn--${kind}`).trigger('click')
    expect(w.emitted('update:modelValue').at(-1)).toEqual([expected])
  })

  it('Tastenkürzel Strg+B und Cmd+I formatieren, andere Tasten nicht', async () => {
    const w = make({ modelValue: 'ab' })
    await select(w, 0, 2)
    await w.find('textarea').trigger('keydown', { key: 'b', ctrlKey: true })
    expect(w.emitted('update:modelValue').at(-1)).toEqual(['**ab**'])
    await w.find('textarea').trigger('keydown', { key: 'i', metaKey: true })
    expect(w.emitted('update:modelValue').at(-1)).toEqual(['***ab***'])
    const n = w.emitted('update:modelValue').length
    await w.find('textarea').trigger('keydown', { key: 'x', ctrlKey: true })
    await w.find('textarea').trigger('keydown', { key: 'b' })
    await w.find('textarea').trigger('keydown', { key: 'b', ctrlKey: true, altKey: true })
    expect(w.emitted('update:modelValue').length).toBe(n)
  })

  it('nach dem Formatieren bekommt das Feld Fokus und die Auswahl', async () => {
    const w = make({ modelValue: 'ab' })
    await select(w, 0, 2)
    await w.find('.rte__btn--bold').trigger('click')
    await nextTick()
    const el = w.find('textarea').element
    expect(document.activeElement).toBe(el)
    expect([el.selectionStart, el.selectionEnd]).toEqual([2, 4])
  })

  it('schreibgeschützt: Knöpfe gesperrt, Tastenkürzel wirkungslos', async () => {
    const w = make({ modelValue: 'ab', readonly: true })
    expect(w.findAll('[role="toolbar"] button').every((b) => b.attributes('disabled') !== undefined)).toBe(true)
    expect(w.find('textarea').attributes('readonly')).toBeDefined()
    await select(w, 0, 2)
    await w.find('textarea').trigger('keydown', { key: 'b', ctrlKey: true })
    expect(w.emitted('update:modelValue')).toBeUndefined()
  })

  it('Zeichenzähler zählt Zeichen (nicht UTF-16-Einheiten) gegen 5.000', () => {
    const w = make({ modelValue: 'a😀b' })
    expect(w.find('.rte__counter').text()).toBe('3 / 5.000 Zeichen')
    expect(w.find('.rte__counter').attributes('role')).toBeUndefined()
    expect(w.find('textarea').attributes('aria-invalid')).toBeUndefined()
    expect(w.find('textarea').attributes('maxlength')).toBeUndefined()
  })

  it('über dem Limit: Warnung in höflicher Live-Region, nichts wird gekürzt', () => {
    const w = make({ modelValue: 'x'.repeat(5001) })
    expect(w.find('.rte__counter').classes()).toContain('rte__counter--over')
    // Review: role="alert" am Zähler meldete jeden Tastendruck.
    expect(w.find('[role="alert"]').exists()).toBe(false)
    expect(w.find('.rte__counter').attributes('role')).toBeUndefined()
    const live = w.find('.rte__warn')
    expect(live.attributes('aria-live')).toBe('polite')
    expect(live.text()).toBe('Zu lang, höchstens 5.000 Zeichen sind erlaubt.')
    expect(w.find('textarea').element.value).toHaveLength(5001)
  })

  it('Live-Region ist unterhalb des Limits leer und ändert sich beim Weitertippen über dem Limit nicht', async () => {
    const w = make({ modelValue: 'x' })
    expect(w.find('.rte__warn').text()).toBe('')
    await w.setProps({ modelValue: 'x'.repeat(5001) })
    const first = w.find('.rte__warn').text()
    await w.setProps({ modelValue: 'x'.repeat(5050) })
    expect(w.find('.rte__warn').text()).toBe(first)
  })

  // Ein zu langer Text allein markiert das Feld nicht als fehlerhaft: focusInvalid wählt sonst
  // ein früheres, nur zu langes Feld statt des Pflichtfeld-Fehlers.
  it('zu langer Text setzt kein aria-invalid, der Fehler des Formulars schon', () => {
    expect(make({ modelValue: 'x'.repeat(5001) }).find('textarea').attributes('aria-invalid')).toBeUndefined()
    expect(make({ modelValue: 'x' }, { 'aria-invalid': true }).find('textarea').attributes('aria-invalid')).toBe('true')
  })

  // Regression: Zahlen folgten der Locale des Prozesses/Browsers statt der UI-Sprache.
  it.each([['de', '5.000'], ['en', '5,000']])('Zähler und Warnung formatieren nach UI-Sprache (%s)', (loc, max) => {
    i18n.global.locale.value = loc
    const w = make({ modelValue: 'x'.repeat(5001) })
    expect(w.find('.rte__counter').text()).toContain(`/ ${max}`)
    expect(w.find('.rte__warn').text()).toContain(max)
    i18n.global.locale.value = 'de'
  })

  it('Vorschau → Fehler des Formulars schaltet zurück in den Bearbeiten-Modus, das Feld ist fokussierbar', async () => {
    const w = make({ modelValue: 'x' })
    await w.find('.rte__btn--preview').trigger('click')
    expect(w.find('textarea').attributes('hidden')).toBeDefined()
    await w.setProps({ ariaInvalid: true })
    expect(w.find('textarea').attributes('hidden')).toBeUndefined()
    expect(w.find('.rte__btn--preview').attributes('aria-pressed')).toBe('false')
    w.find('textarea[aria-invalid="true"]').element.focus()
    expect(document.activeElement).toBe(w.find('textarea').element)
  })

  it('Tastenkürzel mit Shift (Strg+Shift+B) werden nicht abgefangen', async () => {
    const w = make({ modelValue: 'ab' })
    await select(w, 0, 2)
    await w.find('textarea').trigger('keydown', { key: 'B', ctrlKey: true, shiftKey: true })
    expect(w.emitted('update:modelValue')).toBeUndefined()
  })

  it('Tooltip nennt auf dem Mac die Befehlstaste statt „Strg“', async () => {
    const orig = Object.getOwnPropertyDescriptor(Navigator.prototype, 'platform')
    Object.defineProperty(Navigator.prototype, 'platform', { value: 'MacIntel', configurable: true })
    const w = mount(RichTextEditor, { global: { plugins: [i18n] } })
    expect(w.find('.rte__btn--bold').attributes('aria-label')).toBe('Fett (⌘+B)')
    Object.defineProperty(Navigator.prototype, 'platform', orig)
    expect(make().find('.rte__btn--bold').attributes('aria-label')).toBe('Fett (Strg+B)')
  })

  // Regression CRLF: selectionStart zählt im normalisierten Feldwert (\n), der Modellwert hatte \r\n.
  it('Altdaten mit \\r\\n: Auswahl und Text stammen vom selben String', async () => {
    const w = make({ modelValue: 'a\r\nb' })
    const el = w.find('textarea').element
    expect(el.value).toBe('a\nb')
    el.setSelectionRange(2, 3)
    await w.find('.rte__btn--bold').trigger('click')
    expect(w.emitted('update:modelValue').at(-1)).toEqual(['a\n**b**'])
  })

  // Undo: execCommand('insertText') behält die Historie des Feldes; ohne Unterstützung gilt der Fallback.
  it('formatiert über execCommand("insertText") mit minimalem Austausch, wenn verfügbar', async () => {
    const w = make({ modelValue: 'ab cd' })
    const el = w.find('textarea').element
    const calls = []
    document.execCommand = vi.fn((cmd, _ui, text) => {
      calls.push([cmd, text, el.selectionStart, el.selectionEnd])
      el.setRangeText(text, el.selectionStart, el.selectionEnd, 'end')
      el.dispatchEvent(new Event('input', { bubbles: true }))
      return true
    })
    el.setSelectionRange(3, 5)
    await w.find('.rte__btn--bold').trigger('click')
    delete document.execCommand
    expect(calls).toEqual([['insertText', '**cd**', 3, 5]])
    expect(w.emitted('update:modelValue').at(-1)).toEqual(['ab **cd**'])
  })

  it('genau 5.000 Zeichen sind erlaubt', () => {
    const w = make({ modelValue: 'x'.repeat(5000) })
    expect(w.find('.rte__counter').classes()).not.toContain('rte__counter--over')
    expect(w.find('.rte__warn').text()).toBe('')
  })

  it('verbindet Zähler und Fehler des Formulars per aria-describedby; aria-invalid des Aufrufers bleibt', () => {
    const w = make({ modelValue: 'x' }, { 'aria-describedby': 'fehler-1', 'aria-invalid': 'true' })
    const ta = w.find('textarea')
    const ids = ta.attributes('aria-describedby').split(' ')
    expect(ids[0]).toBe('fehler-1')
    expect(ids[1]).toBe(w.find('.rte__counter').attributes('id'))
    expect(ta.attributes('aria-invalid')).toBe('true')
  })

  it('Vorschau schaltet um, zeigt Formatierung und sperrt die Werkzeuge', async () => {
    const w = make({ modelValue: '**fett**\n- a\n- b' })
    const toggle = w.find('.rte__btn--preview')
    expect(toggle.attributes('aria-pressed')).toBe('false')
    expect(w.find('.rte__preview').isVisible()).toBe(false)
    await toggle.trigger('click')
    expect(toggle.attributes('aria-pressed')).toBe('true')
    expect(toggle.text()).toBe('Bearbeiten')
    expect(w.find('.rte__preview').isVisible()).toBe(true)
    expect(w.find('.rte__preview strong').text()).toBe('fett')
    expect(w.findAll('.rte__preview li')).toHaveLength(2)
    expect(w.find('textarea').isVisible()).toBe(false)
    expect(w.findAll('[role="toolbar"] button').every((b) => b.attributes('disabled') !== undefined)).toBe(true)
    await toggle.trigger('click')
    expect(w.find('textarea').isVisible()).toBe(true)
  })

  // Regression: v-show setzte style="display: none" und riss den Inline-Style-Schutz der Views.
  it('setzt nirgends ein style-Attribut, auch nicht beim Umschalten', async () => {
    const w = make({ modelValue: 'x' })
    expect(w.findAll('[style]')).toHaveLength(0)
    await w.find('.rte__btn--preview').trigger('click')
    expect(w.findAll('[style]')).toHaveLength(0)
    expect(w.find('textarea').attributes('hidden')).toBeDefined()
  })

  // Formulare liefern für ein leeres Feld teils null (API-Antwort ohne Wert).
  it('kommt mit null als Wert zurecht (Zähler 0, Werkzeug fügt den Platzhalter ein)', async () => {
    const w = make({ modelValue: null })
    expect(w.find('.rte__counter').text()).toBe('0 / 5.000 Zeichen')
    await w.find('.rte__btn--bold').trigger('click')
    expect(w.emitted('update:modelValue').at(-1)).toEqual(['**fett**'])
  })

  it('Vorschau ohne Text zeigt einen Hinweis', async () => {
    const w = make()
    await w.find('.rte__btn--preview').trigger('click')
    expect(w.find('.rte__preview').text()).toBe('Noch kein Text.')
  })

  it('ist barrierefrei (axe)', async () => {
    const w = make({ modelValue: 'x' }, { id: 'a1', 'aria-label': 'Nachricht' })
    expect(await axe(w.element)).toEqual([])
  })
})

// XSS-Matrix der Vorschau (Anzeige wie Statusseite: utils/richText.js).
const ATTACKS = {
  script: '<script>alert(1)</script>',
  img: '<img src=x onerror=alert(1)>',
  'javascript-Link': '[klick](javascript:alert(1))',
  'http-Link': '[klick](http://example.com)',
  'data-Link': '[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)',
  Bild: '![x](https://example.com/a.png)',
  Überschrift: '# Titel',
  iframe: '<iframe src="https://example.com"></iframe>',
  Style: '<p style="color:red" onclick="alert(1)">x</p>',
  svg: '<svg onload=alert(1)></svg>',
}

describe('RichTextEditor: Vorschau maskiert Roh-HTML, Bilder, Überschriften', () => {
  for (const [name, input] of Object.entries(ATTACKS)) {
    it(name, async () => {
      const w = make({ modelValue: input })
      await w.find('.rte__btn--preview').trigger('click')
      expect(unsafe(w.find('.rte__preview').element), w.find('.rte__preview').html()).toBe(0)
    })
  }

  it('erlaubter https-Link bleibt ein Link', async () => {
    const w = make({ modelValue: '[ok](https://example.com)' })
    await w.find('.rte__btn--preview').trigger('click')
    const a = w.find('.rte__preview a')
    expect(a.attributes('href')).toBe('https://example.com')
    expect(unsafe(w.find('.rte__preview').element)).toBe(0)
  })

  // Gegenprobe: Der Prüfer muss anschlagen, wenn die Eingabe ungefiltert als HTML
  // durchgereicht würde; sonst könnte die Matrix leer laufen.
  it('Gegenprobe: ungefiltertes Durchreichen wird vom Prüfer erkannt', () => {
    const hits = Object.entries(ATTACKS).filter(([, input]) => {
      const div = document.createElement('div')
      div.innerHTML = input
      return unsafe(div) > 0
    })
    expect(hits.length).toBeGreaterThanOrEqual(4)
  })
})
