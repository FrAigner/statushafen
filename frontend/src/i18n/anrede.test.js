import { describe, it, expect } from 'vitest'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import de from './locales/de.json'

// Rest: Das Projekt duzt. Dieser Wächter findet
// förmliche Anrede ("Sie", "Ihr...") in de.json, den deutschen Hilfeseiten und
// der Anleitung unter docs/user-guide.
//
// Konservative Regel (Pronomen "Sie" = 3. Person Plural/Singular ist mehrdeutig):
//  1. "Sie" nach einem kleingeschriebenen Wort/Komma ("Stellen Sie" ist über 2.
//     abgedeckt, "dass Sie", "wenn Sie", "bitten Sie"): mitten im Satz wäre das
//     Pronomen klein geschrieben, großes "Sie" ist also Anrede.
//  2. Imperativ/Höflichkeitsform am Satzanfang: Verb auf -en/-et + "Sie"
//     ("Stellen Sie", "Löschen Sie", "Lesen Sie"). Satzanfang "Sie sind/gelten"
//     bleibt unbeanstandet (Pronomen für Dinge).
//  3. Possessiv "Ihr/Ihre/Ihren/Ihrem/Ihrer/Ihres" und "Ihnen" in jeder Position.
// Berechtigte Treffer (Possessiv für Dinge, z. B. "Ihre Bausteine" einer Quelle)
// stehen in ERLAUBT als exakter Textausschnitt.
export const ERLAUBT = [
  'Ihre Bausteine', // Bausteine der Quelle, keine Anrede
  'Ihr Slug', // Slug der Systemseite
  'Ihr Status zählt', // Status der externen Quelle
  'Ihr letzter Status zählt solange nicht für', // dito
  'Ihre Werte werden', // Werte der Platzhalter
  'Ihre Felder entsprechen', // Felder der SSO-Seite
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
