// At build time Vite replaces the entry script tag completely and
// drops unknown attributes like data-cfasync - so the attribute from
// index.html does not end up in dist/index.html. This plugin adds it back to
// the bundled script tag afterwards so that Cloudflare Rocket Loader really
// leaves it unchanged in production (otherwise the code replacement that
// Rocket Loader inserts inline violates the CSP).
import type { Plugin } from 'vite'

// `satisfies` instead of a Plugin return type: the test calls transformIndexHtml directly, which the
// Plugin type only offers as a union of function and object hook.
export function cloudflareRocketLoaderExclude() {
  return {
    name: 'cloudflare-rocket-loader-exclude',
    transformIndexHtml(html: string): string {
      return html.replace(
        /<script(\s+type="module"[^>]*?)(\s*>)/,
        (match, attrs: string, close: string) =>
          attrs.includes('data-cfasync') ? match : `<script${attrs} data-cfasync="false"${close}`,
      )
    },
  } satisfies Plugin
}
