import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import ForbiddenNotice from './ForbiddenNotice.vue'
import { axe } from '../test-support/axe.js'

describe('ForbiddenNotice', () => {
  beforeEach(() => { i18n.global.locale.value = 'de' })

  it('zeigt die deutsche Meldung als Warnhinweis', () => {
    const w = mount(ForbiddenNotice, { global: { plugins: [i18n] } })
    expect(w.attributes('role')).toBe('alert')
    expect(w.text()).toBe('Keine Berechtigung für diesen Bereich')
  })

  it('zeigt die englische Meldung', () => {
    i18n.global.locale.value = 'en'
    const w = mount(ForbiddenNotice, { global: { plugins: [i18n] } })
    expect(w.text()).toBe('No permission for this area')
  })

  it('hat keine axe-Funde', async () => {
    const w = mount(ForbiddenNotice, { global: { plugins: [i18n] } })
    expect(await axe(w)).toHaveNoViolations()
  })
})
