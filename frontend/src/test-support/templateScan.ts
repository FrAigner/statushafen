// kleiner Template-Scanner für Quelltext-Guards (Pflichtstern,
// novalidate, Hilfe-Schlüssel). Bewusst kein vollständiger HTML-Parser, nur
// so viel, wie die Guards brauchen: Start-Tags samt Attributen finden, auch
// wenn ein Attributwert ">" enthält (z. B. `:options="xs.map(x => x)"`).
// Ein naiver Regex wie /<Onyx[^>]*>/ würde dort mitten im Ausdruck abbrechen.

// Alle .vue-Dateien unter src/ als Rohtext, Schlüssel ist der Pfad ab src/.
const RAW = import.meta.glob('/src/**/*.vue', { query: '?raw', import: 'default', eager: true })

export const vueSources = Object.fromEntries(
  Object.entries(RAW).map(([path, src]) => [path.replace(/^\/src\//, ''), src]),
)

// Nur der <template>-Teil, damit Kommentare oder Strings im <script> nicht
// als Tags erkannt werden.
export function templatePart(src) {
  const start = src.indexOf('<template>')
  const end = src.lastIndexOf('</template>')
  return start === -1 || end === -1 ? '' : src.slice(start, end)
}

// Liefert alle Start-Tags als { name, attrs, raw, index, end }. attrs ist eine
// Map Attributname -> Wert (bare Attribute wie `required` bekommen '').
export function startTags(template) {
  const tags = []
  const re = /<([A-Za-z][\w-]*)/g
  let m
  while ((m = re.exec(template)) !== null) {
    let i = re.lastIndex
    let quote = null
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

function parseAttrs(text) {
  const attrs = {}
  const re = /([:@#]?[\w.:-]+)(?:\s*=\s*("([^"]*)"|'([^']*)'))?/g
  let m
  while ((m = re.exec(text)) !== null) {
    attrs[m[1]] = m[3] ?? m[4] ?? ''
  }
  return attrs
}

// Schlüssel aus einem Ausdruck wie t('a.b') oder $t("a.b", {...}).
export function i18nKeyOf(expr) {
  const m = /\$?t\(\s*['"]([\w.-]+)['"]/.exec(expr || '')
  return m ? m[1] : null
}

// Wert eines Punkt-Pfads in einem Locale-Objekt.
export function lookup(messages, key) {
  return key.split('.').reduce((node, part) => (node == null ? undefined : node[part]), messages)
}
