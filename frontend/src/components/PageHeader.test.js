import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import PageHeader from './PageHeader.vue'
import { axe } from '../test-support/axe.js'

const RouterLinkStub = {
  props: ['to'],
  template: '<a :href="to"><slot /></a>',
}
const mountHeader = (props, slots = {}) => mount(PageHeader, {
  props, slots, global: { plugins: [i18n], stubs: { 'router-link': RouterLinkStub } },
})

describe('PageHeader', () => {
  it('zeigt den Titel als einzige h1', () => {
    const w = mountHeader({ title: 'Vorfälle' })
    expect(w.findAll('h1')).toHaveLength(1)
    expect(w.find('h1').text()).toBe('Vorfälle')
  })

  it('rendert mit level=2 eine h2 statt der h1 (Admin: die Topbar trägt die h1)', () => {
    const w = mountHeader({ title: 'Vorfälle', level: 2 })
    expect(w.find('h1').exists()).toBe(false)
    expect(w.find('h2.page-head__title').text()).toBe('Vorfälle')
  })

  it('lässt Untertitel, Brotkrumen und Aktionen weg, wenn nicht gesetzt', () => {
    const w = mountHeader({ title: 'Vorfälle' })
    expect(w.find('.page-head__subtitle').exists()).toBe(false)
    expect(w.find('nav').exists()).toBe(false)
    expect(w.find('.page-head__actions').exists()).toBe(false)
  })

  it('zeigt Untertitel und Aktionen', () => {
    const w = mountHeader({ title: 'Vorfälle', subtitle: 'Alle offenen und behobenen' }, { actions: '<button type="button">Neu</button>' })
    expect(w.find('.page-head__subtitle').text()).toBe('Alle offenen und behobenen')
    expect(w.find('.page-head__actions button').text()).toBe('Neu')
  })

  it('rendert Brotkrumen: Einträge mit to als Link, der letzte als aktuelle Seite', () => {
    i18n.global.locale.value = 'de'
    const w = mountHeader({
      title: 'Details',
      breadcrumbs: [{ label: 'Vorfälle', to: '/admin/incidents' }, { label: 'Details' }],
    })
    const nav = w.find('nav')
    expect(nav.attributes('aria-label')).toBe('Brotkrumen')
    const items = nav.findAll('li')
    expect(items).toHaveLength(2)
    expect(items[0].find('a').attributes('href')).toBe('/admin/incidents')
    expect(items[0].find('[aria-current]').exists()).toBe(false)
    expect(items[1].find('a').exists()).toBe(false)
    expect(items[1].find('span').attributes('aria-current')).toBe('page')
  })

  it('hat keine axe-Funde', async () => {
    const w = mountHeader(
      { title: 'Details', subtitle: 'x', breadcrumbs: [{ label: 'Vorfälle', to: '/a' }, { label: 'Details' }] },
      { actions: '<button type="button">Neu</button>' },
    )
    expect(await axe(w)).toHaveNoViolations()
  })
})
