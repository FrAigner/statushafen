import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Regressionstest: Das Leuchtturm-Icon der
// öffentlichen Kopfzeile war ein dunkelblaues PNG, das im Dunkelmodus per
// CSS-Filter (brightness(0) invert(1)) aufgehellt werden musste. Jetzt ist es
// ein Inline-SVG mit currentColor; die Farbe kommt aus dem Token --primary und
// folgt damit Hell und Dunkel ohne Filter.
describe('PublicLayout: Logo-Farbe', () => {
  const vue = readFileSync(resolve(__dirname, 'PublicLayout.vue'), 'utf-8')

  it('färbt das Logo über ein Token, nicht über einen Filter', () => {
    const rule = vue.match(/\n\.logo-mark\s*\{([^}]*)\}/)
    expect(rule).not.toBeNull()
    expect(rule[1]).toMatch(/color:\s*var\(--primary\)/)
    expect(vue).not.toMatch(/brightness\(0\)/)
    expect(vue).not.toMatch(/filter:\s*brightness/)
  })

  it('zeichnet das SVG nur mit currentColor, ohne feste Farben', () => {
    const svg = vue.match(/<svg[\s\S]*?<\/svg>/)[0]
    expect(svg).toContain('currentColor')
    expect(svg).not.toMatch(/#[0-9a-f]{3,6}\b/i)
    expect(vue).not.toContain('/logo.png')
  })
})
