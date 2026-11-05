import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { createRequire } from 'module'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// OnyxButton, OnyxInput-Label/-Text, OnyxSelect usw. erschienen in
// einer Serifenschrift. sit-onyx setzt in seinen Komponenten
// font-family: var(--onyx-font-family-paragraph) (bzw. -h1 … -mono) ohne
// generischen Fallback, und die Tokens zeigen auf Schriften, die onyx nicht
// mitliefert und wir nicht laden. happy-dom rechnet kein CSS, deshalb prüft
// der Test die Quellen: Jede Schrift-Variable, die onyx in einer
// font-family-Deklaration benutzt, muss in tokens.css auf :root auf eine
// unserer selbst gehosteten Schriften zeigen.
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
    // Buttons, Inputs, Selects: Absatzschrift; Überschriften und Mono ebenfalls.
    expect(vars).toEqual(expect.arrayContaining([
      '--onyx-font-family-paragraph',
      '--onyx-font-family-h1',
      '--onyx-font-family-mono',
    ]))
  })

  it('onyx setzt .onyx-button ohne generischen Fallback auf die Absatzschrift', () => {
    // Hält die Annahme fest, auf der der Fix beruht.
    expect(onyxCss).toMatch(/\.onyx-button\s*{[^}]*font-family:\s*var\(--onyx-font-family-paragraph\);/)
  })

  it.each(onyxFontVariables(onyxCss))('%s ist in tokens.css auf :root auf unsere Schrift gesetzt', (name) => {
    expect(OUR_FONTS).toContain(rootValue(tokensCss, name))
  })

  it('Gegenprobe: ohne Override (alter Stand) schlägt die Prüfung an', () => {
    const old = tokensCss.replace(/--onyx-font-family-[\w-]+:[^;]+;/g, '')
    expect(rootValue(old, '--onyx-font-family-paragraph')).toBeNull()
    // Ein Wert in einem anderen Selektor als :root zählt nicht.
    expect(rootValue('.x { --onyx-font-family-h1: var(--font-sans); }', '--onyx-font-family-h1')).toBeNull()
    expect(rootValue(':root { --onyx-font-family-h1: var(--font-sans); }', '--onyx-font-family-h1')).toBe('var(--font-sans)')
  })
})
