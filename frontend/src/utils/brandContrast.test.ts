import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { BRAND_ON_PRIMARY, BRAND_SECONDARY_BACKGROUNDS } from './brandContrast'

// 4a: the calculation inputs for the brand contrast must match the
// tokens of the "public" layer, otherwise ensureContrast checks against the
// wrong surface.
const css = readFileSync(resolve(__dirname, '../assets/tokens.css'), 'utf-8')

const tokenIn = (block: string, name: string) => block.match(new RegExp(`${name}:\\s*(#[0-9a-fA-F]{6})`))?.[1].toLowerCase()
const lightBlock = css.slice(css.indexOf(':root,\n.theme-public {'), css.indexOf('/* ── 2b.'))
const darkBlock = css.slice(css.indexOf('html[data-theme="dark"],\nhtml[data-theme="dark"] .theme-public {'), css.indexOf('/* ── 3.'))

describe('brandContrast', () => {
  // The description (--text-secondary) sits in the hero on the tinted
  // status surfaces (-soft), not only on --surface; the brand colour was only checked against
  // --surface and, when dark, sat on --ok-soft at 4.32:1.
  const expected = (block: string) => ['--surface', '--surface-2', '--ok-soft', '--deg-soft', '--part-soft', '--major-soft', '--maint-soft']
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
