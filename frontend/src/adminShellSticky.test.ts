import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname } from 'path'

// Regression 2026-10-29: the admin sidebar (now the tab header) scrolled away with the content
// and ended in the middle of the page. Cause: overflow-x: hidden on
// .admin-shell makes it a scroll container, so sticky elements (header, tabs)
// stick to it instead of the window. happy-dom computes no layout, so the
// test checks the rules themselves: no ancestor of the sticky elements may
// have overflow hidden/auto/scroll (clip is allowed, it creates no scroll
// container).
const css = readFileSync(`${dirname(fileURLToPath(import.meta.url))}/style.css`, 'utf-8')

function rule(selector: string) {
  const m = css.match(new RegExp(`(^|\\n)${selector.replace('.', '\\.')} \\{([^}]*)\\}`))
  return m ? m[2] : ''
}

describe('Admin-Layout: sticky Kopfzeile mit Reitern', () => {
  // The sidebar gave way to the tabs; header, tabs and entry row sit
  // together in .admin-topbar and stay in place as one block.
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
