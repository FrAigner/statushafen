import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AppIcon from './AppIcon.vue'
import { axe } from '../test-support/axe.js'

describe('AppIcon', () => {
  it('rendert ein svg mit Default-Größe 16', () => {
    const wrapper = mount(AppIcon, { props: { name: 'search' } })

    const svg = wrapper.find('svg')
    expect(svg.attributes('width')).toBe('16')
    expect(svg.attributes('height')).toBe('16')
    expect(svg.attributes('aria-hidden')).toBe('true')
  })

  it('übernimmt eine übergebene size', () => {
    const wrapper = mount(AppIcon, { props: { name: 'x', size: 32 } })

    const svg = wrapper.find('svg')
    expect(svg.attributes('width')).toBe('32')
    expect(svg.attributes('height')).toBe('32')
  })

  it('rendert unterschiedliches Markup für unterschiedliche Icon-Namen', () => {
    const sun = mount(AppIcon, { props: { name: 'sun' } })
    const trash = mount(AppIcon, { props: { name: 'trash' } })

    expect(sun.html()).not.toBe(trash.html())
    expect(sun.find('circle').exists()).toBe(true)
    expect(trash.findAll('path').length).toBeGreaterThan(0)
  })

  it('fällt bei unbekanntem Namen auf einen einfachen Punkt zurück', () => {
    const wrapper = mount(AppIcon, { props: { name: 'does-not-exist' } })

    const circle = wrapper.find('circle')
    expect(circle.exists()).toBe(true)
    expect(circle.attributes('r')).toBe('2')
  })

  it('ist für Screenreader ausgeblendet und erzeugt keine axe-Funde', async () => {
    const wrapper = mount(AppIcon, { props: { name: 'x' } })
    expect(wrapper.attributes('aria-hidden')).toBe('true')
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('zeichnet das Zeilenmenü-Symbol „more“ als drei Punkte', () => {
    const wrapper = mount(AppIcon, { props: { name: 'more' } })
    expect(wrapper.findAll('circle')).toHaveLength(3)
  })
})
