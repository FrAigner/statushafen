import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import TagField from './TagField.vue'

const tags = [{ ID: 5, Name: 'DE' }, { ID: 6, Name: 'US' }]

describe('TagField', () => {
  beforeEach(() => { i18n.global.locale.value = 'de' })

  it('ist eine benannte Gruppe mit „optional“ und zeigt die gewählten Tags als Chips', () => {
    const wrapper = mount(TagField, { props: { label: 'Tags (Reichweite)', tags, selectedIds: [5] }, global: { plugins: [i18n] } })
    const group = wrapper.find('[role="group"]')
    expect(group.attributes('aria-labelledby')).toBe(wrapper.find('.form-field__label').attributes('id'))
    expect(wrapper.find('.form-field__label').text()).toBe('Tags (Reichweite)')
    expect(wrapper.find('.form-field__optional').text()).toBe('optional')
    expect(wrapper.findAll('.tag-chip').map(c => c.text())).toEqual(['DE'])
  })

  it('meldet Hinzufügen und Entfernen mit der Tag-ID', async () => {
    const wrapper = mount(TagField, { props: { label: 'Tags', tags, selectedIds: [5] }, global: { plugins: [i18n] } })
    await wrapper.find('.tag-chip__remove').trigger('click')
    await wrapper.find('select').setValue('6')
    expect(wrapper.emitted('toggle')).toEqual([[5], [6]])
  })
})
