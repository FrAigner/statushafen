// Komponenten-Baum, wie ihn GET /api/pages/:id/components liefert
// (Unterkomponenten in Children), für die Tests der Komponenten-Auswahlen.
export const componentTreeFixture = () => [
  { ID: 10, Name: 'API', Children: [{ ID: 12, Name: 'API Gateway (US)', Children: [] }] },
  { ID: 11, Name: 'Web' },
]

// Erwartete Optionen in Baumreihenfolge, Unterkomponente eingerückt (U+2003).
export const expectedTreeOptionLabels = ['API', ' API Gateway (US)', 'Web']

// Beschriftungen der Optionen des OnyxSelect-Stubs im multiple-Modus. Nur
// ASCII-Leerraum abschneiden: text() bzw. trim() würde die Einrückung
// (U+2003) mit entfernen.
export const multiSelectOptionLabels = (wrapper) =>
  wrapper.findAll('.onyx-select-stub-option').map(o => o.element.textContent.replace(/^[ \t\n]+|[ \t\n]+$/g, ''))

// Antwort von GET .../components um eine archivierte Komponente
// (ID 13) ergänzen. Gibt einen fetch-Wrapper zurück, der alles andere
// unverändert an `base` weiterreicht.
export const withArchivedComponent = (base) => async (url, options = {}) => {
  const res = await base(url, options)
  if ((options.method || 'GET') === 'GET' && /\/components$/.test(url)) {
    const body = await res.json()
    return { ...res, json: async () => [...body, { ID: 13, Name: 'Altsystem', ArchivedAt: '2026-11-01T10:00:00Z' }] }
  }
  return res
}
