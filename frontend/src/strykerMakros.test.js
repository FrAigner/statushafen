// Nightly run failure 2026-10-29 (nightly mutation tests): Stryker instrumented `defineProps({...})` in
// OidcLoginButton.vue; the Vue compiler hoists the macros out of setup and rejects references to the local
// stryMutAct helpers ("defineProps() ... cannot reference locally declared variables"). The dry run
// failed and the whole frontend mutation test was red. Any mutated macro line triggers this, so EVERY
// macro in a .vue file must be surrounded by a "Stryker disable" comment.
import { describe, it, expect } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

// `[<(]` also matches TypeScript type arguments such as `defineProps<Props>()`.
const MAKRO = /\b(defineProps|defineEmits|defineModel|withDefaults)\s*[<(]/

function vueDateien(dir) {
  return readdirSync(dir).flatMap((name) => {
    const pfad = join(dir, name)
    if (statSync(pfad).isDirectory()) return vueDateien(pfad)
    return pfad.endsWith('.vue') ? [pfad] : []
  })
}

// Returns the macro lines not preceded (across the comment block) by a "Stryker disable".
function ungeschuetzteMakros(quelltext) {
  const zeilen = quelltext.split('\n')
  const fehler = []
  zeilen.forEach((zeile, i) => {
    if (zeile.trim().startsWith('//') || !MAKRO.test(zeile)) return
    let j = i - 1
    while (j >= 0 && zeilen[j].trim().startsWith('//') && !zeilen[j].includes('Stryker disable')) j--
    if (j < 0 || !zeilen[j].includes('Stryker disable')) fehler.push(i + 1)
  })
  return fehler
}

describe('Stryker-Schutz für Vue-Makros', () => {
  it('erkennt ein ungeschütztes defineProps (Regression OidcLoginButton)', () => {
    expect(ungeschuetzteMakros('const props = defineProps({\n  slug: String,\n})')).toEqual([1])
    expect(
      ungeschuetzteMakros('// Stryker disable all: Grund\nconst props = defineProps({\n})\n// Stryker restore all'),
    ).toEqual([])
    expect(ungeschuetzteMakros("// Stryker disable next-line all: x\nconst e = defineEmits(['a'])")).toEqual([])
    expect(ungeschuetzteMakros("// nur ein Hinweis\nconst e = defineEmits(['a'])")).toEqual([2])
  })

  // TypeScript SFCs declare macros with type arguments (`defineProps<Props>()`), and `withDefaults`
  // hoists its defaults object as well, so Stryker must not instrument those lines either.
  it('detects unprotected macros with TypeScript type arguments and withDefaults', () => {
    expect(ungeschuetzteMakros('const props = defineProps<{ slug: string }>()')).toEqual([1])
    expect(ungeschuetzteMakros("const emit = defineEmits<{ (e: 'save'): void }>()")).toEqual([1])
    expect(ungeschuetzteMakros('const model = defineModel<string>()')).toEqual([1])
    expect(
      ungeschuetzteMakros("const props = withDefaults(defineProps<Props>(), {\n  size: 'm',\n})"),
    ).toEqual([1])
    expect(ungeschuetzteMakros('const props = withDefaults(\n  defineProps<Props>(),\n  { size: 1 },\n)')).toEqual([1, 2])
    expect(
      ungeschuetzteMakros('// Stryker disable next-line all: hoisted macro\nconst props = defineProps<Props>()'),
    ).toEqual([])
  })

  it('schützt jedes Makro in src/**/*.vue vor der Stryker-Instrumentierung', () => {
    const ungeschuetzt = vueDateien('src').flatMap((datei) =>
      ungeschuetzteMakros(readFileSync(datei, 'utf8')).map((zeile) => `${datei}:${zeile}`),
    )
    expect(ungeschuetzt).toEqual([])
  })
})
