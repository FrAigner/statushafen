// Quick search entries for the built-in docs: one per section title and one
// per h2/h3 heading (anchor ids as rendered by docs/index.js). Loaded lazily by
// the quick search so the docs and marked stay out of the admin start bundle.
import { DOC_SOURCES, SECTION_IDS, resolveSection, extractTitle, renderMarkdown } from '../docs'

const ENTITIES = { '&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"', '&#39;': "'" }
const decode = (text) => text.replace(/&(amp|lt|gt|quot|#39);/g, m => ENTITIES[m])
const stripInline = (text) => decode(text.replace(/[`*_]/g, '')).trim()

export function buildDocsEntries(locale, { sources = DOC_SOURCES, sectionIds = SECTION_IDS } = {}) {
  return sectionIds.flatMap(slug => {
    const section = resolveSection(slug, locale, sources)
    if (!section) return []
    const title = stripInline(extractTitle(section.markdown)) || slug
    const base = { kind: 'doc', key: slug, label: title, context: title, to: `/docs/${slug}` }
    const headings = renderMarkdown(section.markdown).headings.map(h => ({
      kind: 'doc',
      key: `${slug}#${h.id}`,
      label: decode(h.text).trim(),
      context: title,
      to: `/docs/${slug}#${h.id}`,
    }))
    return [base, ...headings]
  })
}
