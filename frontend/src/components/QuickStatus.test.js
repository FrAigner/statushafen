import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import i18n from '../i18n'
import QuickStatus from './QuickStatus.vue'
import { axe } from '../test-support/axe.js'

let wrapper = null

function mountQuick(props = {}) {
  wrapper = mount(QuickStatus, {
    props: { modelValue: 'operational', ...props },
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
  return wrapper
}

const button = () => wrapper.find('button')
const list = () => wrapper.find('[role="listbox"]')
const options = () => wrapper.findAll('[role="option"]')
const isOpen = () => button().attributes('aria-expanded') === 'true'
async function press(key, target = button()) {
  await target.trigger('keydown', { key })
}

beforeEach(() => {
  i18n.global.locale.value = 'de'
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('QuickStatus: Darstellung', () => {
  it('zeigt Symbol und Text des aktuellen Status auf dem Button', () => {
    mountQuick({ modelValue: 'degraded_performance' })
    expect(button().text()).toBe('Beeinträchtigt')
    expect(button().find('svg').attributes('data-shape')).toBe('triangle-exclamation')
  })

  it('bietet genau vier Stufen an, Wartung ist kein Komponentenstatus', () => {
    mountQuick()
    expect(options().map(o => o.text())).toEqual(['Betriebsbereit', 'Beeinträchtigt', 'Teilausfall', 'Großstörung'])
    expect(options().map(o => o.find('svg').attributes('data-shape'))).toEqual([
      'circle-check', 'triangle-exclamation', 'diamond-minus', 'octagon-cross',
    ])
  })

  it('markiert die aktuelle Option als ausgewählt', () => {
    mountQuick({ modelValue: 'partial_outage' })
    expect(options().map(o => o.attributes('aria-selected'))).toEqual(['false', 'false', 'true', 'false'])
  })

  it('zeigt einen unbekannten Wert neutral und wählt nichts aus', () => {
    mountQuick({ modelValue: 'kaputt' })
    expect(button().text()).toBe('kaputt')
    expect(button().find('svg').attributes('data-shape')).toBe('circle')
    expect(options().every(o => o.attributes('aria-selected') === 'false')).toBe(true)
  })

  it('zeigt ohne Wert einen leeren Text', () => {
    mountQuick({ modelValue: null })
    expect(button().text()).toBe('')
  })

  it('benennt Button und Liste: Vorgabe und eigenes label', () => {
    mountQuick()
    expect(button().attributes('aria-label')).toBe('Status ändern')
    expect(list().attributes('aria-label')).toBe('Status ändern')
    wrapper.unmount()
    mountQuick({ label: 'Status von API' })
    expect(button().attributes('aria-label')).toBe('Status von API')
    expect(list().attributes('aria-label')).toBe('Status von API')
  })

  it('hat Combobox-Attribute, aria-controls zeigt auf die Liste', () => {
    mountQuick()
    expect(button().attributes('role')).toBe('combobox')
    expect(button().attributes('aria-haspopup')).toBe('listbox')
    expect(button().attributes('aria-controls')).toBe(list().attributes('id'))
  })
})

describe('QuickStatus: Öffnen und Schließen', () => {
  it('ist zunächst geschlossen, ohne activedescendant', () => {
    mountQuick()
    expect(isOpen()).toBe(false)
    expect(list().isVisible()).toBe(false)
    expect(button().attributes('aria-activedescendant')).toBeUndefined()
  })

  it('Klick öffnet, zweiter Klick schließt', async () => {
    mountQuick()
    await button().trigger('click')
    expect(isOpen()).toBe(true)
    expect(list().isVisible()).toBe(true)
    await button().trigger('click')
    expect(isOpen()).toBe(false)
  })

  it.each(['ArrowDown', 'ArrowUp', 'Enter', ' '])('Taste %j öffnet und aktiviert den aktuellen Wert', async (key) => {
    mountQuick({ modelValue: 'partial_outage' })
    await press(key)
    expect(isOpen()).toBe(true)
    expect(button().attributes('aria-activedescendant')).toBe(options()[2].attributes('id'))
  })

  it('andere Tasten öffnen nicht', async () => {
    mountQuick()
    await press('a')
    await press('Escape')
    expect(isOpen()).toBe(false)
  })

  it('öffnet bei unbekanntem Wert auf der ersten Option', async () => {
    mountQuick({ modelValue: 'kaputt' })
    await press('ArrowDown')
    expect(button().attributes('aria-activedescendant')).toBe(options()[0].attributes('id'))
  })

  it('Escape schließt ohne Änderung und lässt den Fokus am Button', async () => {
    mountQuick()
    button().element.focus()
    await press('ArrowDown')
    await press('ArrowDown')
    await press('Escape')
    expect(isOpen()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(document.activeElement).toBe(button().element)
  })

  it('Tab schließt die Liste', async () => {
    mountQuick()
    await press('Enter')
    await press('Tab')
    expect(isOpen()).toBe(false)
  })

  it('Klick außerhalb schließt, Klick innerhalb nicht; nach dem Schließen ist der Listener weg', async () => {
    mountQuick()
    await button().trigger('click')
    await nextTick()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await nextTick()
    expect(isOpen()).toBe(false)

    await button().trigger('click')
    await nextTick()
    options()[0].element.dispatchEvent(new Event('pointerdown', { bubbles: true, cancelable: true }))
    await nextTick()
    expect(isOpen()).toBe(true)
  })

  it('entfernt den Dokument-Listener beim Abbau', async () => {
    mountQuick()
    await button().trigger('click')
    await nextTick()
    wrapper.unmount()
    wrapper = null
    // Darf nach dem Abbau nichts mehr auslösen (keine Fehler).
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
  })
})

describe('QuickStatus: Auswahl per Tastatur', () => {
  it('Pfeile bewegen die aktive Option, Enter übernimmt und schließt', async () => {
    mountQuick({ modelValue: 'operational' })
    button().element.focus()
    await press('ArrowDown')
    await press('ArrowDown')
    expect(button().attributes('aria-activedescendant')).toBe(options()[1].attributes('id'))
    await press('ArrowDown')
    await press('Enter')
    expect(wrapper.emitted('update:modelValue')).toEqual([['partial_outage']])
    expect(isOpen()).toBe(false)
    expect(document.activeElement).toBe(button().element)
  })

  it('Leertaste übernimmt ebenfalls', async () => {
    mountQuick()
    await press('Enter')
    await press('End')
    await press(' ')
    expect(wrapper.emitted('update:modelValue')).toEqual([['major_outage']])
  })

  it('Home springt an den Anfang, Pfeil hoch am Anfang bleibt dort', async () => {
    mountQuick({ modelValue: 'major_outage' })
    await press('Enter')
    await press('Home')
    await press('ArrowUp')
    expect(button().attributes('aria-activedescendant')).toBe(options()[0].attributes('id'))
  })

  it('bricht am Ende nicht um', async () => {
    mountQuick({ modelValue: 'major_outage' })
    await press('Enter')
    await press('ArrowDown')
    expect(button().attributes('aria-activedescendant')).toBe(options()[3].attributes('id'))
  })

  it('sendet nichts, wenn der aktuelle Wert gewählt wird, schließt aber', async () => {
    mountQuick({ modelValue: 'degraded_performance' })
    await press('Enter')
    await press('Enter')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(isOpen()).toBe(false)
  })

  it('akzeptiert auch Kurzformen als Wert (degraded = degraded_performance)', async () => {
    mountQuick({ modelValue: 'degraded' })
    await press('Enter')
    await press('Enter')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(button().text()).toBe('Beeinträchtigt')
  })

  it('nicht navigierende Tasten in der offenen Liste lassen alles wie es ist', async () => {
    mountQuick()
    await press('Enter')
    await press('x')
    expect(isOpen()).toBe(true)
    expect(button().attributes('aria-activedescendant')).toBe(options()[0].attributes('id'))
  })
})

describe('QuickStatus: Auswahl mit der Maus', () => {
  it('Klick auf eine Option übernimmt sie und schließt', async () => {
    mountQuick()
    await button().trigger('click')
    await options()[3].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['major_outage']])
    expect(isOpen()).toBe(false)
  })

  it('Zeigerbewegung setzt die aktive Option', async () => {
    mountQuick()
    await button().trigger('click')
    await options()[2].trigger('pointermove')
    expect(button().attributes('aria-activedescendant')).toBe(options()[2].attributes('id'))
    expect(options()[2].classes()).toContain('quick-status__option--active')
  })
})

describe('QuickStatus: deaktiviert', () => {
  it('lässt sich weder per Klick noch per Taste öffnen', async () => {
    mountQuick({ disabled: true })
    expect(button().attributes('disabled')).toBeDefined()
    await button().trigger('click')
    await press('ArrowDown')
    expect(isOpen()).toBe(false)
  })

  it('ignoriert Klick und Taste auch, wenn der Button nicht nativ gesperrt wäre', async () => {
    mountQuick({ disabled: true })
    button().element.removeAttribute('disabled')
    await button().trigger('click')
    await press('ArrowDown')
    expect(isOpen()).toBe(false)
  })
})

describe('QuickStatus: Barrierefreiheit', () => {
  it('hat keine axe-Funde (zu und offen)', async () => {
    mountQuick()
    expect(await axe(wrapper)).toHaveNoViolations()
    await press('ArrowDown')
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
