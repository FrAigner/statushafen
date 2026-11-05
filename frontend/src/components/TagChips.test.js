import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import TagChips from './TagChips.vue'

const TAGS = [{ ID: 5, Name: 'EU' }, { ID: 6, Name: 'US' }, { ID: 7, Name: 'DE' }]
const mountChips = (selectedIds, tags = TAGS) =>
  mount(TagChips, { props: { tags, selectedIds }, global: { plugins: [i18n] } })

describe('TagChips', () => {
  it('zeigt gewählte Tags als Chips in Auswahlreihenfolge und die übrigen unter „+ Tag“', () => {
    const wrapper = mountChips([7, 5])
    expect(wrapper.findAll('.tag-chip').map(c => c.text())).toEqual(['DE', 'EU'])
    const options = wrapper.findAll('.tag-chips__add option')
    expect(options.map(o => o.text())).toEqual(['+ Tag', 'US'])
    expect(wrapper.find('.tag-chips__add').attributes('aria-label')).toBe('Tag hinzufügen')
  })

  it('entfernt über das × (benannt nach dem Tag) per toggle', async () => {
    const wrapper = mountChips([5])
    const remove = wrapper.find('.tag-chip__remove')
    expect(remove.attributes('aria-label')).toBe('Tag „EU“ entfernen')
    await remove.trigger('click')
    expect(wrapper.emitted('toggle')).toEqual([[5]])
  })

  it('fügt über das Select hinzu (Zahl als ID) und setzt es auf „+ Tag“ zurück', async () => {
    const wrapper = mountChips([5])
    const select = wrapper.find('.tag-chips__add')
    await select.setValue('6')
    expect(wrapper.emitted('toggle')).toEqual([[6]])
    expect(select.element.value).toBe('')
  })

  it('ignoriert die Platzhalter-Option', async () => {
    const wrapper = mountChips([5])
    await wrapper.find('.tag-chips__add').setValue('')
    expect(wrapper.emitted('toggle')).toBeUndefined()
  })

  it('blendet „+ Tag“ aus, wenn alle Tags gewählt sind', () => {
    expect(mountChips([5, 6, 7]).find('.tag-chips__add').exists()).toBe(false)
  })

  it('zeigt eine IDs ohne bekanntes Tag nicht an (und bleibt stabil)', () => {
    const wrapper = mountChips([99, 5])
    expect(wrapper.findAll('.tag-chip').map(c => c.text())).toEqual(['EU'])
  })

  it('zeigt ohne vorhandene Tags den Hinweis statt Chips und Select', () => {
    const wrapper = mountChips([], [])
    expect(wrapper.text()).toContain('Keine Tags vorhanden')
    expect(wrapper.find('select').exists()).toBe(false)
  })
})
