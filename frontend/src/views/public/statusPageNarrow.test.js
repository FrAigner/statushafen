import { describe, it, expect } from 'vitest'
import statusPageSource from './StatusPage.vue?raw'
import treeNodeSource from '../../components/ComponentTreeNode.vue?raw'
import publicLayoutSource from '../../layouts/PublicLayout.vue?raw'

// Die öffentliche Statusseite scrollte bei 390 px horizontal
// (scrollWidth 471 statt 390): Status-Badges rechts abgeschnitten, Hinweis und
// Button der Abo-Karte außerhalb der Karte, Wortmarke ohne Abstand zum
// Theme-Knopf. happy-dom rechnet kein Layout, deshalb prüft der Test die
// CSS-Regeln, die das Überlaufen verhindern (im Browser bei 390/1440 px, hell
// und dunkel, zusätzlich per scrollWidth <= innerWidth gemessen).

const SCHMAL = 390

// Inhalt eines Blocks ab der öffnenden Klammer, verschachtelte Klammern
// werden mitgezählt (für @media-Blöcke).
function blockAt(source, openIndex) {
  let depth = 0
  for (let i = openIndex; i < source.length; i++) {
    if (source[i] === '{') depth++
    if (source[i] === '}' && --depth === 0) return source.slice(openIndex + 1, i)
  }
  return ''
}

function styleOf(source) {
  return source.slice(source.indexOf('<style'))
}

// Alle @media (max-width: N px)-Blöcke, die bei 390 px greifen.
function narrowMediaCss(source) {
  const css = styleOf(source)
  let out = ''
  for (const m of css.matchAll(/@media \(max-width: (\d+)px\) \{/g)) {
    if (Number(m[1]) >= SCHMAL) out += blockAt(css, m.index + m[0].length - 1) + '\n'
  }
  return out
}

// Deklarationen einer Regel mit genau diesem Selektor (Zeilenanfang).
function rule(css, selector) {
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

  describe('Kopfzeile (PublicLayout)', () => {
    it('Wortmarke hat Abstand zum Theme-Knopf', () => {
      const gap = rule(styleOf(publicLayoutSource), '.app-header').match(/gap:\s*(\d+)px/)
      expect(Number(gap?.[1])).toBeGreaterThan(0)
    })
  })
})
