// Teil D: Hilfen für die öffentliche Anzeige externer Quellen.

// Das Dashboard liefert je Komponente `Status` (eigener Status + offene eigene
// Vorfälle) und `effective_status` (zusätzlich das Schlimmste aller gekoppelten
// externen Komponenten; veraltete Quellen zählen nicht). Die Seite
// zeigt überall den effektiven Status. Rechnen tut das Backend (EffectiveStatuses),
// das Frontend setzt den Wert nur ein, damit Gesamtstatus, Filter, Badges und
// Zähler ohne eigene Logik dasselbe sehen. Gruppen behalten "group".
export const applyEffectiveStatus = (nodes) =>
  (nodes || []).map((n) => {
    const children = n.Children ? applyEffectiveStatus(n.Children) : n.Children
    const status = n.Status !== 'group' && n.effective_status ? n.effective_status : n.Status
    return { ...n, Status: status, Children: children }
  })

// Nur http(s)-Adressen werden verlinkt: Die URL stammt aus einer fremden
// Quelle, `javascript:` o. Ä. darf nie in einem href landen. Ergebnis ist die
// normalisierte Adresse oder null.
export const safeHttpUrl = (value) => {
  if (typeof value !== 'string' || value.trim() === '') return null
  try {
    const u = new URL(value.trim())
    return u.protocol === 'https:' || u.protocol === 'http:' ? u.href : null
  } catch {
    return null
  }
}

// Gekoppelte Quellen einer Komponente, die angezeigt werden: verschwundene
// Bausteine nicht (sie zählen auch nicht im effektiven Status).
export const visibleExternalSources = (node) =>
  (node?.external_sources || []).filter((r) => !r.vanished)
