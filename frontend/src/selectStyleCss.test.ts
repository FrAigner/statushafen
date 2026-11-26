import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'fs'
import { dirname, resolve, join, relative } from 'path'
import { fileURLToPath } from 'url'

// One look for every native <select> outside sit-onyx. Before, the filter selects in the card
// toolbars (.comp-filter, e.g. UI texts "Global / Deutsch / Alle Bereiche") and the page-size
// select of the pager showed the browser arrow, the pager select was 56 px tall, the per-page
// role select in the user form was completely unstyled (Arial, grey in dark mode) and the
// subscription manager and incident follow-ups carried their own copies. happy-dom does not
// compute CSS from files, so these tests pin the source; the real rendering was checked in
// Chromium (light/dark, 1280 and 390 px).

const src = dirname(fileURLToPath(import.meta.url))
const read = (...p: string[]) => readFileSync(resolve(src, ...p), 'utf8')
const stripComments = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '')
const css = stripComments(read('style.css'))
const blocks = [...css.matchAll(/(?:^|\})\s*([^{}@]+)\{([^{}]*)\}/g)]
  .map(m => ({ selectors: m[1].split(',').map(s => s.trim()), body: m[2] }))

const SHARED = ['.form-field__control select', '.form-select', '.comp-filter']

function vueFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e): string[] =>
    e.isDirectory() ? vueFiles(join(dir, e.name)) : e.name.endsWith('.vue') ? [join(dir, e.name)] : [])
}

describe('shared native select style', () => {
  const shared = blocks.find(b => SHARED.every(s => b.selectors.includes(s)))!

  it('one rule covers form fields, .form-select and the toolbar filters', () => {
    expect(shared).toBeDefined()
  })

  it('draws the app chevron instead of the browser arrow', () => {
    expect(shared.body).toMatch(/(^|;)\s*appearance:\s*none/)
    expect(shared.body).toMatch(/-webkit-appearance:\s*none/)
    expect(shared.body).toMatch(/background-image:[^;]*linear-gradient[^;]*currentColor/)
    expect(shared.body).toMatch(/background-repeat:\s*no-repeat/)
    expect(shared.body).toMatch(/padding-inline-end:\s*32px/)
  })

  it('uses the shared control tokens (height, border, radius, colours, font)', () => {
    expect(shared.body).toMatch(/min-block-size:\s*var\(--ctl-h\)/)
    expect(shared.body).toMatch(/border:\s*1px solid var\(--border-strong\)/)
    expect(shared.body).toMatch(/border-radius:\s*var\(--r-md\)/)
    expect(shared.body).toMatch(/background-color:\s*var\(--surface\)/)
    expect(shared.body).toMatch(/color:\s*var\(--text\)/)
    expect(shared.body).toMatch(/font-size:\s*var\(--fs-base\)/)
    expect(shared.body).toMatch(/text-overflow:\s*ellipsis/)
  })

  it('no other global rule for these selects resets the chevron or the size', () => {
    const others = blocks.filter(b => b !== shared && b.selectors.some(s => SHARED.includes(s)))
    for (const b of others) {
      expect(b.body, b.selectors.join(', ')).not.toMatch(/(^|;)\s*background:/)
      expect(b.body, b.selectors.join(', ')).not.toMatch(/background-image/)
      expect(b.body, b.selectors.join(', ')).not.toMatch(/min-height:\s*44px/)
    }
  })

  it('the unused .inline-select variant is gone', () => {
    expect(css).not.toMatch(/\.inline-select\b/)
  })
})

describe('native selects use the shared style', () => {
  // Selects with their own deliberate look: header language switcher, tenant switcher, the
  // "+ Tag" ghost select of TagChips.
  const OWN_LOOK = /\b(language-switcher|tenant-select|tag-chips__add)\b/
  const files = vueFiles(resolve(src, 'views')).concat(vueFiles(resolve(src, 'components')), vueFiles(resolve(src, 'layouts')))

  it('every <select> sits in a FormField or carries .form-select / .comp-filter', () => {
    const offenders: string[] = []
    for (const f of files) {
      const tpl = (readFileSync(f, 'utf8').match(/<template>[\s\S]*<\/template>/) || [''])[0]
      for (const m of tpl.matchAll(/<select\b(?:[^>"]|"[^"]*")*>/g)) {
        const tag = m[0]
        if (/:id="f\.id"/.test(tag)) continue
        if (/class="[^"]*\b(form-select|comp-filter)\b/.test(tag)) continue
        if (OWN_LOOK.test(tag)) continue
        offenders.push(`${relative(src, f)}: ${tag.replace(/\s+/g, ' ').slice(0, 80)}`)
      }
    }
    expect(offenders).toEqual([])
  })

  it('no component styles a select arrow or select box of its own', () => {
    for (const rel of ['views/admin/AdminIncidentDetail.vue', 'components/SubscriptionManager.vue']) {
      const style = stripComments((read(rel).match(/<style[\s\S]*<\/style>/) || [''])[0])
      expect(style, rel).not.toMatch(/background-image:[^;]*linear-gradient/)
      expect(style, rel).not.toMatch(/edit-abo-language-select/)
    }
  })

  it('the role select per status page in the user form uses .form-select', () => {
    expect(read('views/admin/AdminUsers.vue')).toMatch(/<select class="form-select" :value="getRoleForPage\(page\.ID\)"/)
  })

  it('the language select of the subscription manager uses .form-select', () => {
    expect(read('components/SubscriptionManager.vue')).toMatch(/<select id="edit-abo-language-select" v-model="editLanguage" class="form-select"/)
  })
})
