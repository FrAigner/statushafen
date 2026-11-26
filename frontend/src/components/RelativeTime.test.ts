import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import RelativeTime from './RelativeTime.vue'

describe('RelativeTime', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-04T10:00:00Z'))
  })
  afterEach(() => vi.useRealTimers())

  it('zeigt die Zeit relativ, den genauen Zeitpunkt im title und im datetime-Attribut', () => {
    const wrapper = mount(RelativeTime, { props: { value: '2026-07-01T10:00:00Z' }, global: { plugins: [i18n] } })
    const time = wrapper.find('time')
    expect(time.text()).toBe('vor 3 Tagen')
    expect(time.attributes('datetime')).toBe('2026-07-01T10:00:00Z')
    expect(time.attributes('title')).toBe(new Date('2026-07-01T10:00:00Z').toLocaleString('de-DE', { dateStyle: 'short', timeStyle: 'short' }))
  })

  it('folgt der UI-Sprache', () => {
    i18n.global.locale.value = 'en'
    const wrapper = mount(RelativeTime, { props: { value: '2026-07-01T10:00:00Z' }, global: { plugins: [i18n] } })
    expect(wrapper.find('time').text()).toBe('3 days ago')
  })

  it('zeigt ohne Wert einen Strich statt eines leeren Zeitelements', () => {
    const wrapper = mount(RelativeTime, { global: { plugins: [i18n] } })
    expect(wrapper.find('time').exists()).toBe(false)
    expect(wrapper.text()).toBe('—')
  })
})
