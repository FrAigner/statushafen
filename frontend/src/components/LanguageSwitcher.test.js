import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'
import { usePublicPageLanguages } from '../composables/usePublicPageLanguages'
import LanguageSwitcher from './LanguageSwitcher.vue'
import { axe } from '../test-support/axe.js'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// The switcher is a select-only combobox: a button (focus stays there) and a
// listbox panel below it, instead of the native <select> whose OS popup opened
// detached from the header (macOS: above the field).
let wrapper
const mountSwitcher = (options = {}) => {
  wrapper = mount(LanguageSwitcher, { attachTo: document.body, global: { plugins: [i18n] }, ...options })
  return wrapper
}
const button = () => wrapper.find('button.language-switcher')
const list = () => wrapper.find('[role="listbox"]')
const options = () => wrapper.findAll('[role="option"]')
const isOpen = () => button().attributes('aria-expanded') === 'true'
const key = (k) => button().trigger('keydown', { key: k })
const choose = async (code) => {
  await button().trigger('click')
  await wrapper.find(`[role="option"][data-code="${code}"]`).trigger('click')
  await flushPromises()
}

describe('LanguageSwitcher', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    useAuth().logout()
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    usePublicPageLanguages().clearPageLanguages()
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
  })

  it('ist ein Button mit Listbox statt eines nativen select', () => {
    mountSwitcher()
    expect(wrapper.find('select').exists()).toBe(false)
    expect(button().attributes('role')).toBe('combobox')
    expect(button().attributes('aria-haspopup')).toBe('listbox')
    expect(button().attributes('aria-controls')).toBe(list().attributes('id'))
    expect(button().attributes('aria-expanded')).toBe('false')
    expect(button().text()).toContain('Deutsch')
    expect(list().isVisible()).toBe(false)
  })

  // auf einer Statusseite alle Sprachen der Seite, nicht nur de/en.
  it('bietet auf einer Statusseite alle Sprachen der Seite an und wechselt auf eine ohne Oberflächentexte', async () => {
    usePublicPageLanguages().setPageLanguages([
      { code: 'cs', name: 'Čeština', is_default: false },
      { code: 'de', name: 'Deutsch', is_default: true },
      { code: 'en', name: 'English', is_default: false },
      { code: 'hr', name: 'Hrvatski', is_default: false },
    ])
    mountSwitcher()

    expect(options().map((o) => [o.attributes('data-code'), o.text()])).toEqual([
      ['cs', 'Čeština'], ['de', 'Deutsch'], ['en', 'English'], ['hr', 'Hrvatski'],
    ])
    await choose('hr')
    expect(i18n.global.locale.value).toBe('hr')
    expect(button().text()).toContain('Hrvatski')
  })

  // Translation switched off (or a single page language): there is no content
  // language to choose, the switcher only changes the UI language; the content
  // stays in the page default.
  it('offers the UI languages when the page has only its default language', async () => {
    usePublicPageLanguages().setPageLanguages([{ code: 'de', name: 'Deutsch', is_default: true }])
    mountSwitcher()

    expect(options().map((o) => o.attributes('data-code'))).toEqual(['de', 'en'])
    await choose('en')
    expect(i18n.global.locale.value).toBe('en')
    expect(button().text()).toContain('English')
  })

  // A page whose only language has no UI texts (e.g. cs): the options are the
  // UI languages, the button shows the page language code instead of nothing.
  it('zeigt den Sprachcode, wenn die angezeigte Sprache nicht angeboten wird', () => {
    usePublicPageLanguages().setPageLanguages([{ code: 'cs', name: 'Čeština', is_default: true }])
    i18n.global.locale.value = 'hr'
    mountSwitcher()
    expect(button().text()).toBe('cs')
    expect(options().every(o => o.attributes('aria-selected') === 'false')).toBe(true)
  })

  it('markiert die aktive Sprache mit aria-selected und Häkchen', () => {
    mountSwitcher()
    const [de, en] = options()
    expect(de.attributes('aria-selected')).toBe('true')
    expect(en.attributes('aria-selected')).toBe('false')
    expect(de.find('.language-switcher__check').exists()).toBe(true)
    expect(en.find('.language-switcher__check').exists()).toBe(false)
  })

  it('öffnet direkt unter dem Button, wechselt per Klick die Locale, schließt und behält den Fokus am Button', async () => {
    mountSwitcher()
    await button().trigger('click')
    expect(isOpen()).toBe(true)
    expect(list().isVisible()).toBe(true)
    await choose('en')
    expect(i18n.global.locale.value).toBe('en')
    expect(isOpen()).toBe(false)
    expect(document.activeElement).toBe(button().element)
  })

  it('Button schaltet um', async () => {
    mountSwitcher()
    await button().trigger('click')
    await button().trigger('click')
    expect(isOpen()).toBe(false)
  })

  it('Tastatur: Pfeil öffnet bei der aktiven Sprache, Pfeile bewegen, Enter übernimmt', async () => {
    mountSwitcher()
    await key('ArrowDown')
    expect(isOpen()).toBe(true)
    expect(button().attributes('aria-activedescendant')).toBe(options()[0].attributes('id'))
    await key('ArrowDown')
    expect(button().attributes('aria-activedescendant')).toBe(options()[1].attributes('id'))
    expect(options()[1].classes()).toContain('language-switcher__option--active')
    await key('Home')
    expect(button().attributes('aria-activedescendant')).toBe(options()[0].attributes('id'))
    await key('End')
    await key('Enter')
    await flushPromises()
    expect(i18n.global.locale.value).toBe('en')
    expect(isOpen()).toBe(false)
    expect(button().attributes('aria-activedescendant')).toBeUndefined()
  })

  it('Tastatur: Leertaste öffnet und übernimmt, andere Tasten bei geschlossener Liste nicht', async () => {
    mountSwitcher()
    await key('a')
    expect(isOpen()).toBe(false)
    await key(' ')
    expect(isOpen()).toBe(true)
    await key('x')
    expect(isOpen()).toBe(true)
    await key(' ')
    await flushPromises()
    expect(isOpen()).toBe(false)
    expect(i18n.global.locale.value).toBe('de')
  })

  it('Escape schließt ohne Wechsel, Tab schließt ebenfalls', async () => {
    mountSwitcher()
    await key('ArrowDown')
    await key('ArrowDown')
    await key('Escape')
    expect(isOpen()).toBe(false)
    expect(i18n.global.locale.value).toBe('de')
    expect(document.activeElement).not.toBe(null)

    await key('ArrowDown')
    await key('Tab')
    expect(isOpen()).toBe(false)
  })

  it('schließt bei Klick/Tap außerhalb, nicht bei Klick in die Liste', async () => {
    mountSwitcher()
    await button().trigger('click')
    list().element.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(isOpen()).toBe(true)
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(isOpen()).toBe(false)
  })

  it('übernimmt die Option unter dem Zeiger als aktive', async () => {
    mountSwitcher()
    await button().trigger('click')
    await options()[1].trigger('pointermove')
    expect(button().attributes('aria-activedescendant')).toBe(options()[1].attributes('id'))
  })

  it('setzt aria-label und title aus der Übersetzung', () => {
    mountSwitcher()
    expect(button().attributes('aria-label')).toBeTruthy()
    expect(button().attributes('title')).toBe(button().attributes('aria-label'))
    expect(list().attributes('aria-label')).toBe(button().attributes('aria-label'))
  })

  // fehlgeschlagenes Speichern ist für eingeloggte User sichtbar.
  it('zeigt bei fehlgeschlagenem Speichern eine Meldung, bei Erfolg keine', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    mountSwitcher()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)

    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse({ error: 'x' }, 500))
    await choose('en')
    expect(wrapper.find('[role="alert"]').text()).toBe(i18n.global.t('language.saveError'))

    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    await choose('de')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('reicht Attribute des Elternteils (z. B. class) an den Button durch, nicht an Liste oder Fehlermeldung', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    mountSwitcher({ attrs: { class: 'header-control' } })
    expect(button().classes()).toContain('header-control')
    expect(wrapper.classes()).not.toContain('header-control')
    expect(list().classes()).not.toContain('header-control')

    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse({ error: 'x' }, 500))
    await choose('en')
    expect(wrapper.find('[role="alert"]').classes()).not.toContain('header-control')
  })

  it('hat keine axe-Funde, geschlossen und geöffnet', async () => {
    mountSwitcher()
    expect(await axe(wrapper)).toHaveNoViolations()
    await button().trigger('click')
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
