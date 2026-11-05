import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import PaginationBar from './PaginationBar.vue'
import { axe } from '../test-support/axe.js'

const mountBar = (props = {}) => mount(PaginationBar, { props: { total: 230, page: 1, pageSize: 50, ...props }, global: { plugins: [i18n] } })

describe('PaginationBar', () => {
  it('zeigt Zeilen x–y von z in einer höflichen Live-Region', () => {
    i18n.global.locale.value = 'de'
    const info = mountBar().find('.pager-info')
    expect(info.text()).toBe('Zeilen 1–50 von 230')
    expect(info.attributes('role')).toBe('status')
    expect(info.attributes('aria-live')).toBe('polite')
  })

  it('kürzt die letzte Seite und zählt bei 0 Treffern von 0', async () => {
    const wrapper = mountBar({ page: 5 })
    expect(wrapper.find('.pager-info').text()).toBe('Zeilen 201–230 von 230')
    await wrapper.setProps({ total: 0, page: 1 })
    expect(wrapper.find('.pager-info').text()).toBe('Zeilen 0–0 von 0')
  })

  it('klemmt eine zu große Seite auf die letzte', () => {
    expect(mountBar({ page: 99 }).find('.pager-info').text()).toBe('Zeilen 201–230 von 230')
  })

  it('Zurück ist auf Seite 1 und Weiter auf der letzten Seite gesperrt', async () => {
    const wrapper = mountBar()
    expect(wrapper.findAll('button')[0].attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button')[1].attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ page: 5 })
    expect(wrapper.findAll('button')[1].attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button')[0].attributes('disabled')).toBeUndefined()
  })

  it('Weiter und Zurück melden die neue Seite und navigate', async () => {
    const wrapper = mountBar({ page: 2 })
    await wrapper.findAll('button')[1].trigger('click')
    await wrapper.findAll('button')[0].trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[3], [2]])
    expect(wrapper.emitted('navigate')).toHaveLength(2)
  })

  it('Seitengröße wählen setzt auf Seite 1 zurück', async () => {
    const wrapper = mountBar({ page: 3 })
    expect(wrapper.findAll('select option').map(o => o.text())).toEqual(['25', '50', '100'])
    await wrapper.find('select').setValue('100')
    expect(wrapper.emitted('update:pageSize')).toEqual([[100]])
    expect(wrapper.emitted('update:page')).toEqual([[1]])
    expect(wrapper.emitted('navigate')).toHaveLength(1)
  })

  it('hat auf Englisch übersetzte Texte und keine axe-Funde', async () => {
    i18n.global.locale.value = 'en'
    const wrapper = mountBar()
    expect(wrapper.find('.pager-info').text()).toBe('Rows 1–50 of 230')
    expect(wrapper.find('nav').attributes('aria-label')).toBe('Page navigation')
    expect(await axe(wrapper)).toHaveNoViolations()
    i18n.global.locale.value = 'de'
  })
})
