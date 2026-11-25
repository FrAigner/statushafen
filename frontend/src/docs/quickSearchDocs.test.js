// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { DOC_SOURCES, renderMarkdown } from './index.js'
import de from '../i18n/locales/de.json'
import en from '../i18n/locales/en.json'

// The built-in docs describe the admin quick search with its UI name and shortcuts.
describe('quick search docs', () => {
  it.each([['de', de], ['en', en]])('%s: getting started explains the quick search', (loc, messages) => {
    const md = DOC_SOURCES[loc]['erste-schritte']
    expect(md).toContain(messages.quickSearch.label)
    expect(md).toMatch(/Ctrl\+K|Strg\+K/)
    expect(md).toContain('`/`')
    expect(renderMarkdown(md).headings.some(h => h.text === messages.quickSearch.label)).toBe(true)
  })
})
