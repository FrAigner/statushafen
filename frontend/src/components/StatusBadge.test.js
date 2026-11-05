import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import StatusBadge from './StatusBadge.vue'
import { axe } from '../test-support/axe.js'

const mountBadge = (props = {}, slots = {}) => mount(StatusBadge, { props, slots, global: { plugins: [i18n] } })

beforeEach(() => {
  i18n.global.locale.value = 'de'
})

describe('StatusBadge', () => {
  it.each([
    ['operational', 'Betriebsbereit', 'operational'],
    ['degraded_performance', 'Beeinträchtigt', 'degraded'],
    ['partial_outage', 'Teilausfall', 'partial'],
    ['major_outage', 'Großstörung', 'major'],
    ['maintenance', 'Wartung', 'maintenance'],
  ])('%s zeigt Text %s mit Symbol und Stufenklasse', (status, text, level) => {
    const w = mountBadge({ status })
    expect(w.text()).toBe(text)
    expect(w.classes()).toContain(`st-badge--${level}`)
    expect(w.findComponent({ name: 'StatusIcon' }).exists()).toBe(true)
  })

  it('behält den Wortlaut „Beeinträchtigt“ und folgt der Sprache', () => {
    expect(mountBadge({ status: 'degraded' }).text()).toBe('Beeinträchtigt')
    i18n.global.locale.value = 'en'
    expect(mountBadge({ status: 'degraded' }).text()).toBe('Degraded Performance')
  })

  it('Variante soft (Standard) hat Fläche, plain nicht', () => {
    expect(mountBadge({ status: 'operational' }).classes()).toContain('st-badge--soft')
    const plain = mountBadge({ status: 'operational', variant: 'plain' })
    expect(plain.classes()).toContain('st-badge--plain')
    expect(plain.classes()).not.toContain('st-badge--soft')
  })

  it('Größe sm (Standard) und md ändern Klasse und Symbolgröße', () => {
    const sm = mountBadge({ status: 'operational' })
    expect(sm.classes()).toContain('st-badge--sm')
    expect(sm.find('svg').attributes('width')).toBe('14')
    const md = mountBadge({ status: 'operational', size: 'md' })
    expect(md.classes()).toContain('st-badge--md')
    expect(md.find('svg').attributes('width')).toBe('18')
  })

  it('label und Slot ersetzen den Text, die Form bleibt', () => {
    expect(mountBadge({ status: 'major_outage', label: 'Datenbank down' }).text()).toBe('Datenbank down')
    const w = mountBadge({ status: 'major_outage' }, { default: 'Eigener Text' })
    expect(w.text()).toBe('Eigener Text')
    expect(w.find('svg').attributes('data-shape')).toBe('octagon-cross')
  })

  it('zeigt bei unbekanntem Status neutral den Rohwert, bei fehlendem nichts', () => {
    const unknown = mountBadge({ status: 'kaputt' })
    expect(unknown.text()).toBe('kaputt')
    expect(unknown.classes()).toContain('st-badge--unknown')
    const none = mountBadge({})
    expect(none.text()).toBe('')
    expect(none.classes()).toContain('st-badge--unknown')
  })

  it('verwirft ungültige Varianten und Größen (Validator)', () => {
    const { variant, size } = StatusBadge.props
    expect(variant.validator('soft') && variant.validator('plain')).toBe(true)
    expect(variant.validator('bunt')).toBe(false)
    expect(size.validator('sm') && size.validator('md')).toBe(true)
    expect(size.validator('xl')).toBe(false)
  })

  it('hat keine axe-Funde', async () => {
    expect(await axe(mountBadge({ status: 'partial_outage' }))).toHaveNoViolations()
  })
})
