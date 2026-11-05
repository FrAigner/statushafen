import { describe, it, expect, beforeEach } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname } from 'path'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

describe('Phase 1: CSS System (Breakpoints, Spacing, Typography)', () => {
  // Die Tokens stehen seit der Konsolidierung in assets/tokens.css
  // (style.css enthält nur Komponenten-Regeln); die Variable heißt weiter styleCss.
  describe('CSS Variables defined in assets/tokens.css', () => {
    let styleCss

    beforeEach(() => {
      styleCss = readFileSync(`${__dirname}/assets/tokens.css`, 'utf-8')
    })

    // Breakpoint Variables
    it('should define --bp-sm (480px)', () => {
      expect(styleCss).toContain('--bp-sm: 480px')
    })

    it('should define --bp-md (768px)', () => {
      expect(styleCss).toContain('--bp-md: 768px')
    })

    it('should define --bp-lg (900px)', () => {
      expect(styleCss).toContain('--bp-lg: 900px')
    })

    it('should define --bp-xl (1200px)', () => {
      expect(styleCss).toContain('--bp-xl: 1200px')
    })

    // Responsive Abstände (clamp), nur die noch genutzten
    it('should define --space-sm with clamp', () => {
      expect(styleCss).toContain('--space-sm: clamp(12px, 3vw, 16px)')
    })

    it('should define --space-md with clamp', () => {
      expect(styleCss).toContain('--space-md: clamp(16px, 4vw, 24px)')
    })

    it('should define --gap-sm with clamp', () => {
      expect(styleCss).toContain('--gap-sm: clamp(12px, 3vw, 16px)')
    })
  })

  // Redesign Stufe 2: Der Mandanten-Umschalter sitzt kompakt in der
  // dunklen Kopfzeile (früher Seitenleiste), die Bereiche stehen als
  // Reiter statt als Seitenleiste.
  describe('Admin-Kopfzeile: Mandanten-Umschalter und Reiter', () => {
    let styleCss

    beforeEach(() => {
      styleCss = readFileSync(`${__dirname}/style.css`, 'utf-8')
    })

    it('behält das Label des Umschalters nur für Screenreader (visuell versteckt)', () => {
      const rule = styleCss.match(/\.tenant-select-label\s*\{[^}]*\}/)[0]
      expect(rule).toContain('position: absolute')
      expect(rule).toContain('clip: rect(0 0 0 0)')
    })

    it('hat einen sichtbaren Hover-State', () => {
      expect(styleCss).toMatch(/\.tenant-select:hover[^{]*\{[^}]*background/)
    })

    it('färbt Kopfzeile und Reiter nur mit Tokens der Ebene admin (--top-*, --nav-*)', () => {
      const bar = styleCss.match(/\.admin-topbar\s*\{[^}]*\}/)[0]
      expect(bar).toContain('background: var(--top-bg)')
      expect(bar).toContain('color: var(--top-text)')
      expect(styleCss.match(/\.area-tabs\s*\{[^}]*\}/)[0]).toContain('background: var(--nav-bg)')
      const active = styleCss.match(/\.area-tab\.active\s*\{[^}]*\}/)[0]
      expect(active).toContain('background: var(--nav-active-bg)')
      expect(active).toContain('color: var(--nav-active-text)')
    })

    it('zeigt nur die Einträge des aktiven Bereichs, der aktive Eintrag hat einen Primärstrich', () => {
      expect(styleCss).toMatch(/\.area-items\s*\{\s*display:\s*none/)
      expect(styleCss).toMatch(/\.area-items\.is-shown\s*\{\s*display:\s*block/)
      expect(styleCss.match(/\.area-items a\.active\s*\{[^}]*\}/)[0]).toContain('border-bottom-color: var(--primary)')
    })

    it('hat sichtbare Fokusringe in der dunklen Kopfzeile und im Benutzermenü', () => {
      expect(styleCss).toMatch(/\.topbar-main :focus-visible,\s*\.area-tabs :focus-visible\s*\{[^}]*outline:\s*2px solid var\(--top-text\)/)
      expect(styleCss).toMatch(/\.user-menu-panel :focus-visible\s*\{[^}]*outline:\s*2px solid var\(--focus\)/)
    })

    it('schaltet Übergänge der Kopfzeile unter prefers-reduced-motion ab', () => {
      const block = styleCss.match(/@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\n\}/)[0]
      expect(block).toContain('animation: none')
      expect(block).toContain('transition: none')
    })
  })

  describe('Mobile Navigation', () => {
    let mobile

    beforeEach(() => {
      const css = readFileSync(`${__dirname}/style.css`, 'utf-8')
      mobile = css.match(/@media \(max-width: 768px\) \{[\s\S]*?\n\}\n/)[0]
    })

    it('blendet die Reiter aus und fährt die Bereiche als Menü aus', () => {
      expect(mobile).toMatch(/\.area-tabs\s*\{\s*display:\s*none/)
      expect(mobile).toMatch(/\.area-items-row\s*\{\s*display:\s*none/)
      expect(mobile).toMatch(/\.admin-nav\.is-open \.area-items-row\s*\{[^}]*position:\s*fixed/)
    })

    it('zeigt im Menü alle Bereiche, auch wenn sie auf dem Desktop ausgeblendet sind', () => {
      expect(mobile).toMatch(/\.admin-nav\.is-open \.area-items\.is-shown\s*\{\s*display:\s*block/)
    })

    it('bricht die Kopfzeile um, damit die Marke nicht abgeschnitten wird', () => {
      expect(mobile).toMatch(/\.topbar-main\s*\{[^}]*flex-wrap:\s*wrap/)
      expect(mobile).toMatch(/\.tenant-switch\s*\{[^}]*flex:\s*1 0 100%/)
      const brand = readFileSync(`${__dirname}/style.css`, 'utf-8').match(/\.topbar-brand\s*\{[^}]*\}/)[0]
      expect(brand).not.toMatch(/overflow:\s*hidden|text-overflow/)
    })
  })

  describe('Mobile Hamburger Menu', () => {
    let styleCss

    beforeEach(() => {
      styleCss = readFileSync(`${__dirname}/style.css`, 'utf-8')
    })

    it('erhöht den Overlay-Kontrast auf rgba(0, 0, 0, 0.7)', () => {
      expect(styleCss).toContain('background: rgba(0, 0, 0, 0.7);')
    })

    it('definiert eine Fade-Transition für den Burger/X-Icon-Wechsel', () => {
      expect(styleCss).toMatch(/\.burger-icon-fade-enter-active,\s*\.burger-icon-fade-leave-active\s*\{[^}]*transition:\s*opacity/)
      expect(styleCss).toMatch(/\.burger-icon-fade-enter-from,\s*\.burger-icon-fade-leave-to\s*\{[^}]*opacity:\s*0/)
    })
  })
})

