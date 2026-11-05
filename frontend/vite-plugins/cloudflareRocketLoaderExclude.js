// Vite ersetzt beim Build das Entry-Script-Tag komplett und
// verwirft dabei unbekannte Attribute wie data-cfasync - das Attribut aus
// index.html landet also nicht in dist/index.html. Dieses Plugin fügt es dem
// gebündelten Script-Tag nachträglich wieder hinzu, damit Cloudflare Rocket
// Loader es in Produktion tatsächlich unverändert lässt (sonst verletzt der
// von Rocket Loader inline eingefügte Code-Ersatz die CSP).
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
