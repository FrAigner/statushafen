import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve, relative, join } from 'path'
import { stripHtmlComments } from './test-support/sourceText'

// Guard for HTML sinks: v-html, innerHTML & co. are only allowed at the known
// places, and each of them gets its value from a sanitizer. A new sink (or a
// previously sanitized value that is passed through unfiltered) shows up here
// and needs a deliberate decision plus an entry.
// Comments and tests do not count.
const root = resolve(dirname(fileURLToPath(import.meta.url)))

// File -> { sink binding, source fragment that proves the sanitizer }
const ALLOWED_SINKS: Record<string, { binding: string; sanitizer: string }> = {
  'components/RichText.vue': { binding: 'html', sanitizer: 'renderRichText(' },
  'views/admin/AdminNotificationTemplates.vue': { binding: 'sanitizedPreviewBody', sanitizer: 'sanitizeMailPreview(previewBody.value)' },
  'views/public/DocsPage.vue': { binding: 'rendered.html', sanitizer: 'renderMarkdown(' },
}

function sourceFiles(dir = root): string[] {
  const files: string[] = []
  for (const name of readdirSync(dir)) {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) files.push(...sourceFiles(full))
    else if (/\.(vue|js|ts)$/.test(name) && !/\.(test|spec)\.[jt]s$/.test(name) && !full.includes('test-support')) files.push(full)
  }
  return files
}

// Only HTML comments and pure line comments: otherwise a "/*" in a template or
// style section could swallow everything up to the next "*/", including a sink.
export function stripComments(src: string) {
  return stripHtmlComments(src).replace(/^\s*\/\/.*$/gm, '')
}

const SINK_PATTERNS = [
  /v-html\s*=\s*"([^"]*)"/g,
  /\.(?:innerHTML|outerHTML)\s*=/g,
  /insertAdjacentHTML\s*\(/g,
  /document\.write\s*\(/g,
  /\binnerHTML\s*:/g,
  /\bdomProps\s*:\s*\{\s*innerHTML/g,
]

interface HtmlSink { kind: string; binding: string | null }

// Returns the sinks of a source as { kind, binding }.
export function findHtmlSinks(src: string) {
  const code = stripComments(src)
  const sinks: HtmlSink[] = []
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

  it('erfasst .ts-Module, aber keine Tests und test-support', () => {
    const files = sourceFiles().map((f) => relative(root, f).split('\\').join('/'))
    expect(files).toContain('main.ts')
    expect(files).toContain('App.vue')
    expect(files.filter((f) => /\.(test|spec)\.[jt]s$/.test(f) || f.startsWith('test-support/'))).toEqual([])
  })

  it('nur die bekannten Senken sind vorhanden, jeweils mit Bereiniger', () => {
    const found: Record<string, HtmlSink[]> = {}
    for (const file of sourceFiles()) {
      const sinks = findHtmlSinks(readFileSync(file, 'utf8'))
      if (sinks.length) found[relative(root, file).split('\\').join('/')] = sinks
    }
    expect(Object.keys(found).sort()).toEqual(Object.keys(ALLOWED_SINKS).sort())
    for (const [file, sinks] of Object.entries(found)) {
      expect(sinks.map((s) => s.binding), file).toEqual([ALLOWED_SINKS[file]!.binding])
      expect(readFileSync(join(root, file), 'utf8'), `${file} bereinigt vor der Ausgabe`).toContain(ALLOWED_SINKS[file]!.sanitizer)
    }
  })
})
