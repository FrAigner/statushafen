// Component tree as GET /api/pages/:id/components returns it
// (subcomponents in Children), for the tests of the component selects.
import type { BaseWrapper } from '@vue/test-utils'
export const componentTreeFixture = () => [
  { ID: 10, Name: 'API', Children: [{ ID: 12, Name: 'API Gateway (US)', Children: [] }] },
  { ID: 11, Name: 'Web' },
]

// Expected options in tree order, subcomponent indented (U+2003).
export const expectedTreeOptionLabels = ['API', ' API Gateway (US)', 'Web']

// Labels of the OnyxSelect stub's options in multiple mode. Only trim
// ASCII whitespace: text() or trim() would remove the indentation
// (U+2003) as well.
export const multiSelectOptionLabels = (wrapper: BaseWrapper<Node>): string[] =>
  wrapper.findAll('.onyx-select-stub-option').map(o => (o.element.textContent as string).replace(/^[ \t\n]+|[ \t\n]+$/g, ''))

// Extends the response of GET .../components by an archived component
// (ID 13). Returns a fetch wrapper that passes everything else on to `base`
// unchanged.
type FetchLike = (url: string, options?: RequestInit) => Promise<Response>
export const withArchivedComponent = (base: FetchLike) => async (url: string, options: RequestInit = {}) => {
  const res = await base(url, options)
  if ((options.method || 'GET') === 'GET' && /\/components$/.test(url)) {
    const body = await res.json()
    return { ...res, json: async () => [...body, { ID: 13, Name: 'Altsystem', ArchivedAt: '2026-11-01T10:00:00Z' }] }
  }
  return res
}
