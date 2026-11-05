import { describe, it, expect, afterEach, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'
import RowMenu from './RowMenu.vue'
import { axe } from '../test-support/axe.js'

const ITEMS = [
  { key: 'edit', label: 'Bearbeiten', icon: 'pencil' },
  { key: 'translations', label: 'Übersetzungen', icon: 'globe', to: '/admin/x/translations' },
  { key: 'delete', label: 'Löschen', icon: 'trash', danger: true },
]

let wrapper
function mountMenu(props = {}) {
  wrapper = mount(RowMenu, {
    attachTo: document.body,
    props: { label: 'Aktionen für „API“', items: ITEMS, ...props },
    global: { stubs: { RouterLink: RouterLinkStub } },
  })
  return wrapper
}
const button = () => wrapper.find('.row-menu__button')
const menu = () => wrapper.find('[role="menu"]')
const items = () => wrapper.findAll('[role="menuitem"]')
const press = (target, key) => target.trigger('keydown', { key })
const isOpen = () => button().attributes('aria-expanded') === 'true'

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('RowMenu', () => {
  it('ist ein Menüknopf mit Name, aria-haspopup und aria-expanded, anfangs geschlossen', () => {
    mountMenu()
    expect(button().attributes('aria-haspopup')).toBe('menu')
    expect(button().attributes('aria-label')).toBe('Aktionen für „API“')
    expect(button().attributes('aria-controls')).toBe(menu().attributes('id'))
    expect(isOpen()).toBe(false)
    expect(menu().isVisible()).toBe(false)
  })

  it('öffnet per Klick und fokussiert den ersten Eintrag; zweiter Klick schließt', async () => {
    mountMenu()
    await button().trigger('click')
    await wrapper.vm.$nextTick()
    expect(isOpen()).toBe(true)
    expect(document.activeElement).toBe(items()[0].element)
    await button().trigger('click')
    expect(isOpen()).toBe(false)
  })

  it('öffnet mit Pfeil unten auf dem ersten, mit Pfeil oben auf dem letzten Eintrag', async () => {
    mountMenu()
    await press(button(), 'ArrowDown')
    await wrapper.vm.$nextTick()
    expect(document.activeElement).toBe(items()[0].element)
    await press(menu(), 'Escape')

    await press(button(), 'ArrowUp')
    await wrapper.vm.$nextTick()
    expect(document.activeElement).toBe(items()[2].element)
  })

  it('bewegt mit Pfeilen (mit Umbruch), Home und End', async () => {
    mountMenu()
    await button().trigger('click')
    await wrapper.vm.$nextTick()

    await press(menu(), 'ArrowDown')
    expect(document.activeElement).toBe(items()[1].element)
    await press(menu(), 'ArrowDown')
    await press(menu(), 'ArrowDown')
    expect(document.activeElement).toBe(items()[0].element) // Umbruch nach unten
    await press(menu(), 'ArrowUp')
    expect(document.activeElement).toBe(items()[2].element) // Umbruch nach oben
    await press(menu(), 'Home')
    expect(document.activeElement).toBe(items()[0].element)
    await press(menu(), 'End')
    expect(document.activeElement).toBe(items()[2].element)
  })

  it('Escape schließt und gibt den Fokus an den Knopf zurück', async () => {
    mountMenu()
    await button().trigger('click')
    await wrapper.vm.$nextTick()
    await press(menu(), 'Escape')
    expect(isOpen()).toBe(false)
    expect(document.activeElement).toBe(button().element)
  })

  it('Tab schließt, ohne den Fokus zu verschieben', async () => {
    mountMenu()
    await button().trigger('click')
    await wrapper.vm.$nextTick()
    await press(menu(), 'Tab')
    expect(isOpen()).toBe(false)
    expect(document.activeElement).not.toBe(button().element)
  })

  it('meldet den Schlüssel des gewählten Eintrags, schließt und fokussiert den Knopf', async () => {
    mountMenu()
    await button().trigger('click')
    await items()[2].trigger('click')
    expect(wrapper.emitted('select')).toEqual([['delete']])
    expect(isOpen()).toBe(false)
    expect(document.activeElement).toBe(button().element)
  })

  it('wählt mit Leertaste den fokussierten Eintrag (Enter löst der Button selbst aus)', async () => {
    mountMenu()
    await button().trigger('click')
    await wrapper.vm.$nextTick()
    await press(menu(), ' ')
    expect(wrapper.emitted('select')).toEqual([['edit']])
  })

  it('rendert Einträge mit `to` als Link, der das Menü schließt und nichts meldet', async () => {
    mountMenu()
    await button().trigger('click')
    const link = wrapper.findComponent(RouterLinkStub)
    expect(link.props('to')).toBe('/admin/x/translations')
    expect(link.attributes('role')).toBe('menuitem')
    await link.trigger('click')
    expect(isOpen()).toBe(false)
    expect(wrapper.emitted('select')).toBeUndefined()
  })

  it('ignoriert Klicks auf deaktivierte Einträge', async () => {
    mountMenu({ items: [{ key: 'delete', label: 'Löschen', disabled: true }] })
    await button().trigger('click')
    await items()[0].trigger('click')
    expect(items()[0].attributes('aria-disabled')).toBe('true')
    expect(wrapper.emitted('select')).toBeUndefined()
  })

  it('schließt bei Klick außerhalb, aber nicht bei Klick im Menü', async () => {
    mountMenu()
    await button().trigger('click')
    menu().element.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(isOpen()).toBe(true)
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(isOpen()).toBe(false)
  })

  it('schließt beim Scrollen und bei Größenänderung', async () => {
    mountMenu()
    await button().trigger('click')
    document.dispatchEvent(new Event('scroll'))
    await wrapper.vm.$nextTick()
    expect(isOpen()).toBe(false)

    await button().trigger('click')
    window.dispatchEvent(new Event('resize'))
    await wrapper.vm.$nextTick()
    expect(isOpen()).toBe(false)
  })

  it('ist deaktivierbar', () => {
    mountMenu({ disabled: true })
    expect(button().attributes('disabled')).toBeDefined()
  })

  it('ignoriert andere Tasten am Knopf und im Menü', async () => {
    mountMenu()
    await press(button(), 'a')
    expect(isOpen()).toBe(false)
    await button().trigger('click')
    await wrapper.vm.$nextTick()
    await press(menu(), 'a')
    expect(isOpen()).toBe(true)
    expect(document.activeElement).toBe(items()[0].element)
  })

  it('setzt das Menü fest (fixed) nach unten unter den Knopf und kippt bei Platzmangel nach oben', async () => {
    mountMenu()
    vi.spyOn(button().element, 'getBoundingClientRect').mockReturnValue({ top: 100, bottom: 144, right: 500 })
    await button().trigger('click')
    await wrapper.vm.$nextTick()
    expect(menu().element.style.insetBlockStart).toBe('148px')
    expect(menu().element.style.insetInlineEnd).toBe(`${Math.max(window.innerWidth - 500, 8)}px`)
    await press(menu(), 'Escape')

    vi.spyOn(menu().element, 'offsetHeight', 'get').mockReturnValue(150)
    button().element.getBoundingClientRect.mockReturnValue({ top: window.innerHeight - 60, bottom: window.innerHeight - 16, right: 500 })
    await button().trigger('click')
    await wrapper.vm.$nextTick()
    expect(menu().element.style.insetBlockStart).toBe(`${window.innerHeight - 60 - 150 - 4}px`)
  })

  it('entfernt die Dokument-Listener beim Unmount', async () => {
    mountMenu()
    await button().trigger('click')
    const remove = vi.spyOn(document, 'removeEventListener')
    wrapper.unmount()
    wrapper = null
    expect(remove).toHaveBeenCalledWith('pointerdown', expect.any(Function), true)
    remove.mockRestore()
  })

  it('hat keine axe-Funde (offen und geschlossen)', async () => {
    mountMenu()
    expect(await axe(wrapper)).toHaveNoViolations()
    await button().trigger('click')
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
