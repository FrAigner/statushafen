import { describe, it, expect } from 'vitest'
import { stripHtmlComments } from './sourceText'

describe('stripHtmlComments', () => {
  it('removes template comments, also across lines', () => {
    expect(stripHtmlComments('a<!-- x -->b<!--\n y\n-->c')).toBe('abc')
  })

  it('removes comments uncovered by removing another one', () => {
    expect(stripHtmlComments('a<!<!-- x -->-- hidden -->b')).toBe('ab')
  })

  it('leaves text without comments unchanged', () => {
    expect(stripHtmlComments('<div class="x">// not a template comment</div>')).toBe('<div class="x">// not a template comment</div>')
  })
})
