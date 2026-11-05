import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SegmentControl from './SegmentControl.vue'
import { axe } from '../test-support/axe.js'

const options = [{ value: 'all', label: 'Alle' }, { value: 'open', label: 'Offen' }]

describe('SegmentControl', () => {
  it('ist eine benannte Gruppe aus Knöpfen, der gewählte Wert ist aria-pressed', () => {
    const wrapper = mount(SegmentControl, { props: { modelValue: 'open', options, label: 'Nach Status filtern' } })
    expect(wrapper.find('[role="group"]').attributes('aria-label')).toBe('Nach Status filtern')
    const buttons = wrapper.findAll('button')
    expect(buttons.map(b => b.text())).toEqual(['Alle', 'Offen'])
    expect(buttons.map(b => b.attributes('aria-pressed'))).toEqual(['false', 'true'])
    expect(buttons[1].classes()).toContain('segment__item--on')
    expect(buttons.every(b => b.attributes('type') === 'button')).toBe(true)
  })

  it('meldet den gewählten Wert', async () => {
    const wrapper = mount(SegmentControl, { props: { modelValue: 'all', options, label: 'L' } })
    await wrapper.findAll('button')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['open']])
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = mount(SegmentControl, { props: { modelValue: 'all', options, label: 'L' }, attachTo: document.body })
    expect(await axe(wrapper)).toHaveNoViolations()
    wrapper.unmount()
  })
})
