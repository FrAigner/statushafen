import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import TagCheckboxList from './TagCheckboxList.vue'
import { axe } from '../test-support/axe.js'

function mountList(props) {
  return mount(TagCheckboxList, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('TagCheckboxList', () => {
  it('zeigt den Empty-Hint, wenn keine Tags vorhanden sind', () => {
    i18n.global.locale.value = 'de'
    const wrapper = mountList({ tags: [], selectedIds: [] })

    expect(wrapper.find('.empty-state').text()).toBe(
      'Keine Tags vorhanden. Unter „Tags" in der Verwaltung anlegen.'
    )
    expect(wrapper.find('.tag-checkbox-list').exists()).toBe(false)
  })

  it('nutzt einen übergebenen emptyHint statt des i18n-Defaults', () => {
    const wrapper = mountList({ tags: [], selectedIds: [], emptyHint: 'Custom Hint' })

    expect(wrapper.find('.empty-state').text()).toBe('Custom Hint')
  })

  it('rendert eine Checkbox je Tag mit Namen', () => {
    const tags = [
      { ID: 1, Name: 'EU' },
      { ID: 2, Name: 'US' },
    ]
    const wrapper = mountList({ tags, selectedIds: [] })

    const labels = wrapper.findAll('.tag-checkbox')
    expect(labels).toHaveLength(2)
    expect(labels[0].text()).toContain('EU')
    expect(labels[1].text()).toContain('US')
  })

  it('markiert Checkboxen als checked, deren ID in selectedIds enthalten ist', () => {
    const tags = [
      { ID: 1, Name: 'EU' },
      { ID: 2, Name: 'US' },
    ]
    const wrapper = mountList({ tags, selectedIds: [2] })

    const inputs = wrapper.findAll('input[type="checkbox"]')
    expect(inputs[0].element.checked).toBe(false)
    expect(inputs[1].element.checked).toBe(true)
  })

  it('emittiert toggle mit der Tag-ID beim Ändern einer Checkbox', async () => {
    const tags = [{ ID: 1, Name: 'EU' }]
    const wrapper = mountList({ tags, selectedIds: [] })

    await wrapper.find('input[type="checkbox"]').setValue(true)

    expect(wrapper.emitted('toggle')).toEqual([[1]])
  })

  it('zeigt den seitengebundenen Hinweis nur für Tags mit StatusPageID', () => {
    const tags = [
      { ID: 1, Name: 'Global' },
      { ID: 2, Name: 'Seite A', StatusPageID: 5 },
    ]
    const wrapper = mountList({ tags, selectedIds: [] })

    const labels = wrapper.findAll('.tag-checkbox')
    expect(labels[0].find('.tag-scope-hint').exists()).toBe(false)
    expect(labels[1].find('.tag-scope-hint').exists()).toBe(true)
    expect(labels[1].find('.tag-scope-hint').text()).toBe('(seitengebunden)')
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = mountList({ tags: [{ ID: 1, Name: 'EU' }, { ID: 2, Name: 'US', StatusPageID: 5 }], selectedIds: [1] })
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
