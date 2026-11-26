// At build time Vite replaces the entry script tag completely and
// drops unknown attributes like data-cfasync - so the attribute from
// index.html does not end up in dist/index.html. This plugin adds it back to
// the bundled script tag afterwards so that Cloudflare Rocket Loader really
// leaves it unchanged in production (otherwise the code replacement that
// Rocket Loader inserts inline violates the CSP).
export function cloudflareRocketLoaderExclude() {
  return {
    name: 'cloudflare-rocket-loader-exclude',
    transformIndexHtml(html) {
      return html.replace(
        /<script(\s+type="module"[^>]*?)(\s*>)/,
        (match, attrs, close) =>
          attrs.includes('data-cfasync') ? match : `<script${attrs} data-cfasync="false"${close}`,
      )
    },
  }
}
