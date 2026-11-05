import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import FormPanel from './FormPanel.vue'
import formPanelSource from './FormPanel.vue?raw'
import { axe } from '../test-support/axe.js'

const mountPanel = (props = {}) => mount(FormPanel, {
  props: { title: 'Seite „Acme“ bearbeiten', ...props },
  slots: { default: '<form class="edit-form"><label for="x">Name</label><input id="x"></form>' },
  global: { plugins: [i18n] },
})

describe('FormPanel', () => {
  beforeEach(() => { i18n.global.locale.value = 'de' })

  it('zeigt Titel, Pflichtfeld-Hinweis und Inhalt; die Karte trägt den Titel als Namen', () => {
    const wrapper = mountPanel()
    const aside = wrapper.find('aside.form-panel')
    expect(aside.attributes('aria-label')).toBe('Seite „Acme“ bearbeiten')
    expect(aside.find('h3').text()).toBe('Seite „Acme“ bearbeiten')
    expect(aside.find('.form-panel__hint').text()).toBe('Felder mit * sind Pflichtfelder.')
    expect(aside.find('form.edit-form input').exists()).toBe(true)
  })

  it('nimmt einen eigenen Hinweis', () => {
    expect(mountPanel({ hint: 'Eigener Hinweis' }).find('.form-panel__hint').text()).toBe('Eigener Hinweis')
  })

  it('blendet den Hinweis aus, wenn showRequiredHint false ist', () => {
    expect(mountPanel({ showRequiredHint: false }).find('.form-panel__hint').exists()).toBe(false)
  })

  it('zeigt einen eigenen Hinweis auch bei showRequiredHint false', () => {
    expect(mountPanel({ showRequiredHint: false, hint: 'Nur dieser' }).find('.form-panel__hint').text()).toBe('Nur dieser')
  })

  it('meldet close über den beschrifteten Schließen-Knopf', async () => {
    const wrapper = mountPanel()
    const close = wrapper.find('button.form-panel__close')
    expect(close.attributes('aria-label')).toBe('Formular schließen')
    expect(close.attributes('type')).toBe('button')
    await close.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  // Regression: ".edit-card h3" aus admin.css (später geladen) zog eine Trennlinie
  // und Abstand unter den Titel im Formularkopf. Die Komponente setzt das per
  // scoped Style zurück (höhere Spezifität, unabhängig von der Ladereihenfolge).
  it('setzt Linie und Abstand des Titels im Kopf per scoped Style zurück', () => {
    const style = formPanelSource.slice(formPanelSource.indexOf('<style scoped>'))
    expect(style).toMatch(/\.form-panel__head h3\s*\{[^}]*border:\s*0/)
    expect(style).toMatch(/\.form-panel__head h3\s*\{[^}]*padding:\s*0/)
    expect(style).toMatch(/\.form-panel__head h3\s*\{[^}]*margin:\s*0/)
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = mountPanel()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
