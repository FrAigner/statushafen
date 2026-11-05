import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { axe, formatViolations } from './axe.js'

// Der Helper selbst muss beide Richtungen zuverlässig melden,
// sonst wären grüne axe-Tests in den Views wertlos.
describe('axe-Helper', () => {
  afterEach(() => { document.body.innerHTML = '' })

  function render(html) {
    const el = document.createElement('div')
    el.innerHTML = html
    document.body.appendChild(el)
    return el
  }

  it('meldet einen Icon-Button ohne Namen (button-name)', async () => {
    const violations = await axe(render('<button><svg aria-hidden="true"></svg></button>'))
    expect(violations.map(v => v.id)).toEqual(['button-name'])
  })

  it('meldet ein Eingabefeld ohne Label (label)', async () => {
    const violations = await axe(render('<input type="text">'))
    expect(violations.map(v => v.id)).toContain('label')
  })

  it('akzeptiert beschriftete Elemente', async () => {
    const el = render('<button aria-label="Schließen">×</button><label for="x">Name</label><input id="x">')
    expect(await axe(el)).toHaveNoViolations()
  })

  it('prüft keine Seitenregeln wie region oder Kontrast', async () => {
    const el = render('<p style="color:#eee;background:#fff">Text ohne Landmark</p>')
    expect(await axe(el)).toEqual([])
  })

  it('hängt einen losgelösten Baum nur während der Prüfung ins Dokument', async () => {
    const el = document.createElement('div')
    el.innerHTML = '<label for="y">Name</label><input id="y">'
    expect(await axe(el.firstChild)).toHaveNoViolations()
    expect(el.isConnected).toBe(false)
  })

  it('prüft beim Wrapper den Mount-Container inklusive aller Wurzelknoten', async () => {
    const Fragment = { template: '<p>Erster Knoten</p><button></button>' }
    const wrapper = mount(Fragment)
    const violations = await axe(wrapper)
    expect(violations.map(v => v.id)).toEqual(['button-name'])
    expect(wrapper.element.isConnected).toBe(false)
    wrapper.unmount()
  })

  it('nutzt das Element selbst, wenn ein Wrapper-Element keinen Elternknoten hat', async () => {
    const button = document.createElement('button')
    const violations = await axe({ element: button })
    expect(violations.map(v => v.id)).toEqual(['button-name'])
  })

  it('formatiert Funde mit Regel und betroffenem Markup', async () => {
    const violations = await axe(render('<button></button>'))
    const text = formatViolations(violations)
    expect(text).toContain('button-name:')
    expect(text).toContain('<button></button>')
    expect(text).not.toContain('weitere')
  })

  it('kürzt die Knotenliste je Regel nach drei Einträgen', async () => {
    const violations = await axe(render('<button id="b1"></button><button id="b2"></button><button id="b3"></button><button id="b4"></button><button id="b5"></button>'))
    const lines = formatViolations(violations).split('\n')
    expect(lines).toEqual([
      expect.stringMatching(/^button-name: /),
      '  <button id="b1"></button>',
      '  <button id="b2"></button>',
      '  <button id="b3"></button>',
      '  … und 2 weitere',
    ])
  })

  it('kürzt bei genau drei Knoten nicht', async () => {
    const violations = await axe(render('<button></button><button></button><button></button>'))
    expect(formatViolations(violations)).not.toContain('weitere')
  })

  it('toHaveNoViolations schlägt mit lesbarer Meldung fehl', async () => {
    const violations = await axe(render('<button></button>'))
    expect(() => expect(violations).toHaveNoViolations()).toThrow(/axe-Funde:\nbutton-name/)
    expect(() => expect([]).not.toHaveNoViolations()).toThrow('axe-Funde erwartet, aber keine gefunden')
  })
})
