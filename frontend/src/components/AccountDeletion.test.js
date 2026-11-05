import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { routerKey } from 'vue-router'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'
import AccountDeletion from './AccountDeletion.vue'
import { axe } from '../test-support/axe.js'

// Konto-löschen-Bereich im Profil.

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

const PAGES = [
  { id: 3, name: 'Solo Page', slug: 'solo-page' },
  { id: 7, name: 'Orphan Page', slug: 'orphan-page' },
]

let wrapper = null

function mountDeletion(router = { push: vi.fn() }) {
  wrapper = mount(AccountDeletion, {
    global: { plugins: [i18n], provide: { [routerKey]: router } },
  })
  return { wrapper, router }
}

async function openAndFill(w, { email = 'a@b.de', password = 'geheim' } = {}) {
  await w.find('[data-test="account-delete-open"]').trigger('click')
  await w.find('#account-delete-email').setValue(email)
  await w.find('#account-delete-password').setValue(password)
}

function lastRequestBody() {
  const call = global.fetch.mock.calls.at(-1)
  return JSON.parse(call[1].body)
}

describe('AccountDeletion', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    useAuth().setAuth('jwt-1', { email: 'a@b.de' })
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.restoreAllMocks()
    useAuth().logout()
  })

  it('zeigt zuerst nur Beschreibung und Öffnen-Button, der Dialog öffnet sich per Klick', async () => {
    const { wrapper: w } = mountDeletion()
    expect(w.text()).toContain(i18n.global.t('accountDeletion.title'))
    expect(w.find('[role="alertdialog"]').exists()).toBe(false)

    await w.find('[data-test="account-delete-open"]').trigger('click')

    const dialog = w.find('[role="alertdialog"]')
    expect(dialog.exists()).toBe(true)
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(dialog.find('h4 .app-icon').exists()).toBe(true)
    expect(dialog.text()).toContain(i18n.global.t('accountDeletion.warning'))
    expect(w.find('[data-test="account-delete-open"]').exists()).toBe(false)
  })

  it('aktiviert den Löschen-Button erst, wenn die E-Mail eingetragen ist', async () => {
    const { wrapper: w } = mountDeletion()
    await w.find('[data-test="account-delete-open"]').trigger('click')
    const submit = () => w.find('[data-test="account-delete-submit"]')
    expect(submit().attributes('disabled')).toBeDefined()

    await w.find('#account-delete-email').setValue('   ')
    expect(submit().attributes('disabled')).toBeDefined()

    await w.find('#account-delete-email').setValue('a@b.de')
    expect(submit().attributes('disabled')).toBeUndefined()
  })

  it('löscht das Konto per DELETE /api/me, meldet ab und leitet zum Login', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ message: 'ok', deleted_pages: [] }))
    const { wrapper: w, router } = mountDeletion()
    await openAndFill(w)

    await w.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/me'),
      expect.objectContaining({ method: 'DELETE' })
    )
    expect(lastRequestBody()).toEqual({ confirm_email: 'a@b.de', password: 'geheim' })
    expect(useAuth().token.value).toBe('')
    expect(router.push).toHaveBeenCalledWith('/login')
  })

  it('kommt ohne Router aus (meldet trotzdem ab)', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ message: 'ok', deleted_pages: [] }))
    wrapper = mount(AccountDeletion, { global: { plugins: [i18n] } })
    await openAndFill(wrapper)

    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(useAuth().token.value).toBe('')
  })

  it('nennt bei 409 die betroffenen Seiten und verlangt eine zweite, ausdrückliche Bestätigung', async () => {
    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: 'x', code: 'sole_admin_pages', pages: PAGES }, 409))
      .mockResolvedValueOnce(jsonResponse({ message: 'ok', deleted_pages: PAGES }))
    const { wrapper: w, router } = mountDeletion()
    await openAndFill(w)

    await w.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(useAuth().token.value).toBe('jwt-1')
    const pages = w.find('[data-test="account-delete-pages"]')
    expect(pages.text()).toContain('Solo Page')
    expect(pages.text()).toContain('orphan-page')
    expect(pages.text()).toContain(i18n.global.t('accountDeletion.pagesIntro'))
    const submit = () => w.find('[data-test="account-delete-submit"]')
    expect(submit().text()).toBe(i18n.global.t('accountDeletion.submitWithPages'))
    expect(submit().attributes('disabled')).toBeDefined()
    expect(w.find('.alert').exists()).toBe(false)

    await w.find('[data-test="account-delete-pages-ack"]').setValue(true)
    expect(submit().attributes('disabled')).toBeUndefined()

    await w.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(lastRequestBody()).toEqual({ confirm_email: 'a@b.de', password: 'geheim', confirm_delete_page_ids: [3, 7] })
    expect(useAuth().token.value).toBe('')
    expect(router.push).toHaveBeenCalledWith('/login')
  })

  it.each([
    [403, { error: 'x', code: 'confirmation_invalid' }, 'accountDeletion.confirmationInvalid'],
    [409, { error: 'x', code: 'last_global_admin' }, 'accountDeletion.lastGlobalAdmin'],
    [500, { error: 'x' }, 'accountDeletion.error'],
  ])('zeigt bei Status %i die passende Fehlermeldung und bleibt angemeldet', async (status, body, key) => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse(body, status))
    const { wrapper: w, router } = mountDeletion()
    await openAndFill(w)

    await w.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(w.find('.alert-warning').text()).toBe(i18n.global.t(key))
    expect(w.find('[data-test="account-delete-pages"]').exists()).toBe(false)
    expect(useAuth().token.value).toBe('jwt-1')
    expect(router.push).not.toHaveBeenCalled()
    expect(w.find('[data-test="account-delete-submit"]').attributes('disabled')).toBeUndefined()
  })

  it('zeigt bei Netzwerkfehler die allgemeine Fehlermeldung', async () => {
    global.fetch = vi.fn().mockRejectedValue(new Error('offline'))
    const { wrapper: w } = mountDeletion()
    await openAndFill(w)

    await w.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(w.find('.alert-warning').text()).toBe(i18n.global.t('accountDeletion.error'))
    expect(useAuth().token.value).toBe('jwt-1')
  })

  it('sperrt den Button während des Löschens', async () => {
    let resolve
    global.fetch = vi.fn().mockReturnValue(new Promise((r) => { resolve = r }))
    const { wrapper: w } = mountDeletion()
    await openAndFill(w)

    await w.find('form').trigger('submit.prevent')
    const submit = w.find('[data-test="account-delete-submit"]')
    expect(submit.attributes('disabled')).toBeDefined()
    expect(submit.text()).toBe(i18n.global.t('common.deleting'))

    resolve(jsonResponse({ message: 'ok', deleted_pages: [] }))
    await flushPromises()
  })

  it('Abbrechen schließt den Dialog und verwirft Eingaben, Seitenliste und Fehler', async () => {
    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: 'x', code: 'sole_admin_pages', pages: PAGES }, 409))
      .mockResolvedValueOnce(jsonResponse({ error: 'x' }, 500))
    const { wrapper: w } = mountDeletion()
    await openAndFill(w)
    await w.find('form').trigger('submit.prevent')
    await flushPromises()
    await w.find('[data-test="account-delete-pages-ack"]').setValue(true)
    await w.find('form').trigger('submit.prevent')
    await flushPromises()
    expect(w.find('.alert-warning').exists()).toBe(true)

    await w.find('[data-test="account-delete-cancel"]').trigger('click')
    expect(w.find('[role="alertdialog"]').exists()).toBe(false)

    await w.find('[data-test="account-delete-open"]').trigger('click')
    expect(w.find('#account-delete-email').element.value).toBe('')
    expect(w.find('#account-delete-password').element.value).toBe('')
    expect(w.find('[data-test="account-delete-pages"]').exists()).toBe(false)
    expect(w.find('.alert-warning').exists()).toBe(false)
  })

  it('setzt die Seiten-Bestätigung zurück, wenn nach einem 409 erneut ein 409 kommt', async () => {
    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: 'x', code: 'sole_admin_pages', pages: PAGES.slice(0, 1) }, 409))
      .mockResolvedValueOnce(jsonResponse({ error: 'x', code: 'sole_admin_pages', pages: PAGES }, 409))
    const { wrapper: w } = mountDeletion()
    await openAndFill(w)
    await w.find('form').trigger('submit.prevent')
    await flushPromises()
    await w.find('[data-test="account-delete-pages-ack"]').setValue(true)

    await w.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(w.find('[data-test="account-delete-pages"]').text()).toContain('Orphan Page')
    expect(w.find('[data-test="account-delete-pages-ack"]').element.checked).toBe(false)
    expect(w.find('[data-test="account-delete-submit"]').attributes('disabled')).toBeDefined()
  })

  it('hat alle Texte auch auf Englisch', async () => {
    i18n.global.locale.value = 'en'
    const { wrapper: w } = mountDeletion()
    expect(w.text()).toContain('Delete account')
  })

  it('hat keine axe-Funde, weder geschlossen noch im offenen Dialog', async () => {
    const { wrapper } = mountDeletion()
    expect(await axe(wrapper)).toHaveNoViolations()

    await openAndFill(wrapper)
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Pflichtfeld E-Mail trägt Stern (Klasse), aria-required und den Hinweis.
  it('kennzeichnet die E-Mail-Bestätigung als Pflichtfeld', async () => {
    const { wrapper: w } = mountDeletion()
    await w.find('[data-test="account-delete-open"]').trigger('click')
    expect(w.find('label[for="account-delete-email"]').classes()).toContain('onyx-required-marker')
    expect(w.find('#account-delete-email').attributes('aria-required')).toBe('true')
    expect(w.find('[data-test="required-hint"]').text()).toBe('Felder mit * sind Pflichtfelder.')
  })
})
