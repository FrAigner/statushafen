import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import i18n from '../i18n'
import { vHelp } from './vHelp'
import { HELP_HIDE_DELAY_MS, HELP_SHOW_DELAY_MS, activeHelp, helpRegistry, tooltipPosition } from './helpTooltipState'
import { useAuth } from '../composables/useAuth'
import { axe } from '../test-support/axe.js'

// Hilfe per v-help direkt am Feld/Button, erst nach Verweilen,
// abschaltbar pro Benutzer.

let wrapper = null
const helpKey = ref('components.name')

const Probe = {
  directives: { help: vHelp },
  setup: () => ({ helpKey, show: ref(true) }),
  template: `
    <div>
      <div v-if="show" class="field" v-help="helpKey"><label for="f">Name</label><input id="f"></div>
      <button class="save" v-help="'components.save'">Speichern</button>
      <button class="row" v-help="'components.rowActions'">A</button>
      <button class="row2" v-help="'components.rowActions'">B</button>
      <div class="group" role="group" aria-label="Tags" v-help="'components.tags'"><input type="checkbox" aria-label="EU"></div>
      <button class="plain">Ohne</button>
    </div>`,
}

function mountProbe() {
  wrapper = mount(Probe, { attachTo: document.body, global: { plugins: [i18n] } })
  return wrapper
}

const el = (sel) => wrapper.find(sel).element
const input = () => el('.field input')
const tooltip = (key = 'components.name') => document.getElementById(`help-${key.replace(/\./g, '-')}`)
const visible = (key) => tooltip(key) && tooltip(key).style.display !== 'none'

function pointer(target, type, pointerType = 'mouse') {
  const event = new Event(type, { bubbles: type !== 'pointerenter' && type !== 'pointerleave' })
  event.pointerType = pointerType
  target.dispatchEvent(event)
}

function key(target, k) {
  target.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true }))
}

async function wait(ms) {
  vi.advanceTimersByTime(ms)
  await nextTick()
  await nextTick()
}

