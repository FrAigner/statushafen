import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { BRAND_ON_PRIMARY, BRAND_SECONDARY_BACKGROUNDS } from './brandContrast'

// 4a: Die Rechengrundlagen für den Marken-Kontrast müssen den
// Tokens der Ebene „public“ entsprechen, sonst prüft ensureContrast gegen die
// falsche Fläche.
const css = readFileSync(resolve(__dirname, '../assets/tokens.css'), 'utf-8')

const tokenIn = (block, name) => block.match(new RegExp(`${name}:\\s*(#[0-9a-fA-F]{6})`))?.[1].toLowerCase()
const lightBlock = css.slice(css.indexOf(':root,\n.theme-public {'), css.indexOf('/* ── 2b.'))
const darkBlock = css.slice(css.indexOf('html[data-theme="dark"],\nhtml[data-theme="dark"] .theme-public {'), css.indexOf('/* ── 3.'))

describe('brandContrast', () => {
  // Die Beschreibung (--text-secondary) steht im Hero auf den getönten
  // Statusflächen (-soft), nicht nur auf --surface; die Markenfarbe wurde nur gegen
  // --surface geprüft und lag dunkel auf --ok-soft bei 4,32:1.
  const expected = (block) => ['--surface', '--surface-2', '--ok-soft', '--deg-soft', '--part-soft', '--major-soft', '--maint-soft']
    .map(name => tokenIn(block, name))

  it('entspricht Oberflächen und Status-Tönungen der Ebene public (hell)', () => {
    expect(BRAND_SECONDARY_BACKGROUNDS.light).toEqual(expected(lightBlock))
  })

  it('entspricht Oberflächen und Status-Tönungen der Ebene public (dunkel)', () => {
    expect(BRAND_SECONDARY_BACKGROUNDS.dark).toEqual(expected(darkBlock))
  })

  it('entspricht --on-primary', () => {
    expect(BRAND_ON_PRIMARY).toBe(tokenIn(lightBlock, '--on-primary'))
  })
})
