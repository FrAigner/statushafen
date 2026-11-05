import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'
import { cloudflareRocketLoaderExclude } from '../vite-plugins/cloudflareRocketLoaderExclude.js'

// Cloudflare Rocket Loader ersetzt Skripte inline, was die strikte
// CSP (script-src 'self') verletzt. data-cfasync="false" weist Rocket Loader
// an, das Skript unverändert zu laden statt es per Inline-Ersatz einzubinden.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const indexHtml = readFileSync(resolve(root, 'index.html'), 'utf-8')

describe('Rocket Loader CSP-Ausschluss', () => {
  it('main.js-Script-Tag trägt data-cfasync="false"', () => {
    const scriptTag = indexHtml.match(/<script[^>]*src="\/src\/main\.js"[^>]*>/)
    expect(scriptTag).toBeTruthy()
    expect(scriptTag[0]).toContain('data-cfasync="false"')
  })

  it('index.html enthält genau ein Script-Tag', () => {
    const scriptTags = indexHtml.match(/<script/g) ?? []
    expect(scriptTags).toHaveLength(1)
  })

  it('vite-Plugin ergänzt data-cfasync="false" am gebündelten Script-Tag (Vite verwirft das Attribut sonst beim Build)', () => {
    const builtHtml =
      '<head></head><body><script type="module" crossorigin src="/assets/index-abc123.js"></script></body>'
    const result = cloudflareRocketLoaderExclude().transformIndexHtml(builtHtml)
    expect(result).toContain('data-cfasync="false"')
    const scriptTag = result.match(/<script[^>]*>/)
    expect(scriptTag[0]).toBe(
      '<script type="module" crossorigin src="/assets/index-abc123.js" data-cfasync="false">',
    )
  })

  it('vite-Plugin fügt das Attribut nicht doppelt ein, falls es bereits vorhanden ist', () => {
    const html = '<script type="module" src="/src/main.js" data-cfasync="false"></script>'
    const result = cloudflareRocketLoaderExclude().transformIndexHtml(html)
    expect(result.match(/data-cfasync/g)).toHaveLength(1)
  })
})
