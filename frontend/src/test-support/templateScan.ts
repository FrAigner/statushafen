// Small template scanner for source guards (required asterisk,
// novalidate, help keys). Deliberately not a complete HTML parser, only
// as much as the guards need: find start tags with their attributes, even
// if an attribute value contains ">" (e.g. `:options="xs.map(x => x)"`).
// A naive regex like /<Onyx[^>]*>/ would stop in the middle of the expression there.

export interface StartTag {
  name: string
  /** Attribute name -> value (bare attributes like `required` get ''). */
  attrs: Record<string, string>
  raw: string
  index: number
  end: number
}

// All .vue files under src/ as raw text; the key is the path from src/ on.
const RAW = import.meta.glob<string>('/src/**/*.vue', { query: '?raw', import: 'default', eager: true })

export const vueSources = Object.fromEntries(
  Object.entries(RAW).map(([path, src]) => [path.replace(/^\/src\//, ''), src]),
)

// Only the <template> part, so that comments or strings in the <script> are not
// recognized as tags.
export function templatePart(src: string): string {
  const start = src.indexOf('<template>')
  const end = src.lastIndexOf('</template>')
  return start === -1 || end === -1 ? '' : src.slice(start, end)
}

// Returns all start tags as { name, attrs, raw, index, end }. attrs is a
// map attribute name -> value (bare attributes like `required` get '').
export function startTags(template: string): StartTag[] {
  const tags: StartTag[] = []
  const re = /<([A-Za-z][\w-]*)/g
  let m: RegExpExecArray | null
  while ((m = re.exec(template)) !== null) {
    let i = re.lastIndex
    let quote: string | null = null
    while (i < template.length) {
      const ch = template[i]
      if (quote) {
        if (ch === quote) quote = null
      } else if (ch === '"' || ch === "'") {
        quote = ch
      } else if (ch === '>') {
        break
      }
      i++
    }
    const raw = template.slice(m.index, i + 1)
    tags.push({ name: m[1], attrs: parseAttrs(template.slice(re.lastIndex, i)), raw, index: m.index, end: i + 1 })
    re.lastIndex = i + 1
  }
  return tags
}

function parseAttrs(text: string): Record<string, string> {
  const attrs: Record<string, string> = {}
  const re = /([:@#]?[\w.:-]+)(?:\s*=\s*("([^"]*)"|'([^']*)'))?/g
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) !== null) {
    attrs[m[1]] = m[3] ?? m[4] ?? ''
  }
  return attrs
}

// Key from an expression like t('a.b') or $t("a.b", {...}).
export function i18nKeyOf(expr: string | null | undefined): string | null {
  const m = /\$?t\(\s*['"]([\w.-]+)['"]/.exec(expr || '')
  return m ? m[1] : null
}

// Value of a dot path in a locale object.
export function lookup(messages: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>((node, part) => (node == null ? undefined : (node as Record<string, unknown>)[part]), messages)
}
