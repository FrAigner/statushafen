import { describe, it, expect } from 'vitest'
import { readFileSync, existsSync, readdirSync, statSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// Stage 4f: cleanup after the redesign. This test ensures that the removed
// dead classes, alias tokens and the AdminPageHead component do not come
// back. Anyone who needs them uses the successors (PageHeader with level=2,
// StatusBadge, level tokens).
const root = resolve(dirname(fileURLToPath(import.meta.url)))
const read = (rel: string) => readFileSync(resolve(root, rel), 'utf-8')
const css = [read('style.css'), read('admin.css')].join('\n')
const tokens = read('assets/tokens.css')

const TOTE_KLASSEN = [
  'card-header', 'badge-operational', 'badge-degraded', 'badge-partial', 'badge-major',
  'badge-investigating', 'badge-identified', 'badge-monitoring', 'badge-resolved',
  'status-pulse', 'status-banner', 'all-operational', 'some-issues', 'major-issues',
  'incident-header', 'incident-message', 'component-card', 'subscribe-card',
  'subscribe-title', 'subscribe-desc', 'subscribe-form', 'dashboard-grid', 'section-header',
  'text-danger', 'text-success', 'fw-600', 'd-flex', 'align-center', 'gap-8', 'gap-12',
  'mt-16', 'ent-kpi-value', 'row-actions', 'cell-nowrap', 'cell-indent', 'sidebar',
]

const TOTE_TOKENS = [
  'text-xs', 'text-sm', 'text-base', 'text-lg', 'text-xl',
  'space-xs', 'space-lg', 'space-xl', 'gap-md',
  'sidebar-w', 'sidebar-width', 'header-height',
  'status-shape-operational', 'status-shape-degraded', 'status-shape-partial',
  'status-shape-major', 'status-shape-maintenance',
  'bg-active', 'bg-color-alt', 'bg-card', 'text-color',
  'status-maintenance', 'status-maintenance-text',
]

describe('Design-Aufräumen (Stufe 4f)', () => {
  it.each(TOTE_KLASSEN)('style.css/admin.css enthalten keine Regel für .%s mehr', (klasse) => {
    expect(css).not.toMatch(new RegExp(`\\.${klasse}(?![\\w-])`))
  })

  it.each(TOTE_TOKENS)('tokens.css definiert --%s nicht mehr', (token) => {
    expect(tokens).not.toMatch(new RegExp(`--${token}\\s*:`))
  })

  it('AdminPageHead ist durch PageHeader (level=2) ersetzt und gelöscht', () => {
    expect(existsSync(resolve(root, 'components/AdminPageHead.vue'))).toBe(false)
    expect(existsSync(resolve(root, 'components/PageHeader.vue'))).toBe(true)
  })

  // The old alias names were removed from tokens.css and replaced by the
  // level tokens in all views/components. The only remaining alias is
  // --text-secondary, the hook for the customer brand (StatusPage.vue).
  describe('entfernte Alias-Tokens', () => {
    const ALIASE = [
      'bg-app', 'bg-panel', 'bg-subtle', 'bg-hover', 'bg-color', 'panel-bg',
      'border-color', 'border-focus', 'text-primary', 'text-muted', 'text-inverse',
      'color-primary', 'color-primary-hover', 'color-primary-bg', 'color-primary-text',
      'status-operational', 'status-degraded', 'status-partial', 'status-major',
      'status-operational-text', 'status-degraded-text', 'status-partial-text', 'status-major-text',
      'color-success', 'color-warning', 'color-danger', 'color-info',
      'color-success-text', 'color-warning-text', 'color-danger-text', 'color-info-text',
      'shadow', 'shadow-sm', 'shadow-md',
    ]
    const quellen: string[] = []
    const sammeln = (dir: string) => {
      for (const name of readdirSync(dir)) {
        const pfad = resolve(dir, name)
        if (statSync(pfad).isDirectory()) sammeln(pfad)
        else if (/\.(vue|js|css)$/.test(name) && !/\.test\.js$/.test(name)) quellen.push(pfad)
      }
    }
    sammeln(root)
    // Comments may mention the old names (explanation in tokens.css).
    const alles = quellen.map((p) => readFileSync(p, 'utf-8')).join('\n').replace(/\/\*[\s\S]*?\*\//g, '')

    it('findet die Quelldateien (Schutz gegen leere Suche)', () => {
      expect(quellen.length).toBeGreaterThan(50)
      expect(alles).toContain('var(--surface)')
    })

    it.each(ALIASE)('--%s wird weder definiert noch verwendet', (alias) => {
      expect(alles).not.toMatch(new RegExp(`--${alias}(?![\\w-])`))
    })
  })
})