describe('v-help', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    helpKey.value = 'components.name'
    useAuth().setAuth('jwt', { email: 'a@b.de' })
    vi.useFakeTimers()
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.useRealTimers()
    vi.restoreAllMocks()
    useAuth().logout()
  })

  it('setzt aria-describedby am Feld, am Button und an der Gruppe auf vorhandene Tooltips', async () => {
    mountProbe()
    await nextTick()

    expect(input().getAttribute('aria-describedby')).toBe('help-components-name')
    expect(el('.field').hasAttribute('aria-describedby')).toBe(false)
    expect(el('.save').getAttribute('aria-describedby')).toBe('help-components-save')
    expect(el('.group').getAttribute('aria-describedby')).toBe('help-components-tags')
    expect(el('.plain').hasAttribute('aria-describedby')).toBe(false)
    const tip = tooltip()
    expect(tip.getAttribute('role')).toBe('tooltip')
    expect(visible()).toBe(false)
    expect(tip.textContent).toContain('Der Anzeigename des Dienstes')
    expect([...tip.querySelectorAll('.help-tooltip__heading')].map(h => h.textContent)).toEqual(['Was: ', 'Auswirkung: ', 'Beispiel: '])
    // Kein ⓘ-Symbol: v-help fügt keine eigenen Bedienelemente hinzu.
    expect(wrapper.findAll('button')).toHaveLength(4)
  })

  it('zeigt die Hilfe erst nach der Verzögerung, Verlassen vorher bricht ab', async () => {
    mountProbe()
    pointer(el('.field'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS - 1)
    expect(visible()).toBe(false)
    pointer(el('.field'), 'pointerleave')
    await wait(HELP_SHOW_DELAY_MS)
    expect(visible()).toBe(false)

    pointer(el('.field'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    expect(visible()).toBe(true)
    expect(activeHelp.value.anchor).toBe(el('.field'))
  })

  it('bleibt offen, wenn der Zeiger in den Tooltip wandert, und schließt danach', async () => {
    mountProbe()
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    const key = 'components.save'
    expect(visible(key)).toBe(true)

    pointer(el('.save'), 'pointerleave')
    await wait(HELP_HIDE_DELAY_MS - 50)
    pointer(tooltip(key), 'pointerenter')
    await wait(1000)
    expect(visible(key)).toBe(true)

    pointer(tooltip(key), 'pointerleave')
    await wait(HELP_HIDE_DELAY_MS)
    expect(visible(key)).toBe(false)
  })

  it('Zurückkehren auf das Element hält den Tooltip offen', async () => {
    mountProbe()
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    pointer(el('.save'), 'pointerleave')
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_HIDE_DELAY_MS + 10)
    expect(visible('components.save')).toBe(true)
  })

  it('ignoriert Touch: kein Öffnen per Antippen', async () => {
    mountProbe()
    pointer(el('.save'), 'pointerenter', 'touch')
    await wait(HELP_SHOW_DELAY_MS * 2)
    expect(visible('components.save')).toBe(false)

    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    pointer(el('.save'), 'pointerleave', 'touch')
    pointer(tooltip('components.save'), 'pointerleave', 'touch')
    pointer(tooltip('components.save'), 'pointerenter', 'touch')
    await wait(HELP_HIDE_DELAY_MS)
    expect(visible('components.save')).toBe(true)
  })

  it('Klick schließt und verhindert das Öffnen, bis der Zeiger das Element verlassen hat', async () => {
    mountProbe()
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    pointer(el('.save'), 'pointerdown')
    await nextTick()
    expect(visible('components.save')).toBe(false)

    // Noch über dem Button: erneutes Hover-Ereignis öffnet nicht.
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    expect(visible('components.save')).toBe(false)

    pointer(el('.save'), 'pointerleave')
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    expect(visible('components.save')).toBe(true)
  })

  it('Klick vor Ablauf der Verzögerung verhindert das Öffnen', async () => {
    mountProbe()
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS / 2)
    pointer(el('.save'), 'pointerdown')
    await wait(HELP_SHOW_DELAY_MS)
    expect(visible('components.save')).toBe(false)
  })

  it('Tippen ins Feld schließt bzw. verhindert das Öffnen', async () => {
    mountProbe()
    pointer(el('.field'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    expect(visible()).toBe(true)
    key(input(), 'a')
    await nextTick()
    expect(visible()).toBe(false)

    pointer(el('.field'), 'pointerleave')
    pointer(el('.field'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS / 2)
    input().dispatchEvent(new Event('input', { bubbles: true }))
    await wait(HELP_SHOW_DELAY_MS)
    expect(visible()).toBe(false)
  })

  it('Tastatur: Fokus per Tab öffnet nach der Verzögerung, ohne den Fokus zu verschieben; Blur schließt', async () => {
    mountProbe()
    key(document.body, 'Tab')
    input().focus()
    await wait(HELP_SHOW_DELAY_MS - 1)
    expect(visible()).toBe(false)
    await wait(1)
    expect(visible()).toBe(true)
    expect(document.activeElement).toBe(input())

    // Fokus wandert innerhalb des Elements: bleibt offen.
    el('.field label').dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget: input() }))
    await nextTick()
    expect(visible()).toBe(true)

    input().blur()
    await nextTick()
    expect(visible()).toBe(false)
  })

  it('Fokus per Maus öffnet nicht', async () => {
    mountProbe()
    pointer(document.body, 'pointerdown')
    input().focus()
    await wait(HELP_SHOW_DELAY_MS)
    expect(visible()).toBe(false)
  })

  it('Escape schließt, ebenso ein Klick außerhalb; Klick in den Tooltip nicht', async () => {
    mountProbe()
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    pointer(tooltip('components.save'), 'pointerdown')
    await nextTick()
    expect(visible('components.save')).toBe(true)

    key(document.body, 'Escape')
    await nextTick()
    expect(visible('components.save')).toBe(false)

    pointer(el('.save'), 'pointerleave')
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    pointer(document.body, 'pointerdown')
    await nextTick()
    expect(visible('components.save')).toBe(false)
  })

  it('Escape im Feld schließt', async () => {
    mountProbe()
    pointer(el('.field'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    key(input(), 'Escape')
    await nextTick()
    expect(visible()).toBe(false)
  })

  it('mehrere Elemente teilen sich einen Tooltip pro Schlüssel', async () => {
    mountProbe()
    await nextTick()
    expect(helpRegistry.get('components.rowActions')).toBe(2)
    expect(document.querySelectorAll('#help-components-rowActions')).toHaveLength(1)
    pointer(el('.row2'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    expect(activeHelp.value.anchor).toBe(el('.row2'))
  })

  it('ausgeschaltet: keine Tooltips, kein aria-describedby, keine Listener; Einschalten stellt alles her', async () => {
    useAuth().setAuth('jwt', { email: 'a@b.de', help_tooltips: false })
    const add = vi.spyOn(document, 'addEventListener')
    mountProbe()
    await nextTick()

    expect(input().hasAttribute('aria-describedby')).toBe(false)
    expect(el('.group').hasAttribute('aria-describedby')).toBe(false)
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    expect(document.querySelector('.help-tooltip-layer')).toBeNull()
    expect(add).not.toHaveBeenCalledWith('keydown', expect.any(Function), true)
    pointer(el('.field'), 'pointerenter')
    key(document.body, 'Tab')
    input().focus()
    await wait(HELP_SHOW_DELAY_MS * 2)
    expect(activeHelp.value).toBeNull()

    useAuth().currentUser.value = { ...useAuth().currentUser.value, help_tooltips: true }
    await nextTick()
    await nextTick()
    expect(input().getAttribute('aria-describedby')).toBe('help-components-name')
    expect(tooltip()).not.toBeNull()

    // Ausschalten bei offenem Tooltip schließt ihn und räumt auf.
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    const remove = vi.spyOn(document, 'removeEventListener')
    useAuth().currentUser.value = { ...useAuth().currentUser.value, help_tooltips: false }
    await nextTick()
    await nextTick()
    expect(activeHelp.value).toBeNull()
    expect(el('.save').hasAttribute('aria-describedby')).toBe(false)
    expect(document.querySelector('.help-tooltip-layer')).toBeNull()
    expect(remove).toHaveBeenCalledWith('keydown', expect.any(Function), true)
    expect(remove).toHaveBeenCalledWith('pointerdown', expect.any(Function), true)
  })

  it('behält fremde aria-describedby-Werte bei', async () => {
    const Comp = {
      directives: { help: vHelp },
      template: '<button aria-describedby="fehler-1" v-help="\'components.save\'">S</button>',
    }
    wrapper = mount(Comp, { attachTo: document.body, global: { plugins: [i18n] } })
    expect(wrapper.element.getAttribute('aria-describedby')).toBe('fehler-1 help-components-save')
    useAuth().currentUser.value = { ...useAuth().currentUser.value, help_tooltips: false }
    await nextTick()
    expect(wrapper.element.getAttribute('aria-describedby')).toBe('fehler-1')
  })

  it('ohne Hilfetext: keine Listener, kein aria-describedby, Warnung', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    helpKey.value = 'components.gibtEsNicht'
    mountProbe()
    await nextTick()
    expect(input().hasAttribute('aria-describedby')).toBe(false)
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('help.components.gibtEsNicht'))
    pointer(el('.field'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    expect(activeHelp.value).toBeNull()
  })

  it('Schlüsselwechsel tauscht Tooltip und aria-describedby; Aushängen räumt auf', async () => {
    mountProbe()
    await nextTick()
    helpKey.value = 'components.description'
    await nextTick()
    await nextTick()
    expect(input().getAttribute('aria-describedby')).toBe('help-components-description')
    expect(helpRegistry.has('components.name')).toBe(false)
    expect(tooltip('components.description')).not.toBeNull()

    pointer(el('.field'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    wrapper.vm.show = false
    await nextTick()
    expect(activeHelp.value).toBeNull()
    expect(helpRegistry.has('components.description')).toBe(false)

    wrapper.unmount()
    wrapper = null
    expect(helpRegistry.size).toBe(0)
    expect(document.querySelector('.help-tooltip-layer')).toBeNull()
  })

  it('Schlüsselwechsel bei ausgeschalteter Hilfe hängt nichts an', async () => {
    useAuth().setAuth('jwt', { email: 'a@b.de', help_tooltips: false })
    mountProbe()
    helpKey.value = 'components.description'
    await nextTick()
    await nextTick()
    expect(input().hasAttribute('aria-describedby')).toBe(false)
    expect(helpRegistry.size).toBe(0)
  })

  it('Element ohne Feld/Button darin wird selbst beschrieben', async () => {
    const Comp = { directives: { help: vHelp }, template: '<div><span tabindex="0" v-help="\'components.name\'">Name</span></div>' }
    wrapper = mount(Comp, { attachTo: document.body, global: { plugins: [i18n] } })
    expect(wrapper.find('span').attributes('aria-describedby')).toBe('help-components-name')
  })

  it('Positionieren ohne passenden Tooltip bzw. nach dem Schließen tut nichts', async () => {
    mountProbe()
    await nextTick()
    activeHelp.value = { key: 'components.save', anchor: el('.save') }
    await nextTick()
    activeHelp.value = { key: 'nicht.registriert', anchor: el('.save') }
    expect(() => window.dispatchEvent(new Event('resize'))).not.toThrow()
    activeHelp.value = null
    expect(() => window.dispatchEvent(new Event('scroll'))).not.toThrow()
    await nextTick()
  })

  it('Neu-Rendern einer Komponente setzt aria-describedby am neuen Feld', async () => {
    const swap = ref(false)
    const Comp = {
      directives: { help: vHelp },
      setup: () => ({ swap }),
      template: '<div><div v-help="\'components.name\'"><input v-if="swap" class="b"><input v-else class="a"></div></div>',
    }
    wrapper = mount(Comp, { attachTo: document.body, global: { plugins: [i18n] } })
    swap.value = true
    await nextTick()
    expect(wrapper.find('.b').attributes('aria-describedby')).toBe('help-components-name')
  })

  it('baut die Ebene neu auf, wenn sie aus dem DOM entfernt wurde', async () => {
    mountProbe()
    await nextTick()
    document.querySelector('.help-tooltip-layer').remove()
    const Extra = { directives: { help: vHelp }, template: '<button v-help="\'components.cancel\'">x</button>' }
    const extra = mount(Extra, { attachTo: document.body, global: { plugins: [i18n] } })
    await nextTick()
    expect(document.querySelectorAll('.help-tooltip-layer')).toHaveLength(1)
    expect(tooltip('components.cancel')).not.toBeNull()
    extra.unmount()
  })

  it('positioniert am Element und folgt Scrollen/Größenänderung', async () => {
    mountProbe()
    el('.save').getBoundingClientRect = () => ({ left: 400, top: 100, width: 80, height: 30, right: 480, bottom: 130 })
    pointer(el('.save'), 'pointerenter')
    await wait(HELP_SHOW_DELAY_MS)
    const tip = tooltip('components.save')
    Object.defineProperty(tip, 'offsetWidth', { configurable: true, value: 200 })
    Object.defineProperty(tip, 'offsetHeight', { configurable: true, value: 100 })
    Object.defineProperty(document.documentElement, 'clientWidth', { configurable: true, value: 1000 })
    window.innerHeight = 800
    window.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(tip.style.top).toBe('136px')
    expect(tip.style.left).toBe('340px')

    el('.save').getBoundingClientRect = () => ({ left: 0, top: 50, width: 80, height: 30, right: 80, bottom: 80 })
    window.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(tip.style.left).toBe('8px')
  })

  it('hat keine axe-Funde, geschlossen und offen', async () => {
    vi.useRealTimers()
    mountProbe()
    await nextTick()
    expect(await axe(document.body)).toEqual([])
    activeHelp.value = { key: 'components.name', anchor: el('.field') }
    await nextTick()
    expect(await axe(document.body)).toEqual([])
  })
})

describe('tooltipPosition', () => {
  const viewport = { width: 1000, height: 800 }
  const size = { width: 200, height: 100 }

  it('unter dem Element, mittig, in den Viewport geklemmt', () => {
    expect(tooltipPosition({ left: 400, top: 100, width: 24, bottom: 124 }, size, viewport)).toEqual({ top: '130px', left: '312px' })
    expect(tooltipPosition({ left: 980, top: 100, width: 24, bottom: 124 }, size, viewport).left).toBe('792px')
    expect(tooltipPosition({ left: 0, top: 100, width: 24, bottom: 124 }, size, viewport).left).toBe('8px')
  })

  it('darüber, wenn unten kein Platz ist; passt beides nicht: unten', () => {
    expect(tooltipPosition({ left: 400, top: 700, width: 24, bottom: 724 }, size, viewport).top).toBe('594px')
    expect(tooltipPosition({ left: 400, top: 50, width: 24, bottom: 74 }, { width: 200, height: 300 }, { width: 1000, height: 200 }).top).toBe('80px')
    // Genau passend unten (Grenzfall <=).
    expect(tooltipPosition({ left: 400, top: 600, width: 24, bottom: 686 }, size, viewport).top).toBe('692px')
  })
})
