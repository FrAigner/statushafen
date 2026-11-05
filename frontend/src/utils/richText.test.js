// jsdom statt happy-dom: DOMPurify braucht ein vollständiges DOM (siehe docs.test.js).
// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { Marked } from 'marked'
import DOMPurify from 'dompurify'
import richTextSource from './richText.js?raw'
import parityFixture from '../../../backend/richtext/testdata/parity.json'
import {
  renderRichText, markdownToPlainText, isHttpsUrl, plainTextToHtml, RICH_TEXT_MAX_LENGTH,
  RICH_TEXT_MAX_RENDER_LENGTH, RICH_TEXT_MAX_BLOCK_LENGTH, RICH_TEXT_CACHE_MAX_KEY_LENGTH, memoize,
  resetRichTextBudget,
} from './richText'

// Das Render-Budget (richTextBudget.test.js) gilt je Seitenaufruf; jeder Test
// beginnt mit vollem Budget, damit die Reihenfolge der Fälle keine Rolle spielt.
beforeEach(() => resetRichTextBudget())

// XSS-Matrix und Formatierung der Anzeige formatierter Texte.

const ALLOWED = new Set(['P', 'BR', 'STRONG', 'EM', 'UL', 'OL', 'LI', 'A', 'CODE', 'BLOCKQUOTE'])

