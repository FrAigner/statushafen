import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import KpiCard from './KpiCard.vue'
import { axe } from '../test-support/axe.js'

const mountKpi = props => mount(KpiCard, { props, global: { plugins: [i18n] } })

describe('KpiCard', () => {
  it('zeigt Beschriftung und Wert, ohne Alarm neutral', () => {
    const w = mountKpi({ label: 'Aktive Statusseiten', value: 4 })
    expect(w.find('.kpi__label').text()).toBe('Aktive Statusseiten')
    expect(w.find('.kpi__value').text()).toBe('4')
    expect(w.classes()).not.toContain('kpi--alert')
    expect(w.find('.kpi__sr').exists()).toBe(false)
  })

  it('akzeptiert Text als Wert, auch 0', () => {
    expect(mountKpi({ label: 'Verfügbarkeit', value: '99,9 %' }).find('.kpi__value').text()).toBe('99,9 %')
    expect(mountKpi({ label: 'Offen', value: 0 }).find('.kpi__value').text()).toBe('0')
  })

  it('markiert Alarm in Farbe und für Screenreader im Text', () => {
    i18n.global.locale.value = 'de'
    const w = mountKpi({ label: 'Offene Vorfälle', value: 3, alert: true })
    expect(w.classes()).toContain('kpi--alert')
    expect(w.find('.kpi__sr').text()).toBe('Handlungsbedarf')
  })

  it('hat keine axe-Funde', async () => {
    expect(await axe(mountKpi({ label: 'Offene Vorfälle', value: 3, alert: true }))).toHaveNoViolations()
  })
})
