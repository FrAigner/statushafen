// Quick search entries for the built-in docs: one per section title and one
// per h2/h3 heading (anchor ids as rendered by docs/index.ts). Loaded lazily by
// the quick search so the docs and marked stay out of the admin start bundle.
import { DOC_SOURCES, SECTION_IDS, resolveSection, extractTitle, renderMarkdown } from '../docs'
import type { DocSources } from '../docs'

const ENTITIES: Record<string, string> = { '&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"', '&#39;': "'" }
const decode = (text: string): string => text.replace(/&(amp|lt|gt|quot|#39);/g, m => ENTITIES[m] as string)
const stripInline = (text: string): string => decode(text.replace(/[`*_]/g, '')).trim()

export interface DocsSearchEntry {
  kind: 'doc'
  key: string
  label: string
  context: string
  to: string
}

export function buildDocsEntries(locale: string, { sources = DOC_SOURCES, sectionIds = SECTION_IDS }: { sources?: DocSources; sectionIds?: readonly string[] } = {}): DocsSearchEntry[] {
  return sectionIds.flatMap((slug): DocsSearchEntry[] => {
    const section = resolveSection(slug, locale, sources)
    if (!section) return []
    const title = stripInline(extractTitle(section.markdown)) || slug
    const base: DocsSearchEntry = { kind: 'doc', key: slug, label: title, context: title, to: `/docs/${slug}` }
    const headings = renderMarkdown(section.markdown).headings.map((h): DocsSearchEntry => ({
      kind: 'doc',
      key: `${slug}#${h.id}`,
      label: decode(h.text).trim(),
      context: title,
      to: `/docs/${slug}#${h.id}`,
    }))
    return [base, ...headings]
  })
}
