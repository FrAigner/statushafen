import { describe, it, expect } from 'vitest'
import statusPageSource from './StatusPage.vue?raw'
import treeNodeSource from '../../components/ComponentTreeNode.vue?raw'
import publicLayoutSource from '../../layouts/PublicLayout.vue?raw'

// The public status page scrolled horizontally at 390 px
// (scrollWidth 471 instead of 390): status badges cut off on the right, hint and
// button of the subscription card outside the card, wordmark without a gap to the
// theme button. happy-dom computes no layout, so the test checks the
// CSS rules that prevent the overflow (in the browser at 390/1440 px, light
// and dark, additionally measured via scrollWidth <= innerWidth).

const SCHMAL = 390

// Content of a block from the opening brace on; nested braces
// are counted too (for @media blocks).
function blockAt(source: string, openIndex: number) {
  let depth = 0
  for (let i = openIndex; i < source.length; i++) {
    if (source[i] === '{') depth++
    if (source[i] === '}' && --depth === 0) return source.slice(openIndex + 1, i)
  }
  return ''
}

function styleOf(source: string) {
  return source.slice(source.indexOf('<style'))
}

// All @media (max-width: N px) blocks that apply at 390 px.
function narrowMediaCss(source: string) {
  const css = styleOf(source)
  let out = ''
  for (const m of css.matchAll(/@media \(max-width: (\d+)px\) \{/g)) {
    if (Number(m[1]) >= SCHMAL) out += blockAt(css, m.index + m[0].length - 1) + '\n'
  }
  return out
}

// Declarations of a rule with exactly this selector (start of line).
function rule(css: string, selector: string) {
  const esc = selector.replaceAll('.', '\\.').replaceAll(' ', '\\s+')
  const m = css.match(new RegExp(`(^|\\n)\\s*${esc} \\{([^}]*)\\}`))
  return m ? m[2] : ''
}

describe('Statusseite bei 390 px', () => {
  describe('Komponentenzeile (ComponentTreeNode)', () => {
    const narrow = narrowMediaCss(treeNodeSource)

    it('bricht bei schmaler Ansicht um, statt rechts überzulaufen', () => {
      expect(rule(narrow, '.tree-row')).toMatch(/flex-wrap:\s*wrap/)
    })

    it('Name darf schrumpfen und umbrechen (sonst schiebt er das Badge hinaus)', () => {
      const name = rule(narrow, '.tree-name')
      expect(name).toMatch(/min-width:\s*0/)
      expect(name).toMatch(/white-space:\s*normal/)
      expect(name).toMatch(/overflow-wrap:\s*anywhere/)
    })

    it('Spacer erzwingt die neue Zeile für Tag und Badge', () => {
      expect(rule(narrow, '.tree-spacer')).toMatch(/flex:\s*0 0 100%/)
    })

    it('breite Ansicht bleibt einzeilig', () => {
      expect(rule(styleOf(treeNodeSource), '.tree-row')).not.toMatch(/flex-wrap/)
    })
  })

  describe('Abo-Karte (StatusPage)', () => {
    const css = styleOf(statusPageSource)

    it('Aktionen stapeln sich unter dem Text, statt aus der Karte zu ragen', () => {
      expect(rule(css, '.subscribe-cta')).toMatch(/flex-wrap:\s*wrap/)
      expect(rule(css, '.subscribe-cta-actions')).toMatch(/flex-wrap:\s*wrap/)
      expect(rule(css, '.subscribe-cta-actions')).toMatch(/max-width:\s*100%/)
    })

    it('Text darf auf Kartenbreite schrumpfen', () => {
      expect(rule(css, '.subscribe-cta-text')).toMatch(/min-width:\s*0/)
    })
  })

  // At 320 px the global .search-input min-width (180px content + 62px
  // padding) made the input wider than the wrapper and pushed the "?" help
  // button 11px past the viewport (e2e/public-schmal.spec.ts).
  describe('Suchzeile (StatusPage)', () => {
    const css = styleOf(statusPageSource)

    it('Suchfeld darf bis auf die Wrapper-Breite schrumpfen', () => {
      const input = rule(css, '.search-wrapper .search-input')
      expect(input).toMatch(/min-width:\s*0/)
      expect(input).toMatch(/flex:\s*1 1 auto/)
    })

    it('Hilfe-Knopf schrumpft nicht und bleibt neben dem Feld', () => {
      expect(rule(css, '.search-help-btn')).toMatch(/flex-shrink:\s*0/)
    })

    // The clear button is positioned against the wrapper, which also holds
    // the help button; with right: var(--sp-2) the "x" sat on top of the "?".
    it('Leeren-Knopf sitzt im Feld, nicht über dem Hilfe-Knopf', () => {
      expect(rule(css, '.search-wrapper')).toMatch(/--search-help-size:\s*24px/)
      const btn = rule(css, '.search-help-btn')
      expect(btn).toMatch(/width:\s*var\(--search-help-size\)/)
      expect(btn).toMatch(/height:\s*var\(--search-help-size\)/)
      expect(rule(css, '.search-clear')).toMatch(
        /right:\s*calc\(var\(--search-help-size\) \+ 3 \* var\(--sp-2\)\)/,
      )
    })
  })

  describe('Kopfzeile (PublicLayout)', () => {
    it('Wortmarke hat Abstand zum Theme-Knopf', () => {
      const gap = rule(styleOf(publicLayoutSource), '.app-header').match(/gap:\s*(\d+)px/)
      expect(Number(gap?.[1])).toBeGreaterThan(0)
    })
  })
})
