// Übersetzungen auf der öffentlichen Statusseite.
// Der Server liefert in einer Antwort (GET /api/public/pages/{slug}/translations,
// docs/api/openapi.yaml) je Objekt die bestätigten Texte in der Sprache des
// Besuchers, Rückfall Standardsprache der Seite; fehlt ein Objekt, gilt das
// Original. Hier stehen nur reine Funktionen: Abruf und Anwenden auf die Daten.
// Alle Texte sind Klartext und werden von Vue als Text gerendert (nie als HTML).

const isPlainObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v)

// Prüft die Antwort so weit, dass sie gefahrlos angewendet werden kann; alles
// andere gilt als „keine Übersetzungen“ (null), die Seite zeigt dann das Original.
export const parsePublicTranslations = (res) => {
  if (!isPlainObject(res) || !isPlainObject(res.components) || !isPlainObject(res.incidents) || !isPlainObject(res.notices)) {
    return null
  }
  return { language: res.language || '', components: res.components, incidents: res.incidents, notices: res.notices }
}

// Ruft eine sprachabhängige öffentliche Antwort ab. Ist die Sprache des Besuchers
// bei der Seite nicht aktiv, antwortet der Server mit 400; dann folgt ein zweiter
// Abruf ohne `lang` (Standardsprache der Seite). Jeder andere Fehler ergibt null:
// Übersetzungen sind eine Zugabe und dürfen die Seite nie blockieren.
const fetchWithLangFallback = async (apiFetch, path, lang, token, parse) => {
  const build = (l) => {
    const params = []
    if (l) params.push(`lang=${encodeURIComponent(l)}`)
    if (token) params.push(`token=${encodeURIComponent(token)}`)
    return `${path}${params.length ? `?${params.join('&')}` : ''}`
  }
  try {
    return parse(await apiFetch(build(lang), { expectedStatuses: [400] }))
  } catch (err) {
    if (err?.status !== 400 || !lang) return null
  }
  try {
    return parse(await apiFetch(build(''), { expectedStatuses: [400] }))
  } catch {
    return null
  }
}

export const fetchPublicTranslations = (apiFetch, slug, lang, token) =>
  fetchWithLangFallback(apiFetch, `/api/public/pages/${slug}/translations`, lang, token, parsePublicTranslations)

// Updates eines Vorfalls. Die Antwort (GET .../incidents/{id}/update-translations)
// trägt je Update-ID den bestätigten Text in der Sprache des Besuchers (Rückfall
// Standardsprache); alles andere gilt als „keine Übersetzungen“ (leere Map).
const parseUpdateTranslations = (res) => (isPlainObject(res) && isPlainObject(res.updates) ? res.updates : {})

export const fetchPublicUpdateTranslations = async (apiFetch, slug, incidentId, lang, token) =>
  (await fetchWithLangFallback(apiFetch, `/api/public/pages/${slug}/incidents/${incidentId}/update-translations`, lang, token, parseUpdateTranslations)) ?? {}

// Timeline-Einträge ({ id, message, … }) mit dem übersetzten Text; ohne Eintrag
// in der Map (oder bei leerem Text) bleibt das Original. Gleiche Liste zurück,
// wenn nichts zu übersetzen ist.
export const localizeUpdates = (list, map) => {
  if (!Array.isArray(list) || !isPlainObject(map) || Object.keys(map).length === 0) return list
  return list.map((u) => {
    const text = map[String(u.id)]
    return typeof text === 'string' && text.trim() !== '' ? { ...u, message: text } : u
  })
}

// Leerer Text einer Übersetzung lässt das Original stehen.
const pick = (original, translated) => (typeof translated === 'string' && translated.trim() !== '' ? translated : original)

const entry = (map, id) => (isPlainObject(map) ? map[String(id)] : undefined)

// Komponenten und Gruppen im Baum: Name (title) und Beschreibung (text).
export const localizeTree = (nodes, map) => {
  if (!Array.isArray(nodes) || !isPlainObject(map) || Object.keys(map).length === 0) return nodes
  const out = nodes.map((n) => {
    const tr = entry(map, n.ID)
    const children = n.Children?.length ? localizeTree(n.Children, map) : n.Children
    if (!tr && children === n.Children) return n
    return { ...n, Name: pick(n.Name, tr?.title), Description: pick(n.Description, tr?.text), Children: children }
  })
  // Gleiche Identität, wenn nichts übersetzt wurde (spart Neuberechnungen).
  return out.every((o, idx) => o === nodes[idx]) ? nodes : out
}

// Vorfälle: Titel und Erstmeldung; die betroffenen Komponenten (i.Components)
// tragen die übersetzten Namen. Die Updates des Vorfalls übersetzt localizeUpdates.
export const localizeIncidents = (list, translations) => {
  if (!Array.isArray(list) || !translations) return list
  const { incidents, components } = translations
  return list.map((i) => {
    const tr = entry(incidents, i.ID)
    const comps = (i.Components || []).map((c) => {
      const ct = entry(components, c.ID)
      return ct ? { ...c, Name: pick(c.Name, ct.title) } : c
    })
    if (!tr && comps.every((c, idx) => c === i.Components[idx])) return i
    return { ...i, Title: pick(i.Title, tr?.title), Message: pick(i.Message, tr?.text), ...(i.Components ? { Components: comps } : {}) }
  })
}

// Ankündigungen: Titel und Meldung.
export const localizeNotices = (list, map) => {
  if (!Array.isArray(list) || !isPlainObject(map)) return list
  return list.map((n) => {
    const tr = entry(map, n.ID)
    return tr ? { ...n, Title: pick(n.Title, tr.title), Message: pick(n.Message, tr.text) } : n
  })
}
