// Umwandlung zwischen dem kommaseparierten SubscribedComponentIDs-String
// (Wire-Format der Subscriber-API, leer = alle Komponenten) und der
// Array-Form, die das Komponenten-Multi-Select im Formular braucht.
export function idsFromCsv(csv) {
  return (csv || '').split(',').filter(Boolean)
}

export function csvFromIds(ids) {
  return (ids || []).join(',')
}

// Komponentennamen für die Tabellen-Anzeige statt roher IDs.
// Unbekannte IDs (z.B. eine inzwischen gelöschte Komponente) fallen auf die
// rohe ID zurück, statt sie stillschweigend zu verschlucken.
export function componentLabelsFromCsv(csv, components, allLabel = 'alle') {
  const ids = idsFromCsv(csv)
  if (ids.length === 0) return allLabel
  return ids
    .map(id => components.find(c => String(c.ID) === id)?.Name ?? id)
    .join(', ')
}
