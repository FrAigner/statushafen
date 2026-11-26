// Redesign stage 4b: guard against relapsing into inline styles and
// fixed hex colours in a view. Checked are the rendered DOM (no
// style attribute) and the source (no style="…", no :style, no hex
// outside of comments). Colours belong in tokens (assets/tokens.css).
import { readFileSync } from 'fs'
import { dirname, resolve } from 'path'
import { fileURLToPath } from 'url'
import { expect } from 'vitest'
import type { BaseWrapper } from '@vue/test-utils'

/**
 * @param wrapper     mounted VTU wrapper of the view (in the state with data)
 * @param testFileUrl `import.meta.url` of the test
 * @param viewFile    file name of the view next to the test, e.g. 'AdminOverview.vue'
 */
export function expectNoInlineStyles(wrapper: BaseWrapper<Node>, testFileUrl: string, viewFile: string): void {
  // sit-onyx internally sets anchor variables via style (tooltips); that is not our markup.
  // The row menu (RowMenu) hides via v-show and positions itself fixed via style.
  const own = wrapper.findAll('[style]').filter(el => !el.element.closest('.onyx-component, .row-menu__list'))
  expect(own.map(el => el.html().slice(0, 120))).toEqual([])

  const source = readFileSync(resolve(dirname(fileURLToPath(testFileUrl)), viewFile), 'utf-8')
  // Template comments are removed until none is left, so that removing one cannot uncover another.
  let withoutHtmlComments = source.replace(/\/\*[\s\S]*?\*\//g, '')
  let previous: string
  do {
    previous = withoutHtmlComments
    withoutHtmlComments = withoutHtmlComments.replace(/<!--[\s\S]*?-->/g, '')
  } while (withoutHtmlComments !== previous)
  const code = withoutHtmlComments
    .replace(/(^|\s)\/\/.*$/gm, '$1') // JS line comments (they contain reference numbers like #496)
  expect(code).not.toMatch(/\sstyle="/)
  expect(code).not.toMatch(/:style=/)
  expect(code).not.toMatch(/#[0-9a-fA-F]{3,8}\b/)
}
