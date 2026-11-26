// axe-core check for component tests.
//
// Usage:
//   const wrapper = mount(Comp, { global: { plugins: [i18n] } })
//   expect(await axe(wrapper)).toHaveNoViolations()
//
// Either a VTU wrapper or a DOM element can be passed. For a wrapper the
// mount container is checked, so that components with several root nodes
// (fragments) are covered completely. axe needs the tree in the document
// (e.g. for <label for>); a tree mounted without attachTo is therefore
// attached to document.body for the duration of the check and detached
// again afterwards, so that existing tests do not have to change their mount.
// Content moved out via Teleport lies outside the container and is not
// checked.
//
// Why only selected rules instead of the full WCAG set:
// - The tests run in happy-dom without layout and without CSS. Rules that
//   need rendered colours, sizes or visibility (color-contrast,
//   target-size, scrollable-region-focusable) only produce noise there
//   or nothing at all. Contrast still has to be checked in the browser.
// - Usually a single view without the app frame is mounted. Page rules
//   (region, landmark-*, page-has-heading-one, document-title, html-has-lang,
//   bypass) would therefore hit every fragment and belong in an E2E run.
// - sit-onyx is replaced by src/test-support/onyxStubs.ts in many tests.
//   The stubs only mimic the labelling (label -> aria-label or button
//   text), not onyx' internal markup. What is checked is whether our
//   views give the components a label; onyx' own markup is the library's
//   business. Structural rules like nested-interactive or list/listitem
//   would produce findings on the stubs that do not exist in the real
//   markup (or miss them the other way round) and therefore stay off.
// What remains are the rules that matter: names for buttons,
// links, images and form fields as well as valid ARIA attributes.
import axeCore from 'axe-core'
import type { Result } from 'axe-core'
import { expect } from 'vitest'

export const A11Y_RULES = [
  'button-name',
  'input-button-name',
  'input-image-alt',
  'label',
  'select-name',
  'link-name',
  'image-alt',
  'svg-img-alt',
  'role-img-alt',
  'aria-allowed-attr',
  'aria-allowed-role',
  'aria-command-name',
  'aria-dialog-name',
  'aria-hidden-focus',
  'aria-input-field-name',
  'aria-prohibited-attr',
  'aria-required-attr',
  'aria-roles',
  'aria-toggle-field-name',
  'aria-valid-attr',
  'aria-valid-attr-value',
  'duplicate-id-aria',
]

function topAncestor(node: Node): Node {
  let root = node
  while (root.parentNode) root = root.parentNode
  return root
}

export async function axe(target: { element: Node } | Element): Promise<Result[]> {
  const element = 'element' in target && target.element ? ((target.element.parentElement || target.element) as Element) : target as Element
  const root = topAncestor(element)
  const detached = root !== document
  if (detached) document.body.appendChild(root)
  try {
    const result = await axeCore.run(element, {
      runOnly: { type: 'rule', values: A11Y_RULES },
      resultTypes: ['violations'],
    })
    return result.violations
  } finally {
    if (detached) (root as ChildNode).remove()
  }
}

// Show only the first nodes per rule: lists like the 90 availability bars
// would otherwise produce pages of messages with identical markup.
const MAX_NODES = 3

export function formatViolations(violations: Result[]): string {
  return violations
    .map(v => {
      const shown = v.nodes.slice(0, MAX_NODES).map(n => `  ${n.html}`)
      const rest = v.nodes.length - MAX_NODES
      if (rest > 0) shown.push(`  … und ${rest} weitere`)
      return `${v.id}: ${v.help}\n${shown.join('\n')}`
    })
    .join('\n')
}

expect.extend({
  toHaveNoViolations(violations: Result[]) {
    const pass = violations.length === 0
    return {
      pass,
      message: () => (pass
        ? 'axe-Funde erwartet, aber keine gefunden'
        : `axe-Funde:\n${formatViolations(violations)}`),
    }
  },
})

// Type for the matcher registered above (expect(await axe(wrapper)).toHaveNoViolations()).
declare module 'vitest' {
  interface Matchers<R extends void | Promise<void> = void | Promise<void>, T = unknown> {
    toHaveNoViolations(): R
  }
}