// Prüft ein Renderergebnis unabhängig vom Renderer: nur erlaubte Tags, nur der
// Link trägt Attribute (href https, target, rel), keine Zeilenumbrüche.
function unsafeFindings(html) {
  const problems = []
  if (/[\r\n]/.test(html)) problems.push('Zeilenumbruch im Ergebnis')
  const doc = new DOMParser().parseFromString(`<body>${html}</body>`, 'text/html')
  for (const el of doc.body.querySelectorAll('*')) {
    if (!ALLOWED.has(el.tagName)) problems.push(`Tag ${el.tagName}`)
    for (const attr of el.attributes) {
      // <ol start="N"> nur mit reiner Zahl.
      if (el.tagName === 'OL' && attr.name === 'start' && /^\d{1,9}$/.test(attr.value)) continue
      const ok = el.tagName === 'A' && (
        (attr.name === 'href' && /^https:\/\//.test(attr.value))
        || (attr.name === 'target' && attr.value === '_blank')
        || (attr.name === 'rel' && attr.value === 'noopener noreferrer nofollow'))
      if (!ok) problems.push(`Attribut ${el.tagName}@${attr.name}=${attr.value}`)
    }
  }
  return problems
}

const XSS = {
  script: '<script>alert(1)</script>',
  'script im Text': 'Hallo <script>alert(1)</script> Welt',
  'javascript-Link': '[klick](javascript:alert(1))',
  'javascript Großschreibung': '[klick](JaVaScRiPt:alert(1))',
  'javascript Leerraum': '[klick]( \tjavascript:alert(1) )',
  'javascript Zeilenumbruch': '[klick](java\nscript:alert(1))',
  'javascript Tab': '[klick](java\tscript:alert(1))',
  'javascript Entität': '[klick](&#106;avascript:alert(1))',
  'javascript Autolink': '<javascript:alert(1)>',
  'data-URI': '[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)',
  vbscript: '[x](vbscript:msgbox(1))',
  'http-Link': '[x](http://example.com/a)',
  'http nackt': 'http://example.com/a',
  ftp: '[x](ftp://example.com)',
  protokollrelativ: '[x](//evil.example/a)',
  Zugangsdaten: '[x](https://trusted.example@evil.example/)',
  'onerror img': '<img src=x onerror=alert(1)>',
  'img src': '<img src="https://evil.example/pixel.png">',
  'Markdown-Bild': '![bild](https://evil.example/pixel.png)',
  'Bild javascript': '![x](javascript:alert(1))',
  onclick: '<a href="https://x.de" onclick="alert(1)">x</a>',
  onmouseover: '<div onmouseover="alert(1)">hover</div>',
  iframe: '<iframe src="https://evil.example"></iframe>',
  'svg onload': '<svg onload=alert(1)>',
  'style-Block': '<style>body{background:url(https://evil.example)}</style>',
  Entitäten: '&lt;script&gt;alert(1)&lt;/script&gt; &#60;script&#62;',
  verschachtelt: '<<script>script>alert(1)<</script>/script>',
  'verschachtelt a': '<a href="x"><a href="javascript:alert(1)">x</a></a>',
  'Titel-Ausbruch': '[x](https://example.com "t\\" onmouseover=\\"alert(1)")',
  'Attribut-Ausbruch': '[x](https://example.com/"onmouseover="alert(1))',
  'Link-Text HTML': '[<script>alert(1)</script>](https://example.com)',
  'Fett mit HTML': '**<img src=x onerror=alert(1)>**',
  'Code mit HTML': '`<script>alert(1)</script>`',
  Codeblock: '```\n<script>alert(1)</script>\n```',
  Zitat: '> <script>alert(1)</script>',
  Liste: '- <script>alert(1)</script>\n- [x](javascript:alert(1))',
  'Überschrift': '# <script>alert(1)</script>',
  Tabelle: '| a | b |\n|---|---|\n| <script> | x |',
  'Autolink mit Quote': 'https://example.com/"><script>alert(1)</script>',
  'mXSS noscript': '<noscript><p title="</noscript><img src=x onerror=alert(1)>">',
  'math/svg Namensraum': '<math><mtext><table><mglyph><style><img src=x onerror=alert(1)>',
}

describe('renderRichText: XSS-Matrix', () => {
  for (const [name, input] of Object.entries(XSS)) {
    it(name, () => {
      const out = renderRichText(input)
      expect(unsafeFindings(out), `${JSON.stringify(input)} -> ${out}`).toEqual([])
      expect(out.toLowerCase()).not.toContain('<script')
      expect(out.toLowerCase()).not.toContain('<img')
    })
  }
})

// Gegenprobe: Der Prüfer muss bei einem Renderer ohne Whitelist anschlagen, sonst
// könnte die Matrix leer laufen. Drei „alte Stände“: marked allein, marked + das
// weite Profil aus docs/index.js und ungefiltertes HTML.
describe('Gegenprobe: Renderer ohne enge Whitelist fällt durch', () => {
  const plainMarked = new Marked({ gfm: true, breaks: true })
  const naive = [
    ['marked ohne Sanitizer', (md) => plainMarked.parse(md)],
    ['marked + USE_PROFILES html (weit)', (md) => DOMPurify.sanitize(plainMarked.parse(md), { USE_PROFILES: { html: true } })],
    ['Eingabe ungefiltert', (md) => md],
  ]
  for (const [label, render] of naive) {
    it(`${label}: mindestens ein Fall der Matrix wird erkannt`, () => {
      const failing = Object.entries(XSS).filter(([, input]) => unsafeFindings(render(input)).length > 0)
      expect(failing.length, label).toBeGreaterThan(5)
    })
  }
  it('das weite Profil lässt <img> und Überschriften durch, die enge Whitelist nicht', () => {
    const wide = DOMPurify.sanitize(plainMarked.parse('# H\n\n<img src="https://x.de/a.png">'), { USE_PROFILES: { html: true } })
    expect(unsafeFindings(wide)).not.toEqual([])
    expect(unsafeFindings(renderRichText('# H\n\n<img src="https://x.de/a.png">'))).toEqual([])
  })
})

describe('renderRichText: Formatierung', () => {
  it('fett, kursiv, Absätze und Zeilenumbrüche', () => {
    const out = renderRichText('**fett** und *kursiv*\nzweite Zeile\n\nNeuer Absatz')
    expect(out).toBe('<p><strong>fett</strong> und <em>kursiv</em><br>zweite Zeile</p><p>Neuer Absatz</p>')
  })

  it('Listen und Zitate', () => {
    expect(renderRichText('- a\n- b')).toBe('<ul><li>a</li><li>b</li></ul>')
    expect(renderRichText('1. a\n2. b')).toBe('<ol><li>a</li><li>b</li></ol>')
    expect(renderRichText('> zitat')).toBe('<blockquote><p>zitat</p></blockquote>')
  })

  it('https-Link öffnet im neuen Tab mit rel noopener noreferrer nofollow', () => {
    const out = renderRichText('[Status](https://status.example.com/a?x=1&y=2) und https://example.org/pfad.')
    const doc = new DOMParser().parseFromString(out, 'text/html')
    const links = [...doc.querySelectorAll('a')]
    expect(links.map(a => a.getAttribute('href'))).toEqual(['https://status.example.com/a?x=1&y=2', 'https://example.org/pfad'])
    for (const a of links) {
      expect(a.getAttribute('target')).toBe('_blank')
      expect(a.getAttribute('rel')).toBe('noopener noreferrer nofollow')
    }
  })

  it('Link mit anderem Schema wird Text mit sichtbarem Ziel', () => {
    const out = renderRichText('[x](http://example.com) [y](javascript:alert(1))')
    expect(out).not.toContain('<a')
    expect(out).toContain('x (http://example.com)')
    expect(out).toContain('y (javascript:alert(1))')
  })

  it('automatisch erkannte Adressen ohne https zeigen nur den Text', () => {
    const out = renderRichText('www.beispiel.de und a@b.de')
    expect(out).not.toContain('<a')
    expect(out).toContain('www.beispiel.de und a@b.de')
  })

  it('Rest der unterstützten Randfälle bleibt sichtbarer Text', () => {
    expect(renderRichText('~~weg~~')).toBe('<p>~~weg~~</p>')
    expect(renderRichText('- [ ] offen\n- [x] fertig')).toBe('<ul><li>[ ] offen</li><li>[x] fertig</li></ul>')
    expect(renderRichText('```\nzeile1\nzeile2\n```')).toBe('<p><code>zeile1<br>zeile2</code></p>')
    expect(renderRichText('<b>roh</b>')).toBe('<p>&lt;b&gt;roh&lt;/b&gt;</p>')
    expect(renderRichText('a <span>x</span> b')).toBe('<p>a &lt;span&gt;x&lt;/span&gt; b</p>')
  })

  it('leere Eingaben', () => {
    expect(renderRichText('')).toBe('')
    expect(renderRichText('  \n ')).toBe('')
    expect(renderRichText(null)).toBe('')
    expect(renderRichText(undefined)).toBe('')
  })
})

// Altdaten: reiner Text bleibt lesbar, einfache Zeilenumbrüche bleiben erhalten.
describe('renderRichText: Altdaten', () => {
  const cases = [
    ['Wir untersuchen das Problem.\nBitte um Geduld.', 'Wir untersuchen das Problem.<br>Bitte um Geduld.'],
    ['snake_case_name und 2 * 3 = 6', 'snake_case_name und 2 * 3 = 6'],
    ['# Kein Titel', '<p># Kein Titel</p>'],
    ['## Zwei\nZeile', '<p>## Zwei<br>Zeile</p>'],
    ['Text\n---\nMehr', '<p>Text</p><p>---</p><p>Mehr</p>'],
    ['Preis: 5 € - 10 €', 'Preis: 5 € - 10 €'],
    ['Stand 2024. Das Jahr war gut.', 'Stand 2024. Das Jahr war gut.'],
    ['Unabgeschlossen **fett', 'Unabgeschlossen **fett'],
    ['a & b < c > d', 'a &amp; b &lt; c &gt; d'],
    ['    eingerückter Text', '    eingerückter Text'],
    ['| a | b |\n|---|---|', '| a | b |<br>|---|---|'],
  ]
  for (const [input, fragment] of cases) {
    it(JSON.stringify(input), () => {
      const out = renderRichText(input)
      expect(out).toContain(fragment)
      expect(unsafeFindings(out)).toEqual([])
    })
  }

  it('Text mit Zeilenanfängen *, _, # und - bleibt vollständig lesbar', () => {
    const legacy = '* Punkt mit Stern\n_ Unterstrich\n# Raute\n- Strich'
    const text = markdownToPlainText(legacy)
    for (const word of ['Punkt mit Stern', 'Unterstrich', '# Raute', 'Strich']) expect(text).toContain(word)
  })
})

describe('markdownToPlainText', () => {
  const cases = {
    '**fett** und *kursiv*': 'fett und kursiv',
    '[Status](https://example.com/s) ansehen': 'Status (https://example.com/s) ansehen',
    'https://example.com/s': 'https://example.com/s',
    '- a\n- b': '- a\n- b',
    '1. eins\n2. zwei': '1. eins\n2. zwei',
    'A\n\nB': 'A\n\nB',
    'Zeile 1\nZeile 2': 'Zeile 1\nZeile 2',
    '> zitat\n> zwei': '> zitat\n> zwei',
    '`code` hier': 'code hier',
    '<b>roh</b>': '<b>roh</b>',
    '# Titel': '# Titel',
    '- erster\n  weiter': '- erster\n  weiter',
    '': '',
    '   ': '',
  }
  for (const [input, expected] of Object.entries(cases)) {
    it(JSON.stringify(input), () => expect(markdownToPlainText(input)).toBe(expected))
  }

  it('Absätze im Zitat bleiben getrennt', () => {
    expect(markdownToPlainText('> a\n>\n> b')).toBe('> a\n> b')
  })
})

describe('isHttpsUrl', () => {
  it('nur https mit Host und ohne Zugangsdaten', () => {
    expect(isHttpsUrl('https://example.com/x')).toBe(true)
    expect(isHttpsUrl('http://example.com')).toBe(false)
    expect(isHttpsUrl('javascript:alert(1)')).toBe(false)
    expect(isHttpsUrl('https://nutzer@example.com')).toBe(false)
    expect(isHttpsUrl('https://nutzer:pw@example.com')).toBe(false)
    expect(isHttpsUrl('nicht-gültig')).toBe(false)
    expect(isHttpsUrl('')).toBe(false)
  })

  it('Höchstlänge entspricht dem Backend (5000)', () => {
    expect(RICH_TEXT_MAX_LENGTH).toBe(5000)
  })
})

// Die Doku-Seite behält ihr weites Profil (Repo-eigene Inhalte); der Hook für
// target/rel darf dort nicht hängen bleiben (eigene DOMPurify-Instanz).
describe('Isolation von DOMPurify', () => {
  it('der Link-Hook wirkt nicht auf das globale DOMPurify', () => {
    renderRichText('[x](https://example.com)')
    const out = DOMPurify.sanitize('<a href="https://example.com">x</a>')
    expect(out).toBe('<a href="https://example.com">x</a>')
  })

  it('richText.js nutzt kein USE_PROFILES', () => {
    expect(richTextSource).not.toMatch(/USE_PROFILES\s*:/)
  })
})

describe('plainTextToHtml', () => {
  it('maskiert und verkraftet fehlenden Text', () => {
    expect(plainTextToHtml('<b>&')).toBe('&lt;b&gt;&amp;')
    expect(plainTextToHtml(undefined)).toBe('')
    expect(plainTextToHtml(null)).toBe('')
  })
})

// Fail-closed: Findet DOMPurify kein DOM (isSupported false), gibt sanitize() seine
// Eingabe ungeprüft zurück. Dann liefert renderRichText nur maskierten Klartext.
describe('renderRichText ohne unterstütztes DOMPurify', () => {
  it('liefert maskierten Klartext statt ungeprüftem HTML', async () => {
    vi.resetModules()
    vi.doMock('dompurify', () => ({ default: () => ({ isSupported: false, addHook() {}, sanitize: (x) => x }) }))
    const mod = await import('./richText')
    expect(mod.renderRichText('**x** <script>alert(1)</script>')).toBe('<p>**x** &lt;script&gt;alert(1)&lt;/script&gt;</p>')
    vi.doUnmock('dompurify')
    vi.resetModules()
  })
})

describe('markdownToPlainText mit unvollständigem DOM', () => {
  it('liest Text, der ohne Absatz-Element im body steht', async () => {
    vi.resetModules()
    vi.doMock('dompurify', () => ({ default: () => ({ isSupported: true, addHook() {}, sanitize: () => '  nur Text  ' }) }))
    const mod = await import('./richText')
    expect(mod.markdownToPlainText('x')).toBe('nur Text')
    vi.doUnmock('dompurify')
    vi.resetModules()
  })
})

// Fund aus dem Review: Tief verschachtelte Zitate ließen marked mit
// RangeError (Aufrufstapel) scheitern; der Fehler leerte die öffentliche Seite.
describe('renderRichText: Render-Fehler', () => {
  it('tief verschachtelte Zitate werfen nicht, der Text bleibt sichtbar und maskiert', () => {
    const input = `${'>'.repeat(5000)} <script>alert(1)</script>`
    let out
    expect(() => { out = renderRichText(input) }).not.toThrow()
    expect(unsafeFindings(out)).toEqual([])
    expect(out).toContain('&lt;script&gt;')
    expect(markdownToPlainText(input)).toContain('<script>alert(1)</script>')
  })
})

// pathologische Eingaben machten marked quadratisch langsam
// (5.000 Zeichen `*a_` rund 0,4 s, 15.000 Zeichen bis 9 s). Zu lange Texte bzw.
// Absätze erscheinen deshalb vollständig als maskierter Klartext.
describe('Leistung bei pathologischen Eingaben', () => {
  const PATHOLOGICAL = {
    '*a_ wiederholt': '*a_'.repeat(Math.ceil(15000 / 3)),
    '_a* wiederholt': '_a*'.repeat(5000),
    '**a_ wiederholt': '**a_'.repeat(3750),
    '*a* Muster': '*a'.repeat(7500),
    'Zitate >': '>'.repeat(15000),
    '5.000 Zeichen *a_': '*a_'.repeat(1667),
  }
  const LIMIT_MS = 3000 // großzügig; ein Block bis 5.000 Zeichen kostet in marked bis ca. 0,4 s

  for (const [name, input] of Object.entries(PATHOLOGICAL)) {
    it(`${name}: Render und Klartext bleiben unter ${LIMIT_MS} ms`, () => {
      const start = performance.now()
      const html = renderRichText(input)
      const text = markdownToPlainText(input)
      expect(performance.now() - start).toBeLessThan(LIMIT_MS)
      expect(unsafeFindings(html)).toEqual([])
      expect(text.length).toBeGreaterThan(0)
    })
  }

  it('zu langer Text erscheint vollständig und maskiert, nicht abgeschnitten', () => {
    const input = `**fett** <b>x</b> ${'wort '.repeat(2000)}ENDE`
    const html = renderRichText(input)
    expect(html).not.toContain('<strong>')
    expect(html).toContain('**fett** &lt;b&gt;x&lt;/b&gt;')
    expect(html.endsWith('ENDE</p>')).toBe(true)
  })

  it('langer Text aus kurzen Absätzen wird weiter formatiert', () => {
    const input = Array.from({ length: 60 }, () => '**fett** ' + 'a'.repeat(60)).join('\n\n')
    expect(input.length).toBeLessThan(8000)
    expect(renderRichText(input)).toContain('<strong>fett</strong>')
  })

  it('Text über der Gesamtgrenze wird auch aus kurzen Absätzen nicht formatiert', () => {
    const input = Array.from({ length: 200 }, () => '**fett** ' + 'a'.repeat(60)).join('\n\n')
    expect(input.length).toBeGreaterThan(8000)
    expect(renderRichText(input)).not.toContain('<strong>')
  })

  it('Block genau an der Grenze wird noch formatiert, einer darüber nicht', () => {
    const at = '**f** ' + 'a'.repeat(RICH_TEXT_MAX_BLOCK_LENGTH - 6)
    expect(at.length).toBe(RICH_TEXT_MAX_BLOCK_LENGTH)
    expect(renderRichText(at)).toContain('<strong>')
    expect(renderRichText(at + 'a')).not.toContain('<strong>')
  })
})

// Fund 9: dieselben Grenzen und Fälle wie im Backend (Mail).
describe('Gleiche Grenzen wie das Backend', () => {
  const hasMarkup = (html) => /<(ul|ol|li|strong|em|code|blockquote|a)[\s>]/.test(html)

  it('Grenzen entsprechen der gemeinsamen Datei', () => {
    expect(RICH_TEXT_MAX_RENDER_LENGTH).toBe(parityFixture.limits.maxRenderLen)
    expect(RICH_TEXT_MAX_BLOCK_LENGTH).toBe(parityFixture.limits.maxBlockLen)
    expect(RICH_TEXT_MAX_RENDER_LENGTH).toBeGreaterThan(RICH_TEXT_MAX_LENGTH)
  })

  for (const c of parityFixture.cases) {
    it(`${c.name}: ${c.formatted ? 'formatiert' : 'unformatiert'}`, () => {
      const input = (c.prefix ?? '') + c.unit.repeat(c.count)
      expect(hasMarkup(renderRichText(input))).toBe(c.formatted)
    })
  }

  it('eine lange Liste ohne Leerzeile in erlaubter Länge wird formatiert', () => {
    const input = '- ab\n'.repeat(RICH_TEXT_MAX_LENGTH / 5)
    expect(input.length).toBe(RICH_TEXT_MAX_LENGTH)
    expect(renderRichText(input)).toContain('<ul>')
  })
})

// Fund 11: einzeilige ```...``` ist Inline-Code, nichts fällt weg.
describe('Dreifache Backticks in einer Zeile', () => {
  it('zeigt Inline-Code und behält die folgenden Zeilen (wie die Mail)', () => {
    const html = renderRichText('```npm install```\nDanach **fett**')
    expect(html).toContain('<code>npm install</code>')
    expect(html).toContain('Danach <strong>fett</strong>')
    expect(markdownToPlainText('```npm install```\nDanach\n\nEnde')).toBe('npm install\nDanach\n\nEnde')
  })
})

// Fund 12: Nummer des Listenanfangs bleibt erhalten.
describe('Nummerierte Liste beginnt bei ihrer Nummer', () => {
  it('<ol start> bleibt, Klartext zählt ab dieser Nummer', () => {
    expect(renderRichText('3. Schritt\n4. Weiter')).toContain('<ol start="3">')
    expect(renderRichText('2024. Das Jahr')).toContain('<ol start="2024">')
    expect(renderRichText('1. a\n2. b')).not.toContain('start=')
    expect(markdownToPlainText('3. Schritt\n4. Weiter')).toBe('3. Schritt\n4. Weiter')
    expect(markdownToPlainText('2024. Das Jahr')).toBe('2024. Das Jahr')
    expect(unsafeFindings(renderRichText('3. Schritt'))).toEqual([])
  })

  it('start ist nur als Zahl auf <ol> erlaubt', () => {
    // Rohes HTML erscheint als Text; die Whitelist lässt start nie auf anderen Tags durch.
    expect(unsafeFindings(renderRichText('<ol start="javascript:alert(1)"><li>x</li></ol>'))).toEqual([])
    expect(unsafeFindings(renderRichText('<p start="3">x</p>'))).toEqual([])
  })
})

// Auch wenn marked (Fehler/Update) ein unerwartetes start-Attribut
// ausgäbe, lässt der Sanitizer es nur als Zahl auf <ol> durch.
describe('Sanitizer-Whitelist für start', () => {
  async function renderWith(markedHtml) {
    vi.resetModules()
    vi.doMock('marked', async (orig) => {
      const real = await orig()
      class Fake extends real.Marked {
        constructor(...args) {
          super(...args)
          this.parse = () => markedHtml
        }
      }
      return { ...real, Marked: Fake }
    })
    const mod = await import('./richText')
    vi.doUnmock('marked')
    return mod.renderRichText('x')
  }

  it('entfernt start auf anderen Tags und bei Nicht-Zahlen, behält Zahlen auf <ol>', async () => {
    expect(await renderWith('<p start="3">a</p>')).toBe('<p>a</p>')
    expect(await renderWith('<ol start="x1"><li>a</li></ol>')).toBe('<ol><li>a</li></ol>')
    expect(await renderWith('<ol start="12"><li>a</li></ol>')).toBe('<ol start="12"><li>a</li></ol>')
    expect(await renderWith('<ol><li>a</li></ol>')).toBe('<ol><li>a</li></ol>')
  })
})

describe('Zwischenspeicher', () => {
  async function loadCounting() {
    vi.resetModules()
    const calls = { n: 0 }
    vi.doMock('marked', async (orig) => {
      const real = await orig()
      class Counting extends real.Marked {
        constructor(...args) {
          super(...args)
          const parse = this.parse
          this.parse = (...a) => { calls.n++; return parse(...a) }
        }
      }
      return { ...real, Marked: Counting }
    })
    const mod = await import('./richText')
    vi.doUnmock('marked')
    return { mod, calls }
  }

  it('zweiter Aufruf mit gleichem Text ruft marked nicht erneut auf', async () => {
    const { mod, calls } = await loadCounting()
    const first = mod.renderRichText('**a** b')
    expect(calls.n).toBe(1)
    expect(mod.renderRichText('**a** b')).toBe(first)
    expect(calls.n).toBe(1)
    mod.renderRichText('anderer Text')
    expect(calls.n).toBe(2)
  })

  it('markdownToPlainText nutzt ebenfalls den Zwischenspeicher', async () => {
    const { mod, calls } = await loadCounting()
    expect(mod.markdownToPlainText('**a** b')).toBe('a b')
    const after = calls.n
    expect(mod.markdownToPlainText('**a** b')).toBe('a b')
    expect(calls.n).toBe(after)
  })

  it('die Obergrenze verdrängt den ältesten Eintrag', async () => {
    const { mod, calls } = await loadCounting()
    for (let i = 0; i <= mod.RICH_TEXT_CACHE_SIZE; i++) mod.renderRichText(`text ${i}`)
    const before = calls.n
    mod.renderRichText(`text ${mod.RICH_TEXT_CACHE_SIZE}`) // jüngster: noch drin
    expect(calls.n).toBe(before)
    mod.renderRichText('text 0') // ältester: verdrängt
    expect(calls.n).toBe(before + 1)
  })

  // Fund 13: LRU statt FIFO - ein Treffer hält den Eintrag im Speicher.
  it('ein gelesener Eintrag wird nicht als nächster verdrängt (LRU)', async () => {
    const { mod, calls } = await loadCounting()
    for (let i = 0; i < mod.RICH_TEXT_CACHE_SIZE; i++) mod.renderRichText(`text ${i}`)
    mod.renderRichText('text 0') // Treffer: jetzt jüngster
    mod.renderRichText('neu') // verdrängt text 1, nicht text 0
    const before = calls.n
    mod.renderRichText('text 0')
    expect(calls.n).toBe(before)
    mod.renderRichText('text 1')
    expect(calls.n).toBe(before + 1)
  })

  it('sehr lange Texte werden nicht als Schlüssel gemerkt', () => {
    let runs = 0
    const cached = memoize((k) => { runs++; return k.length })
    const huge = 'x'.repeat(RICH_TEXT_CACHE_MAX_KEY_LENGTH + 1)
    cached(huge); cached(huge)
    expect(runs).toBe(2)
    const fits = 'x'.repeat(RICH_TEXT_CACHE_MAX_KEY_LENGTH)
    cached(fits); cached(fits)
    expect(runs).toBe(3)
  })

  it('leere und fehlende Texte werden unverändert behandelt', async () => {
    const { mod } = await loadCounting()
    expect(mod.renderRichText(undefined)).toBe('')
    expect(mod.markdownToPlainText(null)).toBe('')
  })
})
