import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// Redesign Stufe 3: Schutz für die CSS-Anteile der Bausteine.
// happy-dom rechnet kein CSS; geprüft wird deshalb der Quelltext (Muster wie
// style.test.js). Das Aussehen selbst prüft dieser Test nicht.
const src = dirname(fileURLToPath(import.meta.url))
const read = file => readFileSync(resolve(src, file), 'utf-8')
const styleCss = read('style.css')
const adminCss = read('admin.css')

// Regelkörper zu einem exakten Selektor (erstes Vorkommen), ohne Kommentare.
function rule(css, selector) {
  const clean = css.replace(/\/\*[\s\S]*?\*\//g, '')
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = clean.match(new RegExp(`(?:^|\\})\\s*${escaped}\\s*\\{([^}]*)\\}`))
  return match ? match[1] : null
}

describe('Tabellenstil', () => {
  it('Regression: admin.css überschreibt .data-table nicht mehr (zweite, ältere Definition)', () => {
    // Vorher stand der Tabellenstil doppelt in style.css und admin.css; admin.css
    // lud später und gewann, ein neuer Stil in style.css blieb wirkungslos.
    expect(adminCss).not.toMatch(/\.data-table/)
  })

  it('behält .data-table und .data-table-wrapper (e2e-Specs suchen danach)', () => {
    expect(rule(styleCss, '.data-table')).not.toBeNull()
    expect(rule(styleCss, '.data-table-wrapper')).not.toBeNull()
  })

  it('Kopf ist ruhig: 12 px, keine Versalien', () => {
    const head = rule(styleCss, '.data-table thead th')
    expect(head).toContain('font-size: var(--fs-xs)')
    expect(head).not.toMatch(/text-transform/)
  })

  it('Zeilen sind 52 px (--row-h), Zahlen tabular-nums und rechtsbündig', () => {
    expect(rule(styleCss, '.data-table td')).toContain('height: var(--row-h)')
    expect(rule(styleCss, '.data-table')).toContain('font-variant-numeric: tabular-nums')
    expect(rule(styleCss, '.data-table .is-numeric')).toContain('text-align: right')
  })

  it('wird unter 860 px zu Karten, mit data-label als Beschriftung', () => {
    expect(styleCss).toMatch(/@media \(max-width: 859px\)\s*\{[\s\S]*\.data-table--cards td::before\s*\{[^}]*attr\(data-label\)/)
  })
})

describe('Buttons', () => {
  it('sind 36 px hoch (--ctl-h) und haben einen Fokusring 2 px --focus mit 2 px Abstand', () => {
    expect(rule(styleCss, '.btn')).toContain('min-height: var(--ctl-h)')
    const focus = rule(styleCss, '.btn:focus-visible')
    expect(focus).toContain('outline: 2px solid var(--focus)')
    expect(focus).toContain('outline-offset: 2px')
  })

  it('kennen primary, secondary, ghost und danger', () => {
    for (const variant of ['primary', 'secondary', 'ghost', 'danger']) {
      expect(rule(styleCss, `.btn-${variant}`), `.btn-${variant}`).not.toBeNull()
    }
  })

  it('deaktivierte Buttons sind gesperrt dargestellt', () => {
    expect(styleCss).toMatch(/\.btn:disabled,\s*\.btn\[aria-disabled="true"\]\s*\{[^}]*cursor: not-allowed/)
  })
})

describe('style.css: Selektoren', () => {
  it('Regression: kein Selektor endet mit Komma vor einer Leerzeile oder einem Kommentar', () => {
    // Ein übrig gebliebener Selektor (`[data-theme="dark"] .action-btn.danger:hover,`)
    // hing vor `.alert-close` und gab dem Hover-Zustand deren Regeln
    // (float: right, Hintergrund none, …).
    expect(styleCss.replace(/\/\*[\s\S]*?\*\//g, '')).not.toMatch(/,\s*\n\s*\n/)
    expect(styleCss).not.toMatch(/,\s*\n\s*\/\*/)
  })
})

describe('Bausteine nutzen nur Tokens', () => {
  const components = ['StatusIcon', 'StatusBadge', 'EmptyState', 'PageHeader', 'KpiCard', 'FormField', 'QuickStatus', 'HelpTooltipLayer']
  it.each(components)('%s.vue enthält keine Hex- oder rgb-Farben', (name) => {
    // Kommentare (/* */ und //-Zeilen mit Verweisnummern wie #472) zählen nicht.
    const text = read(`components/${name}.vue`)
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .replace(/^\s*\/\/.*$/gm, '')
    expect(text).not.toMatch(/#[0-9a-fA-F]{3,8}\b/)
    expect(text).not.toMatch(/rgba?\(/)
  })

  it('HelpTooltipLayer: max. 300 px breit, kleine Versalien-Überschriften', () => {
    const text = read('components/HelpTooltipLayer.vue')
    expect(text).toContain('min(300px')
    expect(text).toMatch(/\.help-tooltip__heading\s*\{[^}]*text-transform: uppercase/)
  })
})
