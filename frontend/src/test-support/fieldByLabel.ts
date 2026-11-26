// Redesign stage 4b: find form fields in component tests by their
// visible label (label -> for -> field), the way a human or a screen reader
// finds them; no reliance on CSS classes or aria-label of the Onyx stubs.
// Returns the field's wrapper. Throws if the label is missing or not
// connected to a field (that would be a bug in itself).
import type { BaseWrapper, DOMWrapper } from '@vue/test-utils'

export function fieldByLabel(wrapper: BaseWrapper<Node>, text: string): DOMWrapper<Element> {
  const label = wrapper.findAll('label').find(l => l.text() === text)
  if (!label) throw new Error(`Kein Label "${text}" gefunden`)
  const id = label.attributes('for')
  const field = id ? wrapper.find(`[id="${id}"]`) : null
  if (!field || !field.exists()) throw new Error(`Label "${text}" ist mit keinem Feld verbunden`)
  return field
}
