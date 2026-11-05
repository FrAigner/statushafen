import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname } from 'path'

// Regression 2026-10-29: Die Admin-Seitenleiste (heute die Reiter-Kopfzeile) scrollte mit dem Inhalt weg
// und endete mitten auf der Seite. Ursache: overflow-x: hidden an
// .admin-shell macht sie zum Scroll-Container, sticky (Kopfzeile, Reiter)
// klebt dann an ihr statt am Fenster. happy-dom rechnet kein Layout, deshalb
// prüft der Test die Regeln selbst: Kein Vorfahr der sticky-Elemente darf
// overflow hidden/auto/scroll haben (clip ist erlaubt, es erzeugt keinen
// Scroll-Container).
const css = readFileSync(`${dirname(fileURLToPath(import.meta.url))}/style.css`, 'utf-8')

function rule(selector) {
  const m = css.match(new RegExp(`(^|\\n)${selector.replace('.', '\\.')} \\{([^}]*)\\}`))
  return m ? m[2] : ''
}

describe('Admin-Layout: sticky Kopfzeile mit Reitern', () => {
  // Die Seitenleiste ist den Reitern gewichen; Kopfzeile, Reiter und
  // Eintragszeile stehen zusammen in .admin-topbar und bleiben als Block stehen.
  it('Kopfzeile mit Reitern ist sticky', () => {
    expect(rule('.admin-topbar')).toMatch(/position:\s*sticky/)
    expect(rule('.admin-topbar')).toMatch(/top:\s*0/)
  })

  it.each(['.admin-shell', '.admin-body'])('%s erzeugt keinen Scroll-Container', (sel) => {
    const body = rule(sel)
    expect(body, sel).not.toBe('')
    expect(body).not.toMatch(/overflow(-x|-y)?:\s*(hidden|auto|scroll)/)
  })

  it('.admin-shell schneidet die mobile Seitenleiste weiterhin ab', () => {
    expect(rule('.admin-shell')).toMatch(/overflow-x:\s*clip/)
  })
})
