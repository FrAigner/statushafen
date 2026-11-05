import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve, relative, join } from 'path'

// Redesign Stufe 1: Sperre gegen neue Hex-Farben.
//
// Farben gehören in Tokens (assets/tokens.css), nicht in Views, Layouts und
// Komponenten. Vor Stufe 1 standen dort 170 Hex-Werte (Stufe 4f: 0);
// sie sollen bis Stufe 4 auf 0 sinken (umsetzung.md). Bis dahin gilt eine
// Ratsche: Der Test zählt Hex-Farben in src/**/*.vue und src/**/*.css außerhalb
// der Token-Datei und lässt nur MAX_HEX_COLORS zu.
//  - Steigt die Zahl, schlägt der Test fehl: stattdessen var(--token) nutzen
//    (oder, falls es noch kein passendes Token gibt, eines in tokens.css anlegen).
//  - Sinkt die Zahl, schlägt er ebenfalls fehl und verlangt, die Obergrenze im
//    selben PR zu senken, damit der Fortschritt nicht wieder verloren geht.
// Kommentare zählen nicht (dort stehen Verweisnummern wie #472 und Beispiele).
const MAX_HEX_COLORS = 0

const root = resolve(dirname(fileURLToPath(import.meta.url)))
const TOKEN_FILES = ['assets/tokens.css']

function sourceFiles(dir = root) {
  const files = []
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

// Entfernt Kommentare, damit nur echter Code gezählt wird: /* … */ (CSS und JS),
// <!-- … --> (Templates) und ganze // -Zeilen bzw. // nach Leerraum (JS).
// "http://…" bleibt unberührt, weil vor // ein Doppelpunkt steht.
export function stripComments(text) {
  return text
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/(^|\s)\/\/.*$/gm, '$1')
}

// #rgb, #rrggbb und #rrggbbaa; nicht mitten in einem Wort, URL-Anker oder
// HTML-Entität (&#123;).
const HEX = /(?<![\w&])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3})\b/g

export function countHex(text) {
  return (stripComments(text).match(HEX) ?? []).length
}

function countsPerFile() {
  const counts = {}
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
      `Es sind nur noch ${total} Hex-Farben: MAX_HEX_COLORS in src/hexColors.test.js auf ${total} senken.`,
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
