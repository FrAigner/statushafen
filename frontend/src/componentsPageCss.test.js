import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { dirname, resolve } from 'path'
import { fileURLToPath } from 'url'

const raw = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'style.css'), 'utf8')

// Kommentare enthalten Klammern und Semikolons und würden die Regelsuche stören.
const css = raw.replace(/\/\*[\s\S]*?\*\//g, '')

// Komponentenseite nach Richtung C: happy-dom rechnet kein CSS aus
// Dateien. Diese Tests prüfen deshalb den Quelltext von style.css auf die
// Deklarationen, deren Fehlen drei sichtbare Fehler verursacht hat:
//  1. Das Seitenformular saß ~150 px unter der Oberkante der Liste.
//  2. Der Sprachwähler im Admin-Kopf zeigte den Browser-Pfeil.
//  3. .action-btn als Link (<a>) war anders groß als als <button>.

// Alle Regelblöcke, deren Selektorliste `selector` als eigenen Selektor enthält.
function blocks(selector) {
  const found = []
  for (const m of css.matchAll(/([^{}@]+)\{([^{}]*)\}/g)) {
    const selectors = m[1].split(',').map(s => s.trim())
    if (selectors.includes(selector)) found.push(m[2])
  }
  return found
}
const declared = (selector) => blocks(selector).join(';')

describe('style.css: Seitenformular (.form-panel)', () => {
  it('klebt 16 px unter dem Scrollbereich, ohne die Topbar-Höhe noch einmal abzuziehen', () => {
    const rule = declared('.form-panel')
    expect(rule).toMatch(/position:\s*sticky/)
    // Der Scrollbereich (main.admin-content) beginnt unter der Topbar; 122 px
    // obendrauf schoben das Formular unter die Listen-Oberkante.
    expect(rule).toMatch(/inset-block-start:\s*var\(--sp-4\)\s*;/)
    expect(rule).not.toMatch(/122px/)
  })
})

describe('style.css: Sprachwähler in der Admin-Kopfzeile', () => {
  it('teilt sich Höhe, Rand und Hintergrund mit Theme-Knopf und Mandanten-Auswahl', () => {
    const group = blocks('.admin-topbar select.language-switcher').find(b => /height:\s*var\(--ctl-h\)/.test(b))
    expect(group).toBeTruthy()
    expect(group).toMatch(/border:\s*1px solid/)
    expect(group).toMatch(/background:/)
    expect(blocks('.icon-btn').find(b => /height:\s*var\(--ctl-h\)/.test(b))).toBeTruthy()
  })

  it('hat einen eigenen Pfeil statt des Browser-Standards', () => {
    const rule = declared('.admin-topbar select.language-switcher')
    expect(rule).toMatch(/appearance:\s*none/)
    expect(rule).toMatch(/background-image:[^;]*linear-gradient[^;]*currentColor/)
    expect(rule).toMatch(/background-repeat:\s*no-repeat/)
  })

  it('lässt dem Pfeil Platz (rechtes Padding) auch unter 768 px', () => {
    expect(declared('.admin-topbar select.language-switcher')).toMatch(/padding:\s*0 28px 0 /)
    expect(css).toMatch(/@media \(max-width: 768px\)[\s\S]*select\.language-switcher,\s*\.admin-topbar select\.tenant-select \{ padding: 0 24px 0 /)
  })
})

describe('style.css: Seitenwähler (Mandanten-Auswahl) in der Admin-Kopfzeile', () => {
  it('hat wie der Sprachwähler einen eigenen Pfeil statt des Browser-Standards', () => {
    const rule = declared('.admin-topbar select.tenant-select')
    expect(rule).toMatch(/appearance:\s*none/)
    expect(rule).toMatch(/background-image:[^;]*linear-gradient[^;]*currentColor/)
    expect(rule).toMatch(/padding:\s*0 28px 0 /)
  })

  it('lässt dem Pfeil auch unter 768 px Platz', () => {
    expect(css).toMatch(/@media \(max-width: 768px\)[\s\S]*select\.tenant-select \{ padding: 0 24px 0 /)
  })
})

describe('style.css: .action-btn als Link und als Button', () => {
  it('legt Boxmodell, Zeilenhöhe und Unterstreichung fest, damit <a> und <button> gleich groß sind', () => {
    const rule = declared('.action-btn')
    expect(rule).toMatch(/box-sizing:\s*border-box/)
    expect(rule).toMatch(/line-height:\s*[\d.]+/)
    expect(rule).toMatch(/text-decoration:\s*none/)
    expect(rule).toMatch(/min-height:\s*44px/)
  })
})

describe('style.css: Felder und Tabelle der Komponentenseite', () => {
  it('Selects in FormField haben einen eigenen Chevron', () => {
    const rule = declared('.form-field__control select')
    expect(rule).toMatch(/appearance:\s*none/)
    expect(rule).toMatch(/linear-gradient/)
  })

  it('das Zeilenmenü und der Gruppenschalter erreichen die Zielgröße', () => {
    expect(declared('.form-panel__close')).toMatch(/inline-size:\s*44px/)
    expect(declared('.group-toggle')).toMatch(/inline-size:\s*28px/)
  })
})

// Betreiber-Wunsch 31.10.: Verhältnis Liste : Formular etwa 2 : 1 (vorher Formular höchstens 400 px).
describe('style.css: Verhältnis Liste zu Seitenformular (.split--open)', () => {
  it('Liste zwei Teile, Formular ein Teil mit Mindestbreite 360 px', () => {
    expect(declared('.split--open')).toMatch(/grid-template-columns:\s*minmax\(0,\s*2fr\)\s*minmax\(360px,\s*1fr\)/)
  })
  it('ab 859 px abwärts liegt das Formular weiter unter der Liste', () => {
    expect(css).toMatch(/@media[^{]*859px[^{]*\{[^@]*\.split--open\s*\{\s*grid-template-columns:\s*minmax\(0,\s*1fr\)\s*;?\s*\}/)
  })
})
