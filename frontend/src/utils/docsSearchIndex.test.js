import { describe, it, expect } from 'vitest'
import { buildDocsEntries } from './docsSearchIndex'

const sources = {
  de: {
    sso: '# Single Sign-on\n\nText\n\n## OIDC einrichten\n\nmehr\n\n### Felder &amp; `Scopes`\n',
    faq: '# FAQ\n',
  },
  en: { sso: '# Single sign-on\n\n## Set up OIDC\n' },
}

describe('buildDocsEntries', () => {
  it('lists section titles and their h2/h3 headings with anchors', () => {
    const entries = buildDocsEntries('de', { sources, sectionIds: ['sso', 'faq'] })
    expect(entries).toEqual([
      { kind: 'doc', key: 'sso', label: 'Single Sign-on', context: 'Single Sign-on', to: '/docs/sso' },
      { kind: 'doc', key: 'sso#oidc-einrichten', label: 'OIDC einrichten', context: 'Single Sign-on', to: '/docs/sso#oidc-einrichten' },
      { kind: 'doc', key: 'sso#felder-scopes', label: 'Felder & Scopes', context: 'Single Sign-on', to: '/docs/sso#felder-scopes' },
      { kind: 'doc', key: 'faq', label: 'FAQ', context: 'FAQ', to: '/docs/faq' },
    ])
  })

  it('skips missing sections and falls back to the slug without a title', () => {
    const entries = buildDocsEntries('de', { sources: { de: { a: 'Nur Text' } }, sectionIds: ['a', 'missing'] })
    expect(entries).toEqual([{ kind: 'doc', key: 'a', label: 'a', context: 'a', to: '/docs/a' }])
  })

  it('uses the current language and falls back to German', () => {
    const entries = buildDocsEntries('en', { sources, sectionIds: ['sso', 'faq'] })
    expect(entries.map(e => e.label)).toEqual(['Single sign-on', 'Set up OIDC', 'FAQ'])
  })

  it('builds entries from the real built-in docs', () => {
    const entries = buildDocsEntries('de')
    expect(entries.some(e => e.key === 'sso')).toBe(true)
    expect(entries.length).toBeGreaterThan(21)
  })
})