// Der Admin-Inhalt war auf 1400 px begrenzt und linksbündig: auf großen Schirmen
// blieb rechts ein leerer Streifen, Tabellen mit Seitenformular wurden gequetscht.
describe('Admin-Inhalt nutzt die volle Breite', () => {
  it('.admin-content hat kein max-width', () => {
    const css = readFileSync(`${__dirname}/style.css`, 'utf-8')
    const block = css.match(/\.admin-content \{([^}]*)\}/)?.[1] ?? ''
    expect(block).not.toBe('')
    expect(block).not.toMatch(/(^|[\s;])max-width\s*:/)
  })
})

// Die Filterchips der Statusseite blieben bei Kundenfarbe indigo, weil
// sie --color-primary (Ebenen-Wert) nutzten; die Kundenmarke setzt nur --primary
// (Inline-Style auf .content-area), wie beim Abonnieren-Knopf.
describe('Filterchips folgen der Kundenmarke', () => {
  const css = readFileSync(`${__dirname}/style.css`, 'utf-8').replace(/\/\*[\s\S]*?\*\//g, '')
  const rule = (selector) => {
    const escaped = selector.replace(/[.:]/g, '\\$&')
    return css.match(new RegExp(`\\n${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? ''
  }

  it('.filter-tag:hover und .filter-tag.active nutzen --primary, nicht --color-primary', () => {
    for (const sel of ['.filter-tag:hover', '.filter-tag.active']) {
      const r = rule(sel)
      expect(r, sel).toMatch(/var\(--primary\)/)
      expect(r, sel).not.toMatch(/--color-primary/)
    }
  })
})

describe('Admin-Kopfzeile: Logo statt Punkt', () => {
  const css = readFileSync(`${__dirname}/style.css`, 'utf-8')
  it('stylt .brand-logo und kennt den alten .brand-dot nicht mehr', () => {
    expect(css).toMatch(/\.topbar-brand \.brand-logo\s*\{[^}]*width:\s*24px/)
    expect(css).not.toContain('brand-dot')
  })
  it('verkleinert das Logo im schmalen Layout statt es auszublenden', () => {
    expect(css).toMatch(/\.topbar-brand \.brand-logo\s*\{\s*width:\s*20px;\s*height:\s*20px;\s*\}/)
  })
})
