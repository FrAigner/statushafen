// Stufe 4c: Felder in FormField haben dynamische IDs (useId).
// Tests finden sie über den Text ihres <label>, so wie es auch Nutzer und
// Screenreader tun, statt über aria-label-Attribute der Onyx-Stubs.
export function fieldByLabel(wrapper, text) {
  const label = wrapper.findAll('label').find(l => l.text() === text)
  if (!label) throw new Error(`Kein Label "${text}" gefunden`)
  return wrapper.find(`[id="${label.attributes('for')}"]`)
}

export const hasLabel = (wrapper, text) => wrapper.findAll('label').some(l => l.text() === text)

// Öffnet den QuickStatus (Listbox-Button) und wählt die Option mit dem Text.
export async function chooseQuickStatus(button, optionText) {
  await button.trigger('click')
  const options = button.element.closest('.quick-status').querySelectorAll('[role="option"]')
  const option = [...options].find(o => o.textContent.trim() === optionText)
  if (!option) throw new Error(`Keine Option "${optionText}"`)
  option.click()
}
