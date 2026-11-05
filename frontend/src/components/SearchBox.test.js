import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SearchBox from './SearchBox.vue'
import { axe } from '../test-support/axe.js'

describe('SearchBox', () => {
  it('bindet v-model, setzt aria-label und nimmt das Label als Platzhalter', async () => {
    const wrapper = mount(SearchBox, { props: { label: 'Tag suchen', modelValue: '', 'onUpdate:modelValue': v => wrapper.setProps({ modelValue: v }) } })
    const input = wrapper.find('input[type="search"]')
    expect(input.attributes('aria-label')).toBe('Tag suchen')
    expect(input.attributes('placeholder')).toBe('Tag suchen')
    await input.setValue('eu')
    expect(wrapper.props('modelValue')).toBe('eu')
  })

  it('zeigt Wert und eigenen Platzhalter und meldet Eingaben als update:modelValue', async () => {
    const wrapper = mount(SearchBox, { props: { modelValue: 'abc', label: 'Seite suchen', placeholder: 'Name…' } })
    const input = wrapper.find('input[type="search"]')
    expect(input.element.value).toBe('abc')
    expect(input.attributes('aria-label')).toBe('Seite suchen')
    expect(input.attributes('placeholder')).toBe('Name…')
    await input.setValue('neu')
    expect(wrapper.emitted('update:modelValue')).toEqual([['neu']])
  })

  it('verwendet einen eigenen Platzhalter und reicht Attribute an das Feld durch', () => {
    const wrapper = mount(SearchBox, { props: { label: 'L', placeholder: 'P' }, attrs: { 'data-x': '1' } })
    const input = wrapper.find('input')
    expect(input.attributes('placeholder')).toBe('P')
    expect(input.attributes('data-x')).toBe('1')
    expect(wrapper.find('.comp-search').attributes('data-x')).toBeUndefined()
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = mount(SearchBox, { props: { modelValue: '', label: 'Suche' }, attachTo: document.body })
    expect(await axe(wrapper)).toHaveNoViolations()
    wrapper.unmount()
  })
})
