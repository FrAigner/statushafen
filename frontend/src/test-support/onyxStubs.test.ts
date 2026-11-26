import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { OnyxSelectStub } from './onyxStubs'

describe('test-support/onyxStubs: OnyxSelectStub (multiple)', () => {
  const options = [{ value: 'a', label: 'A' }, { value: 'b', label: 'B' }]

  it('wählt ohne bisherigen Wert die erste Option aus', async () => {
    const wrapper = mount(OnyxSelectStub, { props: { multiple: true, options, label: 'Wahl' } })
    await wrapper.findAll('input[type="checkbox"]')[0].trigger('change')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['a']]])
  })

  it('schaltet eine gewählte Option ab und eine weitere hinzu', async () => {
    const wrapper = mount(OnyxSelectStub, { props: { multiple: true, options, label: 'Wahl', modelValue: ['a'] } })
    const boxes = wrapper.findAll('input[type="checkbox"]')
    await boxes[0].trigger('change')
    await boxes[1].trigger('change')
    expect(wrapper.emitted('update:modelValue')).toEqual([[[]], [['a', 'b']]])
  })
})
