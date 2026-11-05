import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve, relative, join } from 'path'

// Ratsche gegen natives confirm()/alert()/prompt(). Rückfragen laufen über
// composables/useConfirm.js (confirmDialog) und components/ConfirmDialog.vue.
// Kommentare und Tests zählen nicht.
const root = resolve(dirname(fileURLToPath(import.meta.url)))
const GLOBALS = '(?:window|globalThis|self|top|parent)'
const NAMES = '(?:confirm|alert|prompt)'
const NATIVE_PATTERNS = [
  // confirm(...), auch window.confirm(...), window?.confirm(...); Methoden anderer Objekte (api.confirm) und
  // Funktionsdefinitionen (function confirm()) zählen nicht
  new RegExp(`(?<![\\w$.])(?<!function\\s)${NAMES}\\s*\\(`),
  // window.confirm / globalThis?.alert auch ohne Aufruf (Zuweisung, Weitergabe)
  new RegExp(`${GLOBALS}\\s*(?:\\?\\.|\\.)\\s*${NAMES}\\b`),
  // window["confirm"](...)
  new RegExp(`${GLOBALS}\\s*(?:\\?\\.)?\\s*\\[\\s*['"\`]${NAMES}['"\`]\\s*\\]`),
  // const { confirm } = window
  new RegExp(`\\{[^}]*\\b${NAMES}\\b[^}]*\\}\\s*=\\s*${GLOBALS}\\b`),
]
const NATIVE = { test: (line) => NATIVE_PATTERNS.some((re) => re.test(line)) }

function sourceFiles(dir = root) {
  const files = []
  for (const name of readdirSync(dir)) {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) files.push(...sourceFiles(full))
    else if (/\.(vue|js)$/.test(name) && !/\.(test|spec)\.js$/.test(name) && !full.includes('test-support')) files.push(full)
  }
  return files
}

export function stripComments(src) {
  return src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/<!--[\s\S]*?-->/g, '').replace(/(^|[^:'"`\\])\/\/.*$/gm, '$1')
}

export function findNativeDialogs(src) {
  return stripComments(src).split('\n').filter((l) => NATIVE.test(l))
}

describe('keine nativen Browser-Dialoge', () => {
  it('src enthält kein confirm()/alert()/prompt()', () => {
    const hits = sourceFiles().flatMap((f) => findNativeDialogs(readFileSync(f, 'utf8')).map((l) => `${relative(root, f)}: ${l.trim()}`))
    expect(hits).toEqual([])
  })

  it('Gegenprobe: der Prüfer schlägt bei neuen Stellen an und ignoriert Kommentare und Methoden', () => {
    const verboten = [
      "if (!confirm('x')) return",
      "if (!window.confirm('x')) return",
      "window?.confirm('x')",
      "window['confirm']('x')",
      'window?.["confirm"]("x")',
      "self.confirm('x')",
      "globalThis.confirm('x')",
      'const f = globalThis.confirm',
      'const { confirm } = window',
      'const { alert, confirm: c } = globalThis',
      "window.alert('x')",
      "const ok = prompt('x')",
    ]
    for (const code of verboten) expect(findNativeDialogs(code), code).toHaveLength(1)
    expect(findNativeDialogs("// confirm('x') war früher so\n/* confirm('y') */")).toEqual([])
    expect(findNativeDialogs('endpoints.confirm(id)\nawait confirmDialog({})')).toEqual([])
    expect(findNativeDialogs('async function confirm() {\n  state.value = 1\n}')).toEqual([])
    expect(findNativeDialogs('const { confirmed } = window.state')).toEqual([])
  })
})
