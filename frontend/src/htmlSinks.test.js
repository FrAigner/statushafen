import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve, relative, join } from 'path'

// Wächter für HTML-Senken: v-html, innerHTML & Co. sind nur an den bekannten
// Stellen erlaubt, und jede davon bekommt ihren Wert aus einem Bereiniger. Eine
// neue Senke (oder ein bisher bereinigter Wert, der ungefiltert durchgereicht
// wird) fällt hier auf und braucht eine bewusste Entscheidung samt Eintrag.
// Kommentare und Tests zählen nicht.
const root = resolve(dirname(fileURLToPath(import.meta.url)))

// Datei -> { Bindung der Senke, Quelltext-Fragment, das den Bereiniger belegt }
const ALLOWED_SINKS = {
  'components/RichText.vue': { binding: 'html', sanitizer: 'renderRichText(' },
  'views/admin/AdminNotificationTemplates.vue': { binding: 'sanitizedPreviewBody', sanitizer: 'sanitizeMailPreview(previewBody.value)' },
  'views/public/DocsPage.vue': { binding: 'rendered.html', sanitizer: 'renderMarkdown(' },
}

function sourceFiles(dir = root) {
  const files = []
  for (const name of readdirSync(dir)) {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) files.push(...sourceFiles(full))
    else if (/\.(vue|js)$/.test(name) && !/\.(test|spec)\.js$/.test(name) && !full.includes('test-support')) files.push(full)
  }
  return files
}

// Nur HTML-Kommentare und reine Zeilenkommentare: Ein "/*" in einem Template- oder
// Stilabschnitt dürfte sonst bis zum nächsten "*/" alles verschlucken, auch eine Senke.
export function stripComments(src) {
  return src.replace(/<!--[\s\S]*?-->/g, '').replace(/^\s*\/\/.*$/gm, '')
}

const SINK_PATTERNS = [
  /v-html\s*=\s*"([^"]*)"/g,
  /\.(?:innerHTML|outerHTML)\s*=/g,
  /insertAdjacentHTML\s*\(/g,
  /document\.write\s*\(/g,
  /\binnerHTML\s*:/g,
  /\bdomProps\s*:\s*\{\s*innerHTML/g,
]

// Liefert die Senken eines Quelltexts als { kind, binding }.
export function findHtmlSinks(src) {
  const code = stripComments(src)
  const sinks = []
  for (const re of SINK_PATTERNS) {
    for (const m of code.matchAll(re)) sinks.push({ kind: re.source, binding: m[1] ?? null })
  }
  return sinks
}

describe('HTML-Senken', () => {
  it('erkennt v-html und innerHTML, ignoriert Kommentare', () => {
    expect(findHtmlSinks('<div v-html="x"></div>')).toEqual([{ kind: expect.any(String), binding: 'x' }])
    expect(findHtmlSinks('el.innerHTML = a').length).toBe(1)
    expect(findHtmlSinks('el.insertAdjacentHTML("beforeend", a)').length).toBe(1)
    expect(findHtmlSinks('<!-- nie v-html="x" -->\n// el.innerHTML = a').length).toBe(0)
    expect(findHtmlSinks('/* offen */\n<div v-html="y"></div>').length).toBe(1)
  })

  it('nur die bekannten Senken sind vorhanden, jeweils mit Bereiniger', () => {
    const found = {}
    for (const file of sourceFiles()) {
      const sinks = findHtmlSinks(readFileSync(file, 'utf8'))
      if (sinks.length) found[relative(root, file).split('\\').join('/')] = sinks
    }
    expect(Object.keys(found).sort()).toEqual(Object.keys(ALLOWED_SINKS).sort())
    for (const [file, sinks] of Object.entries(found)) {
      expect(sinks.map((s) => s.binding), file).toEqual([ALLOWED_SINKS[file].binding])
      expect(readFileSync(join(root, file), 'utf8'), `${file} bereinigt vor der Ausgabe`).toContain(ALLOWED_SINKS[file].sanitizer)
    }
  })
})
