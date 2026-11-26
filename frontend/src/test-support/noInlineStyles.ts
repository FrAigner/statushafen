// Redesign Stufe 4b: Schutz gegen Rückfall in Inline-Styles und
// feste Hex-Farben in einer View. Geprüft werden das gerenderte DOM (kein
// style-Attribut) und der Quelltext (kein style="…", kein :style, kein Hex
// außerhalb von Kommentaren). Farben gehören in Tokens (assets/tokens.css).
import { readFileSync } from 'fs'
import { dirname, resolve } from 'path'
import { fileURLToPath } from 'url'
import { expect } from 'vitest'

/**
 * @param wrapper     gemounteter VTU-Wrapper der View (im Zustand mit Daten)
 * @param testFileUrl `import.meta.url` des Tests
 * @param viewFile    Dateiname der View neben dem Test, z. B. 'AdminOverview.vue'
 */
export function expectNoInlineStyles(wrapper, testFileUrl, viewFile) {
  // sit-onyx setzt intern Anker-Variablen per style (Tooltips); das ist nicht unser Markup.
  // Das Zeilenmenü (RowMenu) blendet per v-show aus und positioniert sich fix per style.
  const own = wrapper.findAll('[style]').filter(el => !el.element.closest('.onyx-component, .row-menu__list'))
  expect(own.map(el => el.html().slice(0, 120))).toEqual([])

  const source = readFileSync(resolve(dirname(fileURLToPath(testFileUrl)), viewFile), 'utf-8')
  const code = source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/<!--[\s\S]*?-->/g, '')
    .replace(/(^|\s)\/\/.*$/gm, '$1') // JS-Zeilenkommentare (dort stehen Verweisnummern wie #496)
  expect(code).not.toMatch(/\sstyle="/)
  expect(code).not.toMatch(/:style=/)
  expect(code).not.toMatch(/#[0-9a-fA-F]{3,8}\b/)
}
