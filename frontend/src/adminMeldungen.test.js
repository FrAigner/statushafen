import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

// Meldungen im Admin-Bereich sind im Sichtbereich und blenden sich aus – zentral, nicht je Seite.
const here = dirname(fileURLToPath(import.meta.url))
const css = readFileSync(join(here, 'style.css'), 'utf-8')

describe('Admin-Meldungen', () => {
  it('bleiben beim Scrollen im Sichtbereich (sticky unter dem Rand des Scrollbereichs)', () => {
    const m = css.match(/\.admin-content \.alert \{([^}]*)\}/)
    expect(m).not.toBeNull()
    expect(m[1]).toMatch(/position:\s*sticky/)
    expect(m[1]).toMatch(/top:\s*var\(--space-sm\)/)
  })

  it('blenden sich über den einen Baustein im AdminLayout aus, keine Ansicht baut eine eigene Frist', () => {
    const layout = readFileSync(join(here, 'layouts/AdminLayout.vue'), 'utf-8')
    expect(layout).toContain('useAutoDismissAlerts(contentRef)')
    const dir = join(here, 'views/admin')
    const own = readdirSync(dir).filter(f => f.endsWith('.vue') && /useAutoDismissAlerts|ALERT_AUTO_DISMISS/.test(readFileSync(join(dir, f), 'utf-8')))
    expect(own).toEqual([])
  })
})
