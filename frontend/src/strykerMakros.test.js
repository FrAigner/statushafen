// Nachtlauf-Fehler 2026-10-29 (Mutationstests nachts): Stryker instrumentierte `defineProps({...})` in
// OidcLoginButton.vue; der Vue-Compiler hebt die Makros aus dem Setup und lehnt Verweise auf die lokalen
// stryMutAct-Hilfen ab ("defineProps() ... cannot reference locally declared variables"). Der Dry-Run
// scheiterte, der ganze Frontend-Mutationstest war rot. Jede geänderte Makro-Zeile löst das aus, deshalb muss
// JEDES Makro in einer .vue-Datei von einem "Stryker disable"-Kommentar umgeben sein.
import { describe, it, expect } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

const MAKRO = /\b(defineProps|defineEmits|defineModel)\(/

function vueDateien(dir) {
  return readdirSync(dir).flatMap((name) => {
    const pfad = join(dir, name)
    if (statSync(pfad).isDirectory()) return vueDateien(pfad)
    return pfad.endsWith('.vue') ? [pfad] : []
  })
}

// Liefert die Makro-Zeilen, vor denen (über den Kommentarblock hinweg) kein "Stryker disable" steht.
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

  it('schützt jedes Makro in src/**/*.vue vor der Stryker-Instrumentierung', () => {
    const ungeschuetzt = vueDateien('src').flatMap((datei) =>
      ungeschuetzteMakros(readFileSync(datei, 'utf8')).map((zeile) => `${datei}:${zeile}`),
    )
    expect(ungeschuetzt).toEqual([])
  })
})
