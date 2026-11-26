// Direction C: operate the row menu (RowMenu) in component tests.
// The entries are in the DOM even in the closed menu (v-show), so a click on
// the entry suffices; the button opens the menu for keyboard and mouse.
import type { BaseWrapper, DOMWrapper } from '@vue/test-utils'

export const menuItem = (scope: BaseWrapper<Node>, text: string): DOMWrapper<Element> | undefined =>
  scope.findAll('[role="menuitem"]').find(i => i.text().includes(text))

export const clickMenuItem = (scope: BaseWrapper<Node>, text: string): Promise<void> => (menuItem(scope, text) as DOMWrapper<Element>).trigger('click')
