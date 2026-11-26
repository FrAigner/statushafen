import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve, relative, join } from 'path'

// Redesign stage 1: ban on new hex colors.
//
// Colors belong in tokens (assets/tokens.css), not in views, layouts and
// components. Before stage 1 there were 170 hex values there (stage 4f: 0);
// they were to drop to 0 by stage 4 (umsetzung.md). Until then a ratchet
// applies: the test counts hex colors in src/**/*.vue and src/**/*.css outside
// the token file and allows only MAX_HEX_COLORS.
//  - If the number rises, the test fails: use var(--token) instead
//    (or, if no suitable token exists yet, add one to tokens.css).
//  - If the number drops, it fails as well and requires lowering the limit in
//    the same PR so the progress is not lost again.
// Comments do not count (they contain reference numbers like #472 and examples).
const MAX_HEX_COLORS = 0

const root = resolve(dirname(fileURLToPath(import.meta.url)))
const TOKEN_FILES = ['assets/tokens.css']

function sourceFiles(dir = root): string[] {
  const files: string[] = []
  for (const name of readdirSync(dir)) {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) {
      files.push(...sourceFiles(full))
    } else if (/\.(vue|css)$/.test(name)) {
      files.push(full)
    }
  }
  return files
}

// Strips comments so only real code is counted: /* … */ (CSS and JS),
// <!-- … --> (templates) and whole // lines or // after whitespace (JS).
// "http://…" is left alone because a colon precedes the //.
export function stripComments(text: string) {
  return text
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/(^|\s)\/\/.*$/gm, '$1')
}

// #rgb, #rrggbb and #rrggbbaa; not in the middle of a word, URL anchor or
// HTML entity (&#123;).
const HEX = /(?<![\w&])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3})\b/g

export function countHex(text: string) {
  return (stripComments(text).match(HEX) ?? []).length
}

function countsPerFile() {
  const counts: Record<string, number> = {}
  for (const file of sourceFiles()) {
    const rel = relative(root, file)
    if (TOKEN_FILES.includes(rel)) continue
    const n = countHex(readFileSync(file, 'utf-8'))
    if (n > 0) counts[rel] = n
  }
  return counts
}

describe('Hex-Farben-Sperre', () => {
  const counts = countsPerFile()
  const total = Object.values(counts).reduce((a, b) => a + b, 0)
  const top = Object.entries(counts).sort((a, b) => b[1] - a[1]).slice(0, 8)
    .map(([f, n]) => `${f}: ${n}`).join(', ')

  it(`höchstens ${MAX_HEX_COLORS} Hex-Farben in .vue/.css außerhalb von tokens.css`, () => {
    expect(
      total,
      `Neue Hex-Farben gefunden (${total} statt höchstens ${MAX_HEX_COLORS}). Nutze var(--token) aus assets/tokens.css. Größte Dateien: ${top}`,
    ).toBeLessThanOrEqual(MAX_HEX_COLORS)
  })

  it('Ratsche: sinkt die Zahl, wird die Obergrenze im selben PR gesenkt', () => {
    expect(
      total,
      `Es sind nur noch ${total} Hex-Farben: MAX_HEX_COLORS in src/hexColors.test.ts auf ${total} senken.`,
    ).toBe(MAX_HEX_COLORS)
  })

  it('tokens.css ist die einzige ausgenommene Datei und enthält die Farben wirklich', () => {
    expect(TOKEN_FILES).toEqual(['assets/tokens.css'])
    expect(countHex(readFileSync(resolve(root, 'assets/tokens.css'), 'utf-8'))).toBeGreaterThan(100)
  })

  describe('Zähler', () => {
    it('erkennt #rgb, #rrggbb und #rrggbbaa', () => {
      expect(countHex('a { color: #fff; background: #1a2b3c; border-color: #1a2b3c80 }')).toBe(3)
    })

    it('ignoriert Kommentare, Verweisnummern darin, Anker und Entitäten', () => {
      const text = [
        '/* #ff0000 */',
        '<!-- #00ff00 -->',
        '// Nummer #471 und #abc',
        'const u = "https://x.de/#/pfad"; // #def',
        '<a href="#main">&#123;</a>',
      ].join('\n')
      expect(countHex(text)).toBe(0)
    })

    it('zählt var()-Fallbacks und Inline-Styles mit', () => {
      expect(countHex('color: var(--danger, #c0392b)')).toBe(1)
      expect(countHex('<div style="color: #fff;">')).toBe(1)
    })

    it('lässt Rgb-Funktionen und Namen aus', () => {
      expect(countHex('color: rgb(1, 2, 3); background: red; x: color-mix(in srgb, red 40%, transparent)')).toBe(0)
    })

    it('ein zusätzliches Hex würde die Sperre auslösen (Gegenprobe)', () => {
      expect(total + countHex('.neu { color: #123456 }')).toBeGreaterThan(MAX_HEX_COLORS)
    })
  })
})
