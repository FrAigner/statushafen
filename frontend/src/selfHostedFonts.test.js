import { describe, it, expect } from 'vitest'
import { existsSync, readFileSync, readdirSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// DSGVO: Schriften werden selbst gehostet (npm-Pakete @fontsource/*, vom Build
// ausgeliefert), es gibt keine Verweise auf Google Fonts (die IP der Besucher
// ginge sonst an Google). Lizenz: SIL Open Font License 1.1, Hinweise in THIRD_PARTY_NOTICES.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (p) => readFileSync(resolve(root, p), 'utf-8')

const FONTS = [
  ...[400, 500, 600, 700].map((w) => ['Inter', 'inter', w]),
  ...[400, 500, 600, 700].map((w) => ['IBM Plex Sans', 'ibm-plex-sans', w]),
  ...[400, 500, 600].map((w) => ['IBM Plex Mono', 'ibm-plex-mono', w]),
]

const GOOGLE_FONTS = /fonts\.(googleapis|gstatic)\.com/

function sourceFiles(dir = 'src') {
  return readdirSync(resolve(root, dir), { withFileTypes: true }).flatMap((e) => {
    const path = `${dir}/${e.name}`
    if (e.isDirectory()) return sourceFiles(path)
    return /\.(vue|css|js)$/.test(e.name) && !e.name.endsWith('.test.js') ? [path] : []
  })
}

describe('selbst gehostete Schriften', () => {
  it.each([...sourceFiles(), 'index.html'])(
    '%s verweist nicht auf Google Fonts',
    (file) => {
      expect(read(file)).not.toMatch(GOOGLE_FONTS)
    },
  )

  it('Scanner findet Google-Fonts-Verweise und lässt chat.googleapis.com durch', () => {
    expect("@import url('https://fonts.googleapis.com/css2')").toMatch(GOOGLE_FONTS)
    expect('https://fonts.gstatic.com/s/inter.woff2').toMatch(GOOGLE_FONTS)
    expect('https://chat.googleapis.com/v1/spaces').not.toMatch(GOOGLE_FONTS)
  })

  it('sammelt Quelldateien rekursiv und ohne Testdateien', () => {
    const files = sourceFiles()
    expect(files).toContain('src/style.css')
    expect(files).toContain('src/assets/tokens.css')
    expect(files).toContain('src/main.js')
    expect(files.some((f) => f.endsWith('.vue'))).toBe(true)
    expect(files.some((f) => f.endsWith('.test.js'))).toBe(false)
  })

  it.each(FONTS)('%s: @fontsource/%s Gewicht %s ist eingebunden', (family, slug, weight) => {
    const css = read('src/assets/fonts.css')
    expect(css).toContain(`@import '@fontsource/${slug}/latin-${weight}.css';`)
    const pkg = JSON.parse(read('package.json'))
    expect(Object.keys(pkg.dependencies)).toContain(`@fontsource/${slug}`)
  })

  it('es liegen keine Schriftdateien im Repository (sie kommen aus den npm-Paketen)', () => {
    expect(existsSync(resolve(root, 'public/fonts'))).toBe(false)
  })

  it('fonts.css wird per @import in tokens.css geladen, davor steht nur @layer app-reset', () => {
    const tokens = read('src/assets/tokens.css')
    const idx = tokens.indexOf("@import './fonts.css';")
    expect(idx).toBeGreaterThanOrEqual(0)
    const before = tokens.slice(0, idx).replace(/\/\*[\s\S]*?\*\//g, '').trim()
    expect(before).toBe('@layer app-reset;')
    expect(read('src/main.js')).not.toContain('fonts.css')
  })

  it.each(['inter', 'ibm-plex-sans', 'ibm-plex-mono'])('Lizenztext (OFL) liegt dem Paket @fontsource/%s bei', (slug) => {
    expect(read(`node_modules/@fontsource/${slug}/LICENSE`)).toMatch(/SIL OPEN FONT LICENSE/i)
  })
})
