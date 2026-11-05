// GET /api/pages/:id/components liefert einen Baum (oberste Ebene,
// Unterkomponenten in Children). Die Admin-Seiten haben nur die oberste Ebene
// gelesen, Unterkomponenten waren dort weder sichtbar noch wählbar. Diese
// Helfer machen daraus eine flache Liste in Baumreihenfolge (Eltern vor ihren
// Kindern, Geschwister in der Reihenfolge des Backends) mit Einrückungstiefe.
// Flach statt rekursiv gerendert, damit Tabellen und Auswahlen sowie die
// Namenssuche per ID (find) ohne eigene Rekursion auskommen.
export const flattenComponentTree = (nodes, depth = 0) =>
  (nodes || []).flatMap(n => [{ ...n, depth }, ...flattenComponentTree(n.Children, depth + 1)])

// Einrückung je Ebene. Ein Geviertleerzeichen (U+2003) statt normaler
// Leerzeichen, weil HTML diese nicht zusammenfasst - die Einrückung bleibt im
// Auswahl-Flyout sichtbar.
const INDENT = ' '

// Optionen für OnyxSelect aus der flachen Liste: Name eingerückt nach Tiefe.
// Archivierte Komponenten (ArchivedAt) werden nicht mehr zur Auswahl
// angeboten (Vorfälle, Wartungen, Vorlagen, Abonnenten, Gruppe); keepIds hält
// bereits gewählte (z. B. die aktuelle Eltern-Gruppe) trotzdem in der Auswahl.
export const componentOptions = (flat, keepIds = []) =>
  (flat || []).filter(c => !c.ArchivedAt || (keepIds || []).some(k => String(k) === String(c.ID))).map(c => ({ label: INDENT.repeat(c.depth || 0) + c.Name, value: String(c.ID) }))

// Komponentenseite nach Richtung C: Gruppen sind einklappbar, die
// Suche zeigt Treffer samt ihrer Eltern. Beides arbeitet auf der flachen Liste
// (Baumreihenfolge, `depth`): Die Nachfahren eines Eintrags sind die direkt
// folgenden Einträge mit größerer Tiefe.

// IDs aller Nachfahren des Eintrags mit der ID `id` (leer, wenn er keine hat
// oder fehlt).
export const descendantIds = (flat, id) => {
  const start = flat.findIndex(c => c.ID === id)
  if (start === -1) return []
  const ids = []
  for (let i = start + 1; i < flat.length && flat[i].depth > flat[start].depth; i++) ids.push(flat[i].ID)
  return ids
}

// Sichtbare Zeilen der Liste. Mit Suchbegriff: Einträge, deren Text passt,
// samt aller Vorfahren (damit der Treffer im Baum steht); eingeklappte Gruppen
// zählen dann nicht, sonst wäre ein Treffer in einer zugeklappten Gruppe
// unauffindbar. Ohne Suchbegriff: Nachfahren eingeklappter Gruppen entfallen.
// `textOf(c)` liefert den durchsuchbaren Text eines Eintrags.
export const visibleComponents = (flat, { query = '', collapsed = new Set(), textOf }) => {
  const needle = query.trim().toLowerCase()
  if (needle) {
    const keep = new Set()
    flat.forEach((c, i) => {
      if (!textOf(c).toLowerCase().includes(needle)) return
      keep.add(c.ID)
      let depth = c.depth
      for (let j = i - 1; j >= 0 && depth > 0; j--) {
        if (flat[j].depth < depth) { keep.add(flat[j].ID); depth = flat[j].depth }
      }
    })
    return flat.filter(c => keep.has(c.ID))
  }
  const rows = []
  let hiddenBelow = null
  for (const c of flat) {
    if (hiddenBelow !== null && c.depth > hiddenBelow) continue
    hiddenBelow = collapsed.has(c.ID) ? c.depth : null
    rows.push(c)
  }
  return rows
}
