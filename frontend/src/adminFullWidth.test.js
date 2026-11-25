import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// Admin pages use the full width of the content area (like the system settings); a fixed
// max-width left a wide empty strip on the right on large screens.
const root = resolve(dirname(fileURLToPath(import.meta.url)))
const styleOf = (rel) => readFileSync(resolve(root, rel), 'utf-8').split('<style scoped>')[1] ?? ''
const rule = (css, selector) => css.match(new RegExp(`(^|\\n)${selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')} \\{([^}]*)\\}`))?.[2] ?? ''

describe('admin pages use the full width', () => {
  it('SSO configuration has no max-width', () => {
    expect(styleOf('views/admin/AdminSSO.vue')).not.toMatch(/max-width:\s*\d/)
  })

  it('incident detail has no max-width', () => {
    expect(styleOf('views/admin/AdminIncidentDetail.vue')).not.toMatch(/\.wrap \{[^}]*max-width:\s*\d/)
  })

  it('docs inside the admin frame drop the landing content width', () => {
    expect(rule(styleOf('views/public/DocsPage.vue'), '.docs--embedded')).toMatch(/max-width:\s*none/)
  })
})

// Long edit forms (status page settings) scroll with the whole page, not inside their own frame.
describe('edit panels scroll with the page', () => {
  const css = readFileSync(resolve(root, 'style.css'), 'utf-8')
  const statusPages = readFileSync(resolve(root, 'views/admin/AdminStatusPages.vue'), 'utf-8')

  it('has no form panel with its own scroll area', () => {
    const panelRules = [...css.matchAll(/([^{};]*\.form-panel[^{};]*)\{([^{}]*)\}/g)]
    for (const [, selector, body] of panelRules) {
      expect(body, selector.trim()).not.toMatch(/overflow(-y)?:\s*(auto|scroll)/)
    }
    expect(statusPages).not.toContain('form-panel--scroll')
  })

  it('does not keep a tall edit form sticky, so its end can be reached by scrolling the page', () => {
    expect(statusPages).toContain('class="form-panel--tall"')
    expect(css).toMatch(/\.form-panel--tall \{[^}]*position:\s*static/)
  })
})
