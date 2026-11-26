import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve, join } from 'path'

// The SSO specs targeted the header's sign-in button with
// `.app-header .onyx-button`. Since the redesign it is a plain <button>
// without an Onyx class; the nightly run only failed late. This fast test
// reads all E2E specs and requires every class in an `.app-header …`
// selector to appear in the template of PublicLayout.vue.
const src = resolve(dirname(fileURLToPath(import.meta.url)))
const e2eDir = resolve(src, '../e2e')
const layout = readFileSync(join(src, 'layouts/PublicLayout.vue'), 'utf8')
const template = layout.split('<style')[0]

const klassenImKopf = (source) =>
  [...source.matchAll(/locator\((['"`])([^'"`]*\.app-header[^'"`]*)\1\)/g)]
    .flatMap(m => [...m[2].matchAll(/\.([A-Za-z][\w-]*)/g)].map(c => c[1]))
const unbekannt = (source, tpl) => klassenImKopf(source).filter(k => !tpl.includes(k))

describe('E2E-Selektoren der Kopfzeile existieren', () => {
  it('erkennt die veraltete Klasse onyx-button', () => {
    expect(unbekannt("page.locator('.app-header .onyx-button').click()", template)).toEqual(['onyx-button'])
  })
  it('akzeptiert vorhandene Klassen', () => {
    expect(unbekannt("page.locator('.app-header .login-button')", template)).toEqual([])
  })
  for (const file of readdirSync(e2eDir).filter(f => f.endsWith('.js'))) {
    it(`${file}: Kopfzeilen-Klassen im Layout vorhanden`, () => {
      expect(unbekannt(readFileSync(join(e2eDir, file), 'utf8'), template)).toEqual([])
    })
  }
})
