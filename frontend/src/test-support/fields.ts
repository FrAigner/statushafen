// Stage 4c: fields in FormField have dynamic IDs (useId).
// Tests find them by the text of their <label>, as users and screen readers
// do too, instead of via aria-label attributes of the Onyx stubs.
import type { BaseWrapper, DOMWrapper } from '@vue/test-utils'

export function fieldByLabel(wrapper: BaseWrapper<Node>, text: string): DOMWrapper<Element> {
  const label = wrapper.findAll('label').find(l => l.text() === text)
  if (!label) throw new Error(`Kein Label "${text}" gefunden`)
  return wrapper.find(`[id="${label.attributes('for')}"]`)
}

export const hasLabel = (wrapper: BaseWrapper<Node>, text: string): boolean => wrapper.findAll('label').some(l => l.text() === text)

// Opens the QuickStatus (listbox button) and picks the option with the text.
export async function chooseQuickStatus(button: DOMWrapper<Element>, optionText: string): Promise<void> {
  await button.trigger('click')
  const options = (button.element.closest('.quick-status') as Element).querySelectorAll<HTMLElement>('[role="option"]')
  const option = [...options].find(o => (o.textContent as string).trim() === optionText)
  if (!option) throw new Error(`Keine Option "${optionText}"`)
  option.click()
}
