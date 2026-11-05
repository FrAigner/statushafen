import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import ProfileView from './ProfileView.vue'
import { axe } from '../../test-support/axe.js'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

let activeWrapper = null

function mountProfile() {
  const wrapper = mount(ProfileView, {
    // SSOLinks lädt selbst (eigene Tests in SSOLinks.test.js).
    global: { plugins: [i18n], stubs: { ...onyxFormStubs, SSOLinks: true } },
  })
  activeWrapper = wrapper
  return wrapper
}

describe('ProfileView', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    useAuth().setAuth('jwt-1', { email: 'a@b.de' })
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
    useAuth().logout()
  })

  it('zeigt den Beschreibungstext aus profile.description statt des Entwicklerhinweises', () => {
    const wrapper = mountProfile()

    expect(wrapper.text()).toContain(i18n.global.t('profile.description'))
    expect(wrapper.text()).not.toMatch(/Issue #\d+/)
  })

  // Stufe 4d: /admin/profile liegt im AdminLayout, dessen Topbar die
  // h1 trägt. Der Seitentitel ist eine h2, ohne Inline-Styles.
  it('rendert den Titel als h2 ohne h1', () => {
    const wrapper = mountProfile()

    expect(wrapper.find('h2.page-head__title').text()).toBe(i18n.global.t('profile.title'))
    expect(wrapper.find('h1').exists()).toBe(false)
  })

  it('zeigt ohne angemeldeten Benutzer keine E-Mail-Zeile', () => {
    useAuth().logout()
    const wrapper = mountProfile()

    expect(wrapper.find('.profile-email').exists()).toBe(false)
  })

  // Konto-löschen-Bereich im Profil.
  it('bindet den Bereich zum Löschen des Kontos ein', () => {
    const wrapper = mountProfile()

    expect(wrapper.findComponent({ name: 'AccountDeletion' }).exists()).toBe(true)
    expect(wrapper.text()).toContain(i18n.global.t('accountDeletion.title'))
  })

  // Bereich „Mit SSO verknüpfen“.
  it('bindet den Bereich für SSO-Verknüpfungen ein', () => {
    const wrapper = mountProfile()

    expect(wrapper.findComponent({ name: 'SSOLinks' }).exists()).toBe(true)
  })

  // Schalter „Hilfe-Tooltips anzeigen“ auch im Profil.
  it('bindet den Schalter für Hilfe-Tooltips ein (Default an)', () => {
    const wrapper = mountProfile()

    const toggle = wrapper.findComponent({ name: 'HelpTooltipsToggle' })
    expect(toggle.exists()).toBe(true)
    expect(toggle.text()).toContain('Hilfe-Tooltips anzeigen')
    expect(toggle.find('input[type="checkbox"]').element.checked).toBe(true)
  })

  it('zeigt die E-Mail des eingeloggten Users', () => {
    const wrapper = mountProfile()

    expect(wrapper.text()).toContain('a@b.de')
  })

  it('speichert die gewählte Sprache und zeigt eine Erfolgsmeldung', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    const wrapper = mountProfile()

    await wrapper.find('.onyx-select-stub select').setValue('en')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(i18n.global.locale.value).toBe('en')
    expect(wrapper.text()).toContain('Language saved.')
    expect(wrapper.find('.alert-success').exists()).toBe(true)
    expect(wrapper.find('.alert-warning').exists()).toBe(false)
  })

  it('persistiert die serverseitige Sprachpräferenz per PATCH /api/me', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    const wrapper = mountProfile()

    await wrapper.find('.onyx-select-stub select').setValue('en')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/me'),
      expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ preferred_language: 'en' }) })
    )
  })

  // bei serverseitigem Fehler nicht "gespeichert" melden.
  it('zeigt bei fehlgeschlagenem Speichern die Fehlermeldung statt der Erfolgsmeldung', async () => {
    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse({ error: 'x' }, 500))
    const wrapper = mountProfile()

    await wrapper.find('.onyx-select-stub select').setValue('en')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Language saved.')
    expect(wrapper.text()).toContain('Language could not be saved.')
    expect(wrapper.find('.alert-warning').exists()).toBe(true)
    expect(wrapper.find('.alert-success').exists()).toBe(false)
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = mountProfile()
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Richtung C: Brotkrumen und Spracheinstellung in einer Karte.
  it('zeigt Brotkrumen und die Spracheinstellung in einer Karte mit Titel', () => {
    const wrapper = mountProfile()

    expect(wrapper.find('.page-head__crumbs').text()).toContain('Administration')
    expect(wrapper.find('.page-head__crumbs [aria-current="page"]').text()).toBe(i18n.global.t('profile.title'))
    expect(wrapper.find('section.comp-card h3').text()).toBe(i18n.global.t('profile.cardTitle'))
    expect(wrapper.find('section.comp-card form').exists()).toBe(true)
  })
})
