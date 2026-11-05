import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ListCard from './ListCard.vue'
import { axe } from '../test-support/axe.js'

describe('ListCard', () => {
  it('zeigt den Titel als h3, benennt die Karte danach und rendert den Inhalt bündig', () => {
    const wrapper = mount(ListCard, { props: { title: 'Alle Vorfälle' }, slots: { default: '<p class="inhalt">Tabelle</p>' } })
    const h3 = wrapper.find('h3.comp-card__title')
    expect(h3.text()).toBe('Alle Vorfälle')
    expect(wrapper.find('section.comp-card').attributes('aria-labelledby')).toBe(h3.attributes('id'))
    expect(wrapper.find('.inhalt').exists()).toBe(true)
    expect(wrapper.find('.list-card__body').exists()).toBe(false)
    expect(wrapper.find('.comp-card__tools').exists()).toBe(false)
    expect(wrapper.find('.comp-card__head').element.children).toHaveLength(1)
    expect(wrapper.find('section').classes()).not.toContain('comp-card--visible')
  })

  it('rendert die Werkzeuge im Kartenkopf', () => {
    const wrapper = mount(ListCard, {
      props: { title: 'T' },
      slots: { tools: '<input class="suche">', default: '<table class="t"></table>' },
    })
    expect(wrapper.find('.comp-card__head .comp-card__tools .suche').exists()).toBe(true)
    expect(wrapper.find('table.t').exists()).toBe(true)
  })

  it('mit padded steht der Inhalt im Innenabstand-Rahmen', () => {
    const wrapper = mount(ListCard, { props: { title: 'T', padded: true }, slots: { default: '<p class="inhalt">Formular</p>' } })
    expect(wrapper.find('.list-card__body .inhalt').exists()).toBe(true)
  })

  it('mit overflowVisible bleibt der Kartenrand für Flyouts offen', () => {
    const wrapper = mount(ListCard, { props: { title: 'T', overflowVisible: true } })
    expect(wrapper.find('section').classes()).toContain('comp-card--visible')
  })

  it('vergibt je Karte in einer Seite eine eigene ID', () => {
    const Page = { components: { ListCard }, template: '<div><ListCard title="A" /><ListCard title="B" /></div>' }
    const ids = mount(Page).findAll('h3').map(h => h.attributes('id'))
    expect(new Set(ids).size).toBe(2)
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = mount(ListCard, { props: { title: 'A' }, slots: { default: '<p>x</p>' }, attachTo: document.body })
    expect(await axe(wrapper)).toHaveNoViolations()
    wrapper.unmount()
  })

  it('reicht class (z. B. split__main) an die Karte durch', () => {
    const wrapper = mount(ListCard, { props: { title: 'T' }, attrs: { class: 'split__main' } })
    expect(wrapper.find('section').classes()).toContain('split__main')
  })
})
