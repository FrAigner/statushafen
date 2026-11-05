// jsdom: renderRichText bereinigt mit DOMPurify.
// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { renderRichText } from './richText'
import { applyFormat, minimalReplace } from './richTextEditing'

// Reine Werkzeugleisten-Funktionen.
describe('applyFormat', () => {
  it('fett und kursiv umschließen die Auswahl und behalten sie markiert', () => {
    expect(applyFormat('ab cd', 3, 5, 'bold')).toEqual({ value: 'ab **cd**', start: 5, end: 7 })
    expect(applyFormat('ab cd', 0, 2, 'italic')).toEqual({ value: '*ab* cd', start: 1, end: 3 })
    expect(applyFormat('ab cd', 0, 2, 'code')).toEqual({ value: '`ab` cd', start: 1, end: 3 })
  })

  it('ohne Auswahl wird der Platzhalter eingefügt und markiert', () => {
    expect(applyFormat('x', 1, 1, 'bold', 'fett')).toEqual({ value: 'x**fett**', start: 3, end: 7 })
  })

  it('zweiter Klick nimmt fett wieder zurück', () => {
    const first = applyFormat('ab', 0, 2, 'bold')
    const second = applyFormat(first.value, first.start, first.end, 'bold')
    expect(second).toEqual({ value: 'ab', start: 0, end: 2 })
  })

  it('umschließende Zeichen nur auf einer Seite werden nicht entfernt', () => {
    expect(applyFormat('**ab', 2, 4, 'bold').value).toBe('****ab**')
    expect(applyFormat('ab**', 0, 2, 'bold').value).toBe('**ab****')
  })

  it('Liste und Zitat setzen das Präfix an jede Zeile der Auswahl', () => {
    expect(applyFormat('a\nb\nc', 0, 3, 'list')).toEqual({ value: '- a\n- b\nc', start: 0, end: 7 })
    expect(applyFormat('a\nb', 2, 2, 'quote').value).toBe('a\n> b')
    expect(applyFormat('a', 1, 1, 'list').value).toBe('- a')
  })

  it('Liste schaltet zurück, wenn alle Zeilen schon das Präfix tragen', () => {
    expect(applyFormat('- a\n- b', 0, 7, 'list').value).toBe('a\nb')
    // gemischt: es wird ergänzt, nicht entfernt
    expect(applyFormat('- a\nb', 0, 5, 'list').value).toBe('- - a\n- b')
  })

  // Review: Kursiv auf fettem Text nahm die Fettung weg, Fett auf `***ab***` ergab `*ab*`.
  describe('fett und kursiv kombiniert', () => {
    const sel = (text, kind) => {
      const i = text.indexOf('ab')
      return applyFormat(text, i, i + 2, kind)
    }
    it('Kursiv auf fett ergibt fett-kursiv, nochmal Kursiv wieder fett', () => {
      const a = sel('**ab**', 'italic')
      expect(a.value).toBe('***ab***')
      expect(sel(a.value, 'italic').value).toBe('**ab**')
    })
    it('Fett auf kursiv ergibt fett-kursiv, nochmal Fett wieder kursiv', () => {
      const a = sel('*ab*', 'bold')
      expect(a.value).toBe('***ab***')
      expect(sel(a.value, 'bold').value).toBe('*ab*')
    })
    it('Auswahl bleibt auf dem Text', () => {
      const r = sel('**ab**', 'italic')
      expect(r.value.slice(r.start, r.end)).toBe('ab')
      const u = sel('***ab***', 'bold')
      expect(u.value.slice(u.start, u.end)).toBe('ab')
    })
  })

  describe('Randfälle', () => {
    it('Liste: Auswahl samt Zeilenumbruch (Dreifachklick) nimmt die Folgezeile nicht mit', () => {
      expect(applyFormat('a\nb', 0, 2, 'list').value).toBe('- a\nb')
      expect(applyFormat('a\nb', 0, 2, 'quote').value).toBe('> a\nb')
    })
    it('Liste: Leerzeilen bleiben leer', () => {
      expect(applyFormat('a\n\nb', 0, 4, 'list').value).toBe('- a\n\n- b')
      expect(applyFormat('- a\n\n- b', 0, 8, 'list').value).toBe('a\n\nb')
    })
    it('Liste: eine einzelne leere Zeile (Cursor) bekommt das Präfix', () => {
      expect(applyFormat('', 0, 0, 'list').value).toBe('- ')
    })
    it('Fett: Leerraum am Rand der Auswahl bleibt außerhalb', () => {
      expect(applyFormat('ab cd', 0, 3, 'bold').value).toBe('**ab** cd')
      expect(applyFormat('x  ab', 1, 5, 'italic').value).toBe('x  *ab*')
    })
    it('Fett: nur Leerraum markiert wird wie eine leere Auswahl behandelt', () => {
      expect(applyFormat('a b', 1, 2, 'bold', 'fett').value).toBe('a **fett**b')
    })
    it('Fett über mehrere Absätze umschließt jeden Absatz einzeln', () => {
      const r = applyFormat('a\n\nb', 0, 4, 'bold')
      expect(r.value).toBe('**a**\n\n**b**')
    })
    it('Absätze: ein zweiter Klick nimmt alle wieder zurück', () => {
      const first = applyFormat('a\n\nb', 0, 4, 'bold')
      expect(applyFormat(first.value, first.start, first.end, 'bold').value).toBe('a\n\nb')
    })
    it('Link: `]` und `[` im Linktext werden maskiert und der Link bleibt ganz', () => {
      const r = applyFormat('a]b', 0, 3, 'link')
      expect(r.value).toBe('[a\\]b](https://)')
    })
    it('Code: Backtick im Text bekommt einen längeren Zaun', () => {
      expect(applyFormat('a`b', 0, 3, 'code').value).toBe('`` a`b ``')
      expect(applyFormat('a``b', 0, 4, 'code').value).toBe('``` a``b ```')
    })
  })

  // Die erzeugte Syntax muss vom Renderer auch als Link bzw. Code verstanden werden.
  it('maskierter Link und Backtick-Zaun rendern als Link bzw. Code', () => {
    const link = applyFormat('a]b', 0, 3, 'link').value.replace('https://', 'https://x.de')
    expect(renderRichText(link)).toContain('>a]b</a>')
    const code = applyFormat('a`b', 0, 3, 'code').value
    expect(renderRichText(code)).toContain('<code>a`b</code>')
  })

  it('minimalReplace liefert den kleinsten Austausch', () => {
    expect(minimalReplace('ab cd', 'ab **cd**')).toEqual({ start: 3, end: 5, text: '**cd**' })
    expect(minimalReplace('x', 'x')).toEqual({ start: 1, end: 1, text: '' })
    expect(minimalReplace('aXb', 'ab')).toEqual({ start: 1, end: 2, text: '' })
  })

  it('Link nutzt die Auswahl als Text und markiert die Adresse', () => {
    const r = applyFormat('Siehe Seite', 6, 11, 'link', 'Linktext')
    expect(r.value).toBe('Siehe [Seite](https://)')
    expect(r.value.slice(r.start, r.end)).toBe('https://')
  })

  it('Link ohne Auswahl verwendet den Platzhaltertext', () => {
    const r = applyFormat('', 0, 0, 'link', 'Linktext')
    expect(r.value).toBe('[Linktext](https://)')
    expect(r.value.slice(r.start, r.end)).toBe('https://')
  })
})
