// Conversion between the comma-separated SubscribedComponentIDs string
// (wire format of the subscriber API, empty = all components) and the
// array form the component multi-select in the form needs.
export function idsFromCsv(csv: string | null | undefined): string[] {
  return (csv || '').split(',').filter(Boolean)
}

export function csvFromIds(ids: readonly (string | number)[] | null | undefined): string {
  return (ids || []).join(',')
}

// Component names for the table display instead of raw IDs.
// Unknown IDs (e.g. a component deleted in the meantime) fall back to the
// raw ID instead of silently swallowing it.
export function componentLabelsFromCsv(csv: string | null | undefined, components: readonly { ID: number | string; Name: string }[], allLabel = 'alle'): string {
  const ids = idsFromCsv(csv)
  if (ids.length === 0) return allLabel
  return ids
    .map(id => components.find(c => String(c.ID) === id)?.Name ?? id)
    .join(', ')
}
