import { describe, it, expect } from 'vitest'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import de from './locales/de.json'

// Remainder: the project uses informal address (du). This guard finds
// formal address ("Sie", "Ihr...") in de.json, the German help pages and
// the guide under docs/user-guide.
//
// Conservative rule (the pronoun "Sie" = 3rd person plural/singular is ambiguous):
//  1. "Sie" after a lowercase word/comma ("Stellen Sie" is covered by 2.,
//     "dass Sie", "wenn Sie", "bitten Sie"): mid-sentence the
//     pronoun would be lowercase, so a capital "Sie" is formal address.
//  2. Imperative/polite form at the start of a sentence: verb in -en/-et + "Sie"
//     ("Stellen Sie", "Löschen Sie", "Lesen Sie"). A sentence start "Sie sind/gelten"
//     is not flagged (pronoun for things).
//  3. Possessive "Ihr/Ihre/Ihren/Ihrem/Ihrer/Ihres" and "Ihnen" in any position.
// Legitimate hits (possessive for things, e.g. "Ihre Bausteine" of a source)
// are listed in ERLAUBT as an exact text excerpt.
export const ERLAUBT = [
  'Ihre Bausteine', // building blocks of the source, not an address
  'Ihr Slug', // slug of the system page
  'Ihr Status zählt', // status of the external source
  'Ihr letzter Status zählt solange nicht für', // ditto
  'Ihre Werte werden', // values of the placeholders
  'Ihre Felder entsprechen', // fields of the SSO page
  'Ihr Slug lässt sich',
]

const MITTEN_IM_SATZ = /(?<=[\p{Ll}\d,;)] )Sie\b/u
const IMPERATIV = /(?:^|[.!?:»"„]\s*|\n\s*[-*\d.]*\s*)\p{Lu}\p{Ll}+(?:en|et) Sie\b/u
const POSSESSIV = /\b(?:Ihr(?:e|en|em|er|es)?|Ihnen)\b/u

export function anredeTreffer(text) {
  let rest = text
  for (const ok of ERLAUBT) rest = rest.split(ok).join('')
  const hits = []
  for (const re of [MITTEN_IM_SATZ, IMPERATIV, POSSESSIV]) {
    const m = re.exec(rest)
    if (m) hits.push(m[0].trim())
  }
  return hits
}

function values(obj) {
  return Object.values(obj).flatMap(v => (v && typeof v === 'object' ? values(v) : [String(v)]))
}

function mdFiles(dir) {
  return readdirSync(dir).filter(f => f.endsWith('.md')).map(f => resolve(dir, f))
}

describe('Anrede-Wächter', () => {
  it('Gegenprobe: erkennt förmliche Anrede', () => {
    expect(anredeTreffer('Stellen Sie sicher, dass es klappt.')).not.toEqual([])
    expect(anredeTreffer('Bitte lesen Sie die Version.')).not.toEqual([])
    expect(anredeTreffer('Wenn Sie fertig sind.')).not.toEqual([])
    expect(anredeTreffer('Das betrifft Ihre Komponenten.')).not.toEqual([])
    expect(anredeTreffer('Erst so.\nLöschen Sie diese zuerst.')).not.toEqual([])
  })

  it('lässt Pronomen für Dinge und erlaubte Ausschnitte durch', () => {
    expect(anredeTreffer('Sie gelten sofort auf der Statusseite.')).toEqual([])
    expect(anredeTreffer('Die Liste ist leer. Sie wird gefüllt.')).toEqual([])
    expect(anredeTreffer('Quelle löschen? Ihre Bausteine werden entfernt.')).toEqual([])
    expect(anredeTreffer('Stelle sicher, dass du fertig bist.')).toEqual([])
  })

  it('de.json duzt', () => {
    const hits = values(de).filter(v => anredeTreffer(v).length)
    expect(hits).toEqual([])
  })

  it('deutsche Hilfeseiten und Anleitung duzen', () => {
    const dateien = [
      ...mdFiles(resolve(__dirname, '../docs/de')),
      ...mdFiles(resolve(__dirname, '../../../docs/user-guide')),
    ]
    expect(dateien.length).toBeGreaterThan(10)
    const hits = dateien.flatMap(f =>
      readFileSync(f, 'utf8').split('\n')
        .filter(l => anredeTreffer(l).length)
        .map(l => `${f.split('/').pop()}: ${l.trim().slice(0, 80)}`))
    expect(hits).toEqual([])
  })
})
