import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'
import { usePublicPageLanguages } from '../composables/usePublicPageLanguages'
import LanguageSwitcher from './LanguageSwitcher.vue'
import { axe } from '../test-support/axe.js'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

describe('LanguageSwitcher', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    useAuth().logout()
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    usePublicPageLanguages().clearPageLanguages()
  })

  // auf einer Statusseite alle Sprachen der Seite, nicht nur de/en.
  it('bietet auf einer Statusseite alle Sprachen der Seite an und wechselt auf eine ohne Oberflächentexte', async () => {
    usePublicPageLanguages().setPageLanguages([
      { code: 'cs', name: 'Čeština', is_default: false },
      { code: 'de', name: 'Deutsch', is_default: true },
      { code: 'en', name: 'English', is_default: false },
      { code: 'hr', name: 'Hrvatski', is_default: false },
    ])
    const wrapper = mount(LanguageSwitcher, { global: { plugins: [i18n] } })

    expect(wrapper.findAll('option').map((o) => [o.element.value, o.text()])).toEqual([
      ['cs', 'Čeština'], ['de', 'Deutsch'], ['en', 'English'], ['hr', 'Hrvatski'],
    ])
    await wrapper.find('select').setValue('hr')
    expect(i18n.global.locale.value).toBe('hr')
    expect(wrapper.find('select').element.value).toBe('hr')
  })

  it('rendert eine Option je unterstützter Sprache und markiert die aktive Locale als ausgewählt', () => {
    const wrapper = mount(LanguageSwitcher, { global: { plugins: [i18n] } })

    const select = wrapper.find('select')
    expect(select.element.value).toBe('de')
    expect(select.findAll('option').length).toBeGreaterThanOrEqual(2)
  })

  it('wechselt bei Auswahl die aktive Locale über useLanguage', async () => {
    const wrapper = mount(LanguageSwitcher, { global: { plugins: [i18n] } })

    await wrapper.find('select').setValue('en')

    expect(i18n.global.locale.value).toBe('en')
  })

  it('setzt aria-label und title aus der Übersetzung', () => {
    const wrapper = mount(LanguageSwitcher, { global: { plugins: [i18n] } })

    const select = wrapper.find('select')
    expect(select.attributes('aria-label')).toBeTruthy()
    expect(select.attributes('title')).toBe(select.attributes('aria-label'))
  })

  // fehlgeschlagenes Speichern ist für eingeloggte User sichtbar.
  it('zeigt bei fehlgeschlagenem Speichern eine Meldung, bei Erfolg keine', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    const wrapper = mount(LanguageSwitcher, { global: { plugins: [i18n] } })
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)

    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse({ error: 'x' }, 500))
    await wrapper.find('select').setValue('en')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toBe(i18n.global.t('language.saveError'))

    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    await wrapper.find('select').setValue('de')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('reicht Attribute des Elternteils (z. B. class) ans select durch, nicht an die Fehlermeldung', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    const wrapper = mount(LanguageSwitcher, {
      attrs: { class: 'header-control' },
      global: { plugins: [i18n] },
    })
    expect(wrapper.find('select').classes()).toContain('header-control')

    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse({ error: 'x' }, 500))
    await wrapper.find('select').setValue('en')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').classes()).not.toContain('header-control')
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = mount(LanguageSwitcher, { global: { plugins: [i18n] } })
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
