import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { createRequire } from 'module'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// OnyxButton, OnyxInput label/text, OnyxSelect etc. appeared in a serif
// font. In its components sit-onyx sets
// font-family: var(--onyx-font-family-paragraph) (or -h1 … -mono) without a
// generic fallback, and the tokens point to fonts that onyx does not ship
// and we do not load. happy-dom computes no CSS, so the test checks the
// sources: every font variable onyx uses in a font-family declaration must
// point, in tokens.css on :root, to one of our self-hosted fonts.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const require = createRequire(import.meta.url)
const onyxCss = readFileSync(require.resolve('sit-onyx/style.css'), 'utf-8')
const tokensCss = readFileSync(resolve(root, 'src/assets/tokens.css'), 'utf-8')

const OUR_FONTS = new Set(['var(--font-sans)', 'var(--font-mono)'])

function onyxFontVariables(css) {
  const names = new Set()
  for (const [, value] of css.matchAll(/font-family:\s*([^;}]+)/g)) {
    for (const [, name] of value.matchAll(/var\((--onyx-font-family-[\w-]+)/g)) names.add(name)
  }
  return [...names].sort()
}

function rootBlocks(css) {
  const withoutComments = css.replace(/\/\*[\s\S]*?\*\//g, '')
  return [...withoutComments.matchAll(/(?:^|[};])\s*:root\s*{([^}]*)}/g)].map(m => m[1]).join('\n')
}

function rootValue(css, name) {
  const m = rootBlocks(css).match(new RegExp(`${name}:\\s*([^;]+);`))
  return m ? m[1].trim() : null
}

describe('sit-onyx nutzt unsere Schriften', () => {
  const vars = onyxFontVariables(onyxCss)

  it('findet die Schrift-Variablen, die onyx in font-family verwendet', () => {
    // Buttons, inputs, selects: paragraph font; headings and mono as well.
    expect(vars).toEqual(expect.arrayContaining([
      '--onyx-font-family-paragraph',
      '--onyx-font-family-h1',
      '--onyx-font-family-mono',
    ]))
  })

  it('onyx setzt .onyx-button ohne generischen Fallback auf die Absatzschrift', () => {
    // Pins the assumption the fix is based on.
    expect(onyxCss).toMatch(/\.onyx-button\s*{[^}]*font-family:\s*var\(--onyx-font-family-paragraph\);/)
  })

  it.each(onyxFontVariables(onyxCss))('%s ist in tokens.css auf :root auf unsere Schrift gesetzt', (name) => {
    expect(OUR_FONTS).toContain(rootValue(tokensCss, name))
  })

  it('Gegenprobe: ohne Override (alter Stand) schlägt die Prüfung an', () => {
    const old = tokensCss.replace(/--onyx-font-family-[\w-]+:[^;]+;/g, '')
    expect(rootValue(old, '--onyx-font-family-paragraph')).toBeNull()
    // A value in a selector other than :root does not count.
    expect(rootValue('.x { --onyx-font-family-h1: var(--font-sans); }', '--onyx-font-family-h1')).toBeNull()
    expect(rootValue(':root { --onyx-font-family-h1: var(--font-sans); }', '--onyx-font-family-h1')).toBe('var(--font-sans)')
  })
})
