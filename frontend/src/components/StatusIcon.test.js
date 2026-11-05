import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusIcon from './StatusIcon.vue'
import { axe } from '../test-support/axe.js'

const SHAPES = {
  operational: 'circle-check',
  degraded_performance: 'triangle-exclamation',
  partial_outage: 'diamond-minus',
  major_outage: 'octagon-cross',
  maintenance: 'square-clock',
}

describe('StatusIcon', () => {
  it.each(Object.entries(SHAPES))('%s zeichnet die Form %s mit Zeichen', (status, shape) => {
    const w = mount(StatusIcon, { props: { status } })
    expect(w.find('svg').attributes('data-shape')).toBe(shape)
    expect(w.find('.si-shape').exists()).toBe(true)
    expect(w.find('.si-glyph').exists()).toBe(true)
  })

  it('setzt die Stufenklasse für die Farbe (Kurz- und Rohwert gleich)', () => {
    expect(mount(StatusIcon, { props: { status: 'degraded_performance' } }).classes()).toContain('status-icon--degraded')
    expect(mount(StatusIcon, { props: { status: 'degraded' } }).classes()).toContain('status-icon--degraded')
    expect(mount(StatusIcon, { props: { status: 'maintenance' } }).classes()).toContain('status-icon--maintenance')
  })

  it('zeichnet unbekannte Status neutral: offener Kreis, keine Statusfarbe', () => {
    for (const status of ['kaputt', null]) {
      const w = mount(StatusIcon, { props: { status } })
      expect(w.classes()).toContain('status-icon--unknown')
      expect(w.find('svg').attributes('data-shape')).toBe('circle')
      expect(w.find('.si-shape--open').exists()).toBe(true)
      expect(w.find('.si-glyph').exists()).toBe(false)
    }
  })

  it('ist ohne label dekorativ (aria-hidden, keine Rolle)', () => {
    const svg = mount(StatusIcon, { props: { status: 'operational' } }).find('svg')
    expect(svg.attributes('aria-hidden')).toBe('true')
    expect(svg.attributes('role')).toBeUndefined()
    expect(svg.attributes('aria-label')).toBeUndefined()
  })

  it('bekommt mit label Rolle img und einen Namen', () => {
    const svg = mount(StatusIcon, { props: { status: 'operational', label: 'In Betrieb' } }).find('svg')
    expect(svg.attributes('role')).toBe('img')
    expect(svg.attributes('aria-label')).toBe('In Betrieb')
    expect(svg.attributes('aria-hidden')).toBeUndefined()
  })

  it('übernimmt die Größe', () => {
    const svg = mount(StatusIcon, { props: { status: 'operational', size: 22 } }).find('svg')
    expect(svg.attributes('width')).toBe('22')
    expect(svg.attributes('height')).toBe('22')
  })

  it('hat keine axe-Funde (dekorativ und mit Namen)', async () => {
    expect(await axe(mount(StatusIcon, { props: { status: 'major_outage' } }))).toHaveNoViolations()
    expect(await axe(mount(StatusIcon, { props: { status: 'major_outage', label: 'Großstörung' } }))).toHaveNoViolations()
  })
})
