import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Redesign 4a: Schutz für die fünf öffentlichen Dateien.
// Inline-Styles: 0 (einzige Ausnahme: die Kundenmarke auf .content-area),
// keine alten .badge-*-Klassen, keine Hex-Farben (nur Tokens).
const FILES = [
  'views/public/StatusPage.vue',
  'components/PageLayoutList.vue',
  'components/ComponentTreeNode.vue',
  'components/SubscribeWizard.vue',
  'components/SubscriptionManager.vue',
  'components/AvailabilityBars.vue',
]
const read = (f) => readFileSync(resolve(__dirname, f), 'utf-8')
// Kommentare entfernen (dort stehen Verweisnummern wie #534).
const code = (f) => read(f).replace(/\/\*[\s\S]*?\*\//g, '').replace(/<!--[\s\S]*?-->/g, '').replace(/^\s*\/\/.*$/gm, '')

describe('Öffentliche Seiten (Redesign 4a)', () => {
  it.each(FILES)('%s hat keinen Inline-Style außer der Kundenmarke', (f) => {
    const found = [...code(f).matchAll(/(?<![\w-])(:style|style)=("[^"]*")/g)].map(m => `${m[1]}=${m[2]}`)
    expect(found).toEqual(f === 'views/public/StatusPage.vue' ? [':style="brandingStyle"'] : [])
  })

  it.each(FILES)('%s nutzt keine alten .badge-*-Klassen und keinen Status-Farbpunkt', (f) => {
    const c = code(f)
    expect(c).not.toMatch(/\bbadge-(operational|degraded|partial|major|resolved|monitoring|identified|investigating)\b/)
    expect(c).not.toMatch(/class="badge\b|'badge'|\.tree-dot|status-pulse/)
  })

  it.each(FILES)('%s enthält keine Hex-Farben', (f) => {
    expect(code(f).match(/#[0-9a-fA-F]{3,8}\b/g) ?? []).toEqual([])
  })
})
