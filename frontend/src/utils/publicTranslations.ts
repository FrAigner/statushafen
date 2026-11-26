// Translations on the public status page.
// The server delivers in one response (GET /api/public/pages/{slug}/translations,
// docs/api/openapi.yaml) per object the confirmed texts in the visitor's
// language, falling back to the page's default language; if an object is
// missing, the original applies. Only pure functions live here: fetching and
// applying to the data. All texts are plain text and are rendered by Vue as
// text (never as HTML).
import type { ApiFetch } from '../composables/useApi'
import type { ApiError } from '../types/api'

/** Translated texts of one object: title (name/title) and text (description/message). */
export interface TranslatedText {
  title?: string
  text?: string
}

/** Translated texts per object ID. */
export type TranslationMap = Record<string, TranslatedText>

export interface PublicTranslations {
  language: string
  components: TranslationMap
  incidents: TranslationMap
  notices: TranslationMap
}

interface LocalizableTreeNode {
  ID: number
  Name?: string
  Description?: string
  Children?: LocalizableTreeNode[] | null
}

interface LocalizableComponentRef {
  ID: number
  Name?: string
}

interface LocalizableIncident {
  ID: number
  Title?: string
  Message?: string
  Components?: LocalizableComponentRef[] | null
}

interface LocalizableNotice {
  ID: number
  Title?: string
  Message?: string
}

const isPlainObject = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)

// Checks the response far enough that it can be applied safely; everything
// else counts as "no translations" (null), and the page then shows the original.
export const parsePublicTranslations = (res: unknown): PublicTranslations | null => {
  if (!isPlainObject(res) || !isPlainObject(res.components) || !isPlainObject(res.incidents) || !isPlainObject(res.notices)) {
    return null
  }
  return { language: (res.language as string) || '', components: res.components as TranslationMap, incidents: res.incidents as TranslationMap, notices: res.notices as TranslationMap }
}

// Fetches a language-dependent public response. If the visitor's language is
// not active on the page, the server answers with 400; then a second fetch
// without `lang` follows (the page's default language). Any other error yields null:
// translations are an extra and must never block the page.
const fetchWithLangFallback = async <R>(apiFetch: ApiFetch, path: string, lang: string | null | undefined, token: unknown, parse: (res: unknown) => R): Promise<R | null> => {
  const build = (l: string | null | undefined) => {
    const params: string[] = []
    if (l) params.push(`lang=${encodeURIComponent(l)}`)
    if (token) params.push(`token=${encodeURIComponent(token as string)}`)
    return `${path}${params.length ? `?${params.join('&')}` : ''}`
  }
  try {
    return parse(await apiFetch(build(lang), { expectedStatuses: [400] }))
  } catch (err) {
    if ((err as ApiError | null | undefined)?.status !== 400 || !lang) return null
  }
  try {
    return parse(await apiFetch(build(''), { expectedStatuses: [400] }))
  } catch {
    return null
  }
}

export const fetchPublicTranslations = (apiFetch: ApiFetch, slug: string, lang: string | null | undefined, token?: unknown): Promise<PublicTranslations | null> =>
  fetchWithLangFallback(apiFetch, `/api/public/pages/${slug}/translations`, lang, token, parsePublicTranslations)

// Updates of an incident. The response (GET .../incidents/{id}/update-translations)
// carries per update ID the confirmed text in the visitor's language (falling back
// to the default language); everything else counts as "no translations" (empty map).
const parseUpdateTranslations = (res: unknown): Record<string, unknown> => (isPlainObject(res) && isPlainObject(res.updates) ? res.updates : {})

export const fetchPublicUpdateTranslations = async (apiFetch: ApiFetch, slug: string, incidentId: number | string, lang: string | null | undefined, token?: unknown): Promise<Record<string, unknown>> =>
  (await fetchWithLangFallback(apiFetch, `/api/public/pages/${slug}/incidents/${incidentId}/update-translations`, lang, token, parseUpdateTranslations)) ?? {}

// Timeline entries ({ id, message, … }) with the translated text; without an entry
// in the map (or with an empty text) the original stays. The same list comes back
// if there is nothing to translate.
export const localizeUpdates = <L extends readonly { id: number | string; message?: string }[] | null | undefined>(list: L, map: unknown): L => {
  if (!Array.isArray(list) || !isPlainObject(map) || Object.keys(map).length === 0) return list
  return (list as NonNullable<L>).map((u) => {
    const text = map[String(u.id)]
    return typeof text === 'string' && text.trim() !== '' ? { ...u, message: text } : u
  }) as unknown as L
}

// An empty translated text leaves the original in place.
const pick = <O>(original: O, translated: unknown): O | string => (typeof translated === 'string' && translated.trim() !== '' ? translated : original)

const entry = (map: unknown, id: number | string): TranslatedText | undefined => (isPlainObject(map) ? map[String(id)] as TranslatedText | undefined : undefined)

// Components and groups in the tree: name (title) and description (text).
export const localizeTree = <L extends readonly LocalizableTreeNode[] | null | undefined>(nodes: L, map: unknown): L => {
  if (!Array.isArray(nodes) || !isPlainObject(map) || Object.keys(map).length === 0) return nodes
  const list = nodes as readonly LocalizableTreeNode[]
  const out = list.map((n) => {
    const tr = entry(map, n.ID)
    const children = n.Children?.length ? localizeTree(n.Children, map) : n.Children
    if (!tr && children === n.Children) return n
    return { ...n, Name: pick(n.Name, tr?.title), Description: pick(n.Description, tr?.text), Children: children }
  })
  // Same identity if nothing was translated (saves recomputations).
  return out.every((o, idx) => o === list[idx]) ? nodes : out as unknown as L
}

// Incidents: title and initial message; the affected components (i.Components)
// carry the translated names. localizeUpdates translates the incident's updates.
export const localizeIncidents = <L extends readonly LocalizableIncident[] | null | undefined>(list: L, translations: PublicTranslations | null | undefined): L => {
  if (!Array.isArray(list) || !translations) return list
  const { incidents, components } = translations
  return (list as readonly LocalizableIncident[]).map((i) => {
    const tr = entry(incidents, i.ID)
    const comps = (i.Components || []).map((c) => {
      const ct = entry(components, c.ID)
      return ct ? { ...c, Name: pick(c.Name, ct.title) } : c
    })
    if (!tr && comps.every((c, idx) => c === (i.Components as LocalizableComponentRef[])[idx])) return i
    return { ...i, Title: pick(i.Title, tr?.title), Message: pick(i.Message, tr?.text), ...(i.Components ? { Components: comps } : {}) }
  }) as unknown as L
}

// Notices: title and message.
export const localizeNotices = <L extends readonly LocalizableNotice[] | null | undefined>(list: L, map: unknown): L => {
  if (!Array.isArray(list) || !isPlainObject(map)) return list
  return (list as readonly LocalizableNotice[]).map((n) => {
    const tr = entry(map, n.ID)
    return tr ? { ...n, Title: pick(n.Title, tr.title), Message: pick(n.Message, tr.text) } : n
  }) as unknown as L
}
