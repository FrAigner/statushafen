// jsdom statt happy-dom: Die Bereinigung (DOMPurify) braucht ein vollständiges DOM.
// @vitest-environment jsdom
import { describe, it, expect, afterEach } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import RichText from './RichText.vue'
import { axe } from '../test-support/axe.js'

enableAutoUnmount(afterEach)

describe('RichText', () => {
  it('zeigt Markdown formatiert und reagiert auf Änderungen', async () => {
    const w = mount(RichText, { props: { text: '**fett**\n- a\n- b' } })
    expect(w.find('strong').text()).toBe('fett')
    expect(w.findAll('li')).toHaveLength(2)
    await w.setProps({ text: '*kursiv*' })
    expect(w.find('em').text()).toBe('kursiv')
    expect(w.find('strong').exists()).toBe(false)
  })

  it('gibt Klassen des Aufrufers an das Wurzelelement weiter', () => {
    const w = mount(RichText, { props: { text: 'x' }, attrs: { class: 'sp-incident-message' } })
    expect(w.find('.rich-text').classes()).toContain('sp-incident-message')
  })

  it('führt kein Markup aus', () => {
    const w = mount(RichText, { props: { text: '<img src=x onerror=alert(1)><script>alert(2)</script>[x](javascript:alert(3))' } })
    expect(w.element.querySelectorAll('img, script, a, [onerror]')).toHaveLength(0)
    expect(w.text()).toContain('<script>alert(2)</script>')
  })

  it('plain zeigt den Text unverändert mit Zeilenumbrüchen (Fremdtext ist kein Markdown)', () => {
    const w = mount(RichText, { props: { text: '**roh**\nzweite Zeile <b>x</b>', plain: true } })
    expect(w.find('.rich-text').classes()).toContain('rich-text--plain')
    expect(w.text()).toBe('**roh**\nzweite Zeile <b>x</b>')
    expect(w.find('strong').exists()).toBe(false)
    expect(w.find('b').exists()).toBe(false)
  })

  it('leerer Text ergibt ein leeres Element', () => {
    expect(mount(RichText).text()).toBe('')
    expect(mount(RichText, { props: { text: '' } }).html()).toContain('rich-text')
  })

  it('hat keine Verstöße gegen die Barrierefreiheit', async () => {
    const w = mount(RichText, { props: { text: 'Text mit [Link](https://example.com) und\n\n- Liste' }, attachTo: document.body })
    expect(await axe(w)).toHaveNoViolations()
  })
})
