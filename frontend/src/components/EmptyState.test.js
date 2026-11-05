import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import EmptyState from './EmptyState.vue'
import { axe } from '../test-support/axe.js'

describe('EmptyState', () => {
  it('zeigt nur den Titel, wenn sonst nichts übergeben wird', () => {
    const w = mount(EmptyState, { props: { title: 'Keine Komponenten' } })
    expect(w.find('.empty-state__title').text()).toBe('Keine Komponenten')
    expect(w.find('.empty-state__icon').exists()).toBe(false)
    expect(w.find('.empty-state__text').exists()).toBe(false)
    expect(w.find('.empty-state__action').exists()).toBe(false)
  })

  it('zeigt Symbol, Satz und Aktion, wenn vorhanden', () => {
    const w = mount(EmptyState, {
      props: { title: 'Keine Komponenten', text: 'Lege die erste an.', icon: 'folder' },
      slots: { action: '<button type="button">Anlegen</button>' },
    })
    expect(w.find('.empty-state__icon').exists()).toBe(true)
    expect(w.find('.empty-state__text').text()).toBe('Lege die erste an.')
    expect(w.find('.empty-state__action button').text()).toBe('Anlegen')
  })

  it('ändert die Überschriftenhierarchie nicht (Titel ist ein Absatz)', () => {
    const w = mount(EmptyState, { props: { title: 'Leer' } })
    expect(w.find('h1,h2,h3,h4,h5,h6').exists()).toBe(false)
  })

  it('hat keine axe-Funde', async () => {
    const w = mount(EmptyState, {
      props: { title: 'Leer', text: 'Nichts da.', icon: 'folder' },
      slots: { action: '<button type="button">Anlegen</button>' },
    })
    expect(await axe(w)).toHaveNoViolations()
  })
})
