import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import StatusTiles from './StatusTiles.vue'

const mountTiles = (modelValue) => mount(StatusTiles, { props: { modelValue }, global: { plugins: [i18n] } })

describe('StatusTiles', () => {
  it('zeigt vier Kacheln mit Statussymbol und Text (nie nur Farbe)', () => {
    const wrapper = mountTiles('operational')
    const tiles = wrapper.findAll('.status-tiles__tile')
    expect(tiles.map(t => t.text())).toEqual(['Betriebsbereit', 'Beeinträchtigt', 'Teilausfall', 'Großstörung'])
    expect(tiles.every(t => t.find('svg.status-icon').exists())).toBe(true)
  })

  it('wählt die Kachel zum Roh-Status des Backends vor', () => {
    const wrapper = mountTiles('partial_outage')
    const checked = wrapper.findAll('input').filter(i => i.element.checked)
    expect(checked.map(i => i.element.value)).toEqual(['partial_outage'])
  })

  it('bildet eine Radiogruppe (gleicher Name) und meldet den Roh-Status bei Auswahl', async () => {
    const wrapper = mountTiles('operational')
    const inputs = wrapper.findAll('input[type="radio"]')
    expect(new Set(inputs.map(i => i.attributes('name'))).size).toBe(1)
    await inputs[3].setValue(true)
    expect(wrapper.emitted('update:modelValue')).toEqual([['major_outage']])
  })
})
