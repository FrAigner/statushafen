// Built-in documentation under /docs.
//
// The content lives as Markdown in the repo (src/docs/<language>/<section>.md),
// so that docs and app have ONE source and changes to features land in the same
// PR as the docs. Vite includes the files as text via ?raw; they end up in the
// (lazily loaded) chunk of the docs page, not in the start bundle.
//
// Rendering uses marked (pinned exactly) and the result is ALWAYS sent through
// DOMPurify: the Markdown files do come from the repo, but marked passes raw
// HTML through, and an accidental <script> or onerror in a docs file must
// never execute.
import { Marked } from 'marked'
import DOMPurify from 'dompurify'

/** Markdown text per locale and section slug. */
export type DocSources = Record<string, Record<string, string>>

export interface DocHeading {
  id: string
  text: string
  depth: number
}

export interface DocSection {
  markdown: string
  locale: string
  fallback: boolean
}

// Order of the navigation. The slugs are language independent (German), so that
// a deep link like /docs/vorfaelle points to the same section in both languages.
// Titles come from the first heading of the respective file.
export const SECTION_IDS: string[] = [
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

export function byLocaleAndSlug(modules: Record<string, string>): DocSources {
  const out: DocSources = {}
  for (const [path, text] of Object.entries(modules)) {
    const m = path.match(/\/([a-z]{2})\/([a-z0-9-]+)\.md$/)
    if (!m) continue
    const [, locale, slug] = m as unknown as [string, string, string]
    out[locale] ??= {}
    out[locale][slug] = text
  }
  return out
}

export const DOC_SOURCES = byLocaleAndSlug(
  import.meta.glob<string>('./*/*.md', { query: '?raw', import: 'default', eager: true })
)

// Removes HTML tags. Repeats until nothing changes, so that removing one tag can never leave another
// one behind; for this pattern a single pass is already final, so the result is the same either way.
export function stripTags(html: string): string {
  let text = html
  let previous: string
  do {
    previous = text
    text = text.replace(/<[^>]*>/g, '')
  } while (text !== previous)
  return text
}

// Heading IDs for anchors (/docs/vorfaelle#status-ablauf). Umlauts are
// spelled out, so that the anchors are readable and usable in URLs without escaping.
export function slugify(text: unknown): string {
  return stripTags(String(text)
    .toLowerCase()
    .replace(/ä/g, 'ae').replace(/ö/g, 'oe').replace(/ü/g, 'ue').replace(/ß/g, 'ss'))
    .replace(/&[a-z]+;/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

// Renders Markdown to sanitized HTML and returns the headings (levels 2
// and 3) for the table of contents. Each call uses its own Marked instance,
// so that the ID counting for duplicate headings does not continue across
// sections.
export function renderMarkdown(markdown: string | null | undefined): { html: string; headings: DocHeading[] } {
  const headings: DocHeading[] = []
  const used = new Map<string, number>()
  const marked = new Marked({ gfm: true })
  marked.use({
    renderer: {
      heading({ tokens, depth }) {
        const html = this.parser.parseInline(tokens)
        const text = stripTags(html)
        const base = slugify(text) || 'abschnitt'
        const n = used.get(base) || 0
        used.set(base, n + 1)
        const id = n ? `${base}-${n}` : base
        if (depth === 2 || depth === 3) headings.push({ id, text, depth })
        return `<h${depth} id="${id}">${html}</h${depth}>\n`
      },
    },
  })
  // Without async extensions marked.parse returns synchronously.
  const raw = marked.parse(markdown || '') as string
  const html = DOMPurify.sanitize(raw, { USE_PROFILES: { html: true } })
  return { html, headings }
}

// First heading of a Markdown file as the section title.
export function extractTitle(markdown: string | null | undefined): string {
  const m = (markdown || '').match(/^#\s+(.+)$/m)
  return m ? (m[1] as string).trim() : ''
}

// Markdown without the first heading: the page sets the title itself, so that
// the table of contents can sit between title and text.
export function stripTitle(markdown: string | null | undefined): string {
  return (markdown || '').replace(/^#\s+.+$/m, '')
}

// Returns the section in the requested language. If it is missing there, the
// German version is used and `fallback` is set, so that the page can show a
// notice.
export function resolveSection(slug: string, locale: string, sources: DocSources = DOC_SOURCES): DocSection | null {
  const own = sources[locale]?.[slug]
  if (own) return { markdown: own, locale, fallback: false }
  const base = sources[DOCS_DEFAULT_LOCALE]?.[slug]
  if (base) return { markdown: base, locale: DOCS_DEFAULT_LOCALE, fallback: locale !== DOCS_DEFAULT_LOCALE }
  return null
}

// Full text without Markdown syntax for the simple search in the navigation.
export function plainText(markdown: string | null | undefined): string {
  return (markdown || '')
    .replace(/```[a-z]*/g, ' ')
    .replace(/[#*_`>|[\]()-]/g, ' ')
    .replace(/\s+/g, ' ')
    .toLowerCase()
}
