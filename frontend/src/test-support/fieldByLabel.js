// Redesign Stufe 4b: Formularfelder in Component-Tests über ihre
// sichtbare Beschriftung finden (Label -> for -> Feld), wie ein Mensch bzw.
// Screenreader sie findet; kein Verlass auf CSS-Klassen oder aria-label der
// Onyx-Stubs. Gibt den Wrapper des Feldes zurück. Wirft, wenn das Label fehlt
// oder nicht mit einem Feld verbunden ist (das wäre selbst ein Fehler).
export function fieldByLabel(wrapper, text) {
  const label = wrapper.findAll('label').find(l => l.text() === text)
  if (!label) throw new Error(`Kein Label "${text}" gefunden`)
  const id = label.attributes('for')
  const field = id ? wrapper.find(`[id="${id}"]`) : null
  if (!field || !field.exists()) throw new Error(`Label "${text}" ist mit keinem Feld verbunden`)
  return field
}
