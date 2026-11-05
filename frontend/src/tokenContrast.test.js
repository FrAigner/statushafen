import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'
import { contrastRatio, parseHex } from './utils/contrast'

// Redesign Stufe 1: Kontrast der Design-Tokens als Unit-Test.
//
// Dieser Test liest die FINALEN Werte aus
// assets/tokens.css (beide Ebenen, hell und dunkel) und prüft dieselben Paare
// nach WCAG 2.1 AA: Text >= 4,5:1, Symbole/Flächen >= 3:1. Ein zu helles Token
// fällt so schon in der Unit-Suite auf, nicht erst im e2e-axe (a11y.spec.js).
//
// happy-dom rechnet kein CSS, deshalb wird die Datei geparst und das Kaskaden-
// Verhalten nachgebaut, das hier zählt: Ein Element der Ebene erbt von :root,
// seine eigenen Deklarationen (.theme-public/.theme-admin) gewinnen, und
// html[data-theme="dark"] schlägt die Hell-Werte derselben Stufe.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const tokensCss = readFileSync(resolve(root, 'src/assets/tokens.css'), 'utf-8')

const DARK = 'html[data-theme="dark"]'

function parseBlocks(css) {
  const clean = css.replace(/\/\*[\s\S]*?\*\//g, '')
  const blocks = []
  for (const [, selector, body] of clean.matchAll(/([^{}@;]+)\{([^{}]*)\}/g)) {
    const decls = {}
    for (const [, name, value] of body.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) decls[name] = value.trim()
    blocks.push({ selectors: selector.split(',').map(s => s.trim().replace(/\s+/g, ' ')), decls })
  }
  return blocks
}

function collect(blocks, selectors) {
  const merged = {}
  for (const b of blocks) {
    if (b.selectors.some(s => selectors.includes(s))) Object.assign(merged, b.decls)
  }
  return merged
}

// Löst var(--x) rekursiv auf (nur der Wert, keine Fallbacks nötig).
function resolveTokens(map) {
  const out = {}
  const resolve1 = (name, seen = []) => {
    const value = map[name]
    if (value === undefined || seen.includes(name)) return undefined
    return value.replace(/var\((--[\w-]+)\)/g, (_, ref) => resolve1(ref, [...seen, name]) ?? `var(${ref})`)
  }
  for (const name of Object.keys(map)) out[name] = resolve1(name)
  return out
}

// Endwerte eines Elements der Ebene: erst :root (hell, ggf. dunkel), darüber
// die eigenen Regeln der Ebene (hell, ggf. dunkel).
function levelTokens(css, level, mode) {
  const blocks = parseBlocks(css)
  const cls = `.theme-${level}`
  const base = collect(blocks, [':root'])
  const baseDark = mode === 'dark' ? collect(blocks, [DARK]) : {}
  const own = collect(blocks, [cls])
  const ownDark = mode === 'dark' ? collect(blocks, [`${DARK} ${cls}`]) : {}
  return resolveTokens({ ...base, ...baseDark, ...own, ...ownDark })
}

const LEVELS = ['public', 'admin']
const MODES = ['light', 'dark']
const STATUSES = ['ok', 'deg', 'part', 'major', 'maint']

// Paar-Liste wie generator/check.mjs; Hintergründe als Token-Namen.
function pairsFor() {
  const pairs = []
  const add = (fg, bg, min) => pairs.push([fg, bg, min])
  for (const bg of ['bg', 'surface', 'surface-2']) {
    for (const fg of ['text', 'text-2', 'text-3', 'primary-text']) add(fg, bg, 4.5)
  }
  add('on-primary', 'primary', 4.5)
  add('on-primary', 'primary-hover', 4.5)
  add('nav-text', 'nav-bg', 4.5)
  // Kopfzeile und Bereichs-Reiter des Admin-Layouts (Text auf --top-bg).
  add('top-text', 'top-bg', 4.5)
  add('nav-text', 'top-bg', 4.5)
  add('nav-active-text', 'nav-active-bg', 4.5)
  add('primary-text', 'primary-soft', 4.5)
  for (const s of STATUSES) {
    for (const bg of ['surface', 'surface-2', `${s}-soft`, 'bg']) add(`${s}-text`, bg, 4.5)
    // Beschreibung und Zusammenfassung stehen im Hero auf der Statustönung.
    add('text-2', `${s}-soft`, 4.5)
    add(s, 'surface', 3)
    add(s, 'bg', 3)
    add('icon-on', s, 3)
  }
  add('focus', 'surface', 3)
  add('border-strong', 'surface', 1.5)
  return pairs
}

function failures(tokens, pairs = pairsFor()) {
  const bad = []
  for (const [fg, bg, min] of pairs) {
    const a = parseHex(tokens[`--${fg}`] ?? '')
    const b = parseHex(tokens[`--${bg}`] ?? '')
    if (!a || !b) { bad.push(`${fg} auf ${bg}: Token fehlt oder ist kein Hex-Wert`); continue }
    const ratio = contrastRatio(a, b)
    if (ratio < min) bad.push(`${fg} (${tokens[`--${fg}`]}) auf ${bg} (${tokens[`--${bg}`]}): ${ratio.toFixed(2)} < ${min}`)
  }
  return bad
}

describe('Token-Kontrast', () => {
  for (const level of LEVELS) {
    for (const mode of MODES) {
      describe(`Ebene ${level}, ${mode === 'light' ? 'hell' : 'dunkel'}`, () => {
        const tokens = levelTokens(tokensCss, level, mode)

        it('alle Textpaare erreichen 4,5:1, Symbole und Flächen 3:1', () => {
          expect(failures(tokens)).toEqual([])
        })

        it('sit-onyx-Markenskala: 500 und 600 tragen weiße Schrift mit 4,5:1 (CTA-Buttons)', () => {
          for (const step of ['500', '600']) {
            const brand = parseHex(tokens[`--onyx-color-onyx-${step}`])
            expect(brand, `--onyx-color-onyx-${step}`).not.toBeNull()
            expect(contrastRatio(brand, parseHex('#ffffff'))).toBeGreaterThanOrEqual(4.5)
          }
        })

        it('Markenskala 500/600 entspricht im Hell-Modus --primary/--primary-hover', () => {
          if (mode === 'light') {
            expect(tokens['--onyx-color-onyx-500']).toBe(tokens['--primary'])
            expect(tokens['--onyx-color-onyx-600']).toBe(tokens['--primary-hover'])
          }
        })

        it('onyx-Variablen zeigen auf die Tokens der Ebene', () => {
          expect(tokens['--onyx-color-base-background-blank']).toBe(tokens['--surface'])
          expect(tokens['--onyx-color-base-background-tinted']).toBe(tokens['--surface-2'])
          expect(tokens['--onyx-color-text-icons-neutral-intense']).toBe(tokens['--text'])
          expect(tokens['--onyx-color-text-icons-neutral-medium']).toBe(tokens['--text-2'])
          expect(tokens['--onyx-color-text-icons-neutral-soft']).toBe(tokens['--text-3'])
          expect(tokens['--onyx-color-text-icons-danger-intense']).toBe(tokens['--major-text'])
          expect(tokens['--onyx-color-component-border-neutral']).toBe(tokens['--border-strong'])
          expect(tokens['--onyx-color-component-border-neutral-hover']).toBe(tokens['--text-3'])
          expect(tokens['--onyx-radius-md']).toBe('6px')
        })

        // Der Umrissknopf („+ Neue Gruppe“) färbt Text und Rand mit
        // --onyx-color-text-icons-primary-intense. sit-onyx setzt es auf :root
        // (Wert der Hell-Skala, light-dark() greift ohne color-scheme nie); ohne eigene
        // Zeile im Ableitungsblock sah das Admin-Dunkel #4b44d6 auf #10151c (2,4:1).
        it('onyx-Primärtext (Umrissknöpfe, Links) folgt --primary-text und erreicht 4,5:1', () => {
          expect(tokens['--onyx-color-text-icons-primary-intense']).toBe(tokens['--primary-text'])
          const fg = parseHex(tokens['--onyx-color-text-icons-primary-intense'])
          for (const bg of ['bg', 'surface', 'surface-2']) {
            expect(contrastRatio(fg, parseHex(tokens[`--${bg}`])), bg).toBeGreaterThanOrEqual(4.5)
          }
        })

        it('der Marken-Hook --text-secondary zeigt auf die Ebene', () => {
          expect(tokens['--text-secondary']).toBe(tokens['--text-2'])
        })
      })
    }
  }

  it('beide Ebenen definieren dieselben Paletten-Schlüssel hell und dunkel', () => {
    const keys = (level, mode) => Object.keys(levelTokens(tokensCss, level, mode))
      .filter(k => !k.startsWith('--onyx-') && !k.startsWith('--bg-') && !k.startsWith('--text-') && !k.startsWith('--color-') && !k.startsWith('--status-'))
      .sort()
    expect(keys('admin', 'light')).toEqual(keys('public', 'light'))
    expect(keys('public', 'dark')).toEqual(keys('public', 'light'))
    expect(keys('admin', 'dark')).toEqual(keys('public', 'light'))
  })

  it('die Ebenen unterscheiden sich sichtbar (public = Indigo A, admin = Blau C)', () => {
    expect(levelTokens(tokensCss, 'public', 'light')['--primary']).toBe('#4b44d6')
    expect(levelTokens(tokensCss, 'admin', 'light')['--primary']).toBe('#1f5bcc')
    expect(levelTokens(tokensCss, 'public', 'dark')['--bg']).toBe('#0c0c0f')
    expect(levelTokens(tokensCss, 'admin', 'dark')['--bg']).toBe('#090c11')
    expect(levelTokens(tokensCss, 'admin', 'dark')['--font-sans']).toMatch(/^'IBM Plex Sans'/)
    expect(levelTokens(tokensCss, 'public', 'dark')['--font-sans']).toMatch(/^'Inter'/)
  })

  describe('Gegenprobe: der Test erkennt zu geringen Kontrast', () => {
    const tokens = levelTokens(tokensCss, 'public', 'light')

    it('ein zu helles Gelb als Schrift auf Weiß fällt durch', () => {
      const bad = { ...tokens, '--deg-text': '#d9a520' }
      expect(failures(bad).some(f => f.startsWith('deg-text'))).toBe(true)
    })

    it('ein zu helles Statussymbol (3:1) fällt durch', () => {
      const bad = { ...tokens, '--ok': '#7fd3a0' }
      expect(failures(bad).some(f => f.startsWith('ok ('))).toBe(true)
    })

    it('ein fehlendes Token wird gemeldet statt übersprungen', () => {
      const bad = { ...tokens }
      delete bad['--major-text']
      expect(failures(bad).some(f => f.includes('Token fehlt'))).toBe(true)
    })

    it('der aktuelle Stand ist grün (Gegenstück zu den roten Fällen)', () => {
      expect(failures(tokens)).toEqual([])
    })
  })

  describe('Parser', () => {
    it('liest Selektorlisten, Kommentare und löst var() auf', () => {
      const css = `/* x */ :root, .theme-public { --a: #111111; --b: var(--a); }
        html[data-theme="dark"] .theme-public { --a: #eeeeee; }`
      expect(levelTokens(css, 'public', 'light')).toEqual({ '--a': '#111111', '--b': '#111111' })
      expect(levelTokens(css, 'public', 'dark')).toEqual({ '--a': '#eeeeee', '--b': '#eeeeee' })
    })

    it('eigene Regeln der Ebene schlagen :root, ein unbekannter Verweis bleibt stehen', () => {
      const css = ':root { --a: #111111; --c: var(--fehlt); } .theme-admin { --a: #222222; }'
      const t = levelTokens(css, 'admin', 'light')
      expect(t['--a']).toBe('#222222')
      expect(t['--c']).toBe('var(--fehlt)')
    })

    it('Zyklen laufen nicht endlos', () => {
      expect(resolveTokens({ '--a': 'var(--b)', '--b': 'var(--a)' })['--a']).toBe('var(--a)')
    })
  })
})
