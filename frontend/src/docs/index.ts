// Eingebaute Dokumentation unter /docs.
//
// Die Inhalte liegen als Markdown im Repo (src/docs/<sprache>/<abschnitt>.md),
// damit Doku und App EINE Quelle haben und Änderungen an Funktionen im selben
// PR wie die Doku landen. Vite bindet die Dateien per ?raw als Text ein; sie
// landen im (lazy geladenen) Chunk der Doku-Seite, nicht im Start-Bundle.
//
// Gerendert wird mit marked (exakt gepinnt) und das Ergebnis IMMER durch
// DOMPurify geschickt: Die Markdown-Dateien stammen zwar aus dem Repo, aber
// marked lässt Roh-HTML durch, und ein versehentliches <script> oder onerror
// in einer Doku-Datei darf nie ausgeführt werden.
import { Marked } from 'marked'
import DOMPurify from 'dompurify'

// Reihenfolge der Navigation. Die Slugs sind sprachunabhängig (deutsch), damit
// ein Deep-Link wie /docs/vorfaelle in beiden Sprachen auf denselben Abschnitt
// zeigt. Titel kommen aus der ersten Überschrift der jeweiligen Datei.
export const SECTION_IDS = [
  'was-ist-statushafen',
  'erste-schritte',
  'statusseiten',
  'oeffentliche-statusseite',
  'komponenten-und-tags',
  'externe-quellen',
  'vorfaelle',
  'wartungsfenster',
  'ankuendigungen',
  'formatierte-texte',
  'vorlagen',
  'mail-templates',
  'abonnements',
  'benutzer-und-rollen',
  'sprachen',
  'sso',
  'systemeinstellungen',
  'selbstueberwachung',
  'audit-log',
  'api-und-webhooks',
  'faq',
]

export const DOCS_DEFAULT_LOCALE = 'de'

export function byLocaleAndSlug(modules) {
  const out = {}
  for (const [path, text] of Object.entries(modules)) {
    const m = path.match(/\/([a-z]{2})\/([a-z0-9-]+)\.md$/)
    if (!m) continue
    out[m[1]] ??= {}
    out[m[1]][m[2]] = text
  }
  return out
}

export const DOC_SOURCES = byLocaleAndSlug(
  import.meta.glob('./*/*.md', { query: '?raw', import: 'default', eager: true })
)

// Überschriften-IDs für Anker (/docs/vorfaelle#status-ablauf). Umlaute werden
// ausgeschrieben, damit die Anker lesbar und in URLs ohne Escaping nutzbar sind.
export function slugify(text) {
  return String(text)
    .toLowerCase()
    .replace(/ä/g, 'ae').replace(/ö/g, 'oe').replace(/ü/g, 'ue').replace(/ß/g, 'ss')
    .replace(/<[^>]*>/g, '')
    .replace(/&[a-z]+;/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

// Rendert Markdown zu bereinigtem HTML und liefert die Überschriften (Ebene 2
// und 3) für das Inhaltsverzeichnis. Jeder Aufruf nutzt eine eigene
// Marked-Instanz, damit die ID-Zählung für doppelte Überschriften nicht über
// Abschnitte hinweg weiterläuft.
export function renderMarkdown(markdown) {
  const headings = []
  const used = new Map()
  const marked = new Marked({ gfm: true })
  marked.use({
    renderer: {
      heading({ tokens, depth }) {
        const html = this.parser.parseInline(tokens)
        const text = html.replace(/<[^>]*>/g, '')
        const base = slugify(text) || 'abschnitt'
        const n = used.get(base) || 0
        used.set(base, n + 1)
        const id = n ? `${base}-${n}` : base
        if (depth === 2 || depth === 3) headings.push({ id, text, depth })
        return `<h${depth} id="${id}">${html}</h${depth}>\n`
      },
    },
  })
  const raw = marked.parse(markdown || '')
  const html = DOMPurify.sanitize(raw, { USE_PROFILES: { html: true } })
  return { html, headings }
}

// Erste Überschrift einer Markdown-Datei als Abschnittstitel.
export function extractTitle(markdown) {
  const m = (markdown || '').match(/^#\s+(.+)$/m)
  return m ? m[1].trim() : ''
}

// Markdown ohne die erste Überschrift: Die Seite setzt den Titel selbst, damit
// das Inhaltsverzeichnis zwischen Titel und Text stehen kann.
export function stripTitle(markdown) {
  return (markdown || '').replace(/^#\s+.+$/m, '')
}

// Liefert den Abschnitt in der gewünschten Sprache. Fehlt er dort, wird die
// deutsche Fassung genommen und `fallback` gesetzt, damit die Seite einen
// Hinweis zeigen kann.
export function resolveSection(slug, locale, sources = DOC_SOURCES) {
  const own = sources[locale]?.[slug]
  if (own) return { markdown: own, locale, fallback: false }
  const base = sources[DOCS_DEFAULT_LOCALE]?.[slug]
  if (base) return { markdown: base, locale: DOCS_DEFAULT_LOCALE, fallback: locale !== DOCS_DEFAULT_LOCALE }
  return null
}

// Volltext ohne Markdown-Syntax für die einfache Suche in der Navigation.
export function plainText(markdown) {
  return (markdown || '')
    .replace(/```[a-z]*/g, ' ')
    .replace(/[#*_`>|[\]()-]/g, ' ')
    .replace(/\s+/g, ' ')
    .toLowerCase()
}
