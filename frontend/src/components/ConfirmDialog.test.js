import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import i18n from '../i18n'
import ConfirmDialog from './ConfirmDialog.vue'
import { confirmDialog, answerConfirm } from '../composables/useConfirm'
import { axe } from '../test-support/axe.js'

// gemeinsamer Bestätigungsdialog statt natives confirm().

let wrapper = null
const q = (sel) => document.body.querySelector(sel)
const key = (el, k, extra = {}) => el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...extra }))
const settle = async () => { await nextTick(); await nextTick() }

describe('ConfirmDialog', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    wrapper = mount(ConfirmDialog, { global: { plugins: [i18n] }, attachTo: document.body })
  })
  afterEach(() => {
    answerConfirm(false)
    wrapper.unmount()
    document.body.innerHTML = ''
  })

  it('ist ohne Aufruf nicht sichtbar', () => {
    expect(q('[role="alertdialog"]')).toBeNull()
  })

  it('zeigt Text und Titel als alertdialog mit Namen und Beschreibung', async () => {
    confirmDialog({ message: 'Benutzer „a@b.de“ wirklich löschen?' })
    await settle()
    const dlg = q('[role="alertdialog"]')
    expect(dlg.getAttribute('aria-modal')).toBe('true')
    expect(document.getElementById(dlg.getAttribute('aria-labelledby')).textContent).toBe('Bitte bestätigen')
    expect(document.getElementById(dlg.getAttribute('aria-describedby')).textContent).toBe('Benutzer „a@b.de“ wirklich löschen?')
  })

  it('übernimmt Titel und Beschriftung des Aufrufers; ohne Beschriftung gilt "Löschen" (danger) bzw. "Bestätigen"', async () => {
    confirmDialog({ message: 'm', title: 'Eigener Titel', confirmLabel: 'Lösen' })
    await settle()
    expect(q('#confirm-dialog-title').textContent).toBe('Eigener Titel')
    expect(q('[data-test="confirm-ok"]').textContent.trim()).toBe('Lösen')
    answerConfirm(false)
    await settle()
    confirmDialog({ message: 'm' })
    await settle()
    expect(q('[data-test="confirm-ok"]').textContent.trim()).toBe('Löschen')
    expect(q('[data-test="confirm-ok"]').classList.contains('btn-danger')).toBe(true)
    answerConfirm(false)
    await settle()
    confirmDialog({ message: 'm', danger: false })
    await settle()
    expect(q('[data-test="confirm-ok"]').textContent.trim()).toBe('Bestätigen')
    expect(q('[data-test="confirm-ok"]').classList.contains('btn-primary')).toBe(true)
  })

  it('Bestätigen liefert true und schließt', async () => {
    const p = confirmDialog({ message: 'm' })
    await settle()
    q('[data-test="confirm-ok"]').click()
    expect(await p).toBe(true)
    await settle()
    expect(q('[role="alertdialog"]')).toBeNull()
  })

  it('Abbrechen, Esc und Klick auf den Hintergrund liefern false', async () => {
    let p = confirmDialog({ message: 'm' })
    await settle()
    q('[data-test="confirm-cancel"]').click()
    expect(await p).toBe(false)
    await settle()

    p = confirmDialog({ message: 'm' })
    await settle()
    key(q('[role="alertdialog"]'), 'Escape')
    expect(await p).toBe(false)
    await settle()

    p = confirmDialog({ message: 'm' })
    await settle()
    q('[data-test="confirm-backdrop"]').click()
    expect(await p).toBe(false)
  })

  it('ein Klick im Dialog selbst schließt ihn nicht', async () => {
    const p = confirmDialog({ message: 'm' })
    await settle()
    q('.confirm-dialog').click()
    expect(q('[role="alertdialog"]')).not.toBeNull()
    answerConfirm(true)
    expect(await p).toBe(true)
  })

  it('Fokus startet auf "Abbrechen", damit Enter nichts Gefährliches bestätigt', async () => {
    confirmDialog({ message: 'm' })
    await settle()
    expect(document.activeElement).toBe(q('[data-test="confirm-cancel"]'))
  })

  it('Tab bleibt im Dialog (Fokusfalle in beide Richtungen)', async () => {
    confirmDialog({ message: 'm' })
    await settle()
    const cancel = q('[data-test="confirm-cancel"]')
    const ok = q('[data-test="confirm-ok"]')
    ok.focus()
    key(ok, 'Tab')
    expect(document.activeElement).toBe(cancel)
    key(cancel, 'Tab', { shiftKey: true })
    expect(document.activeElement).toBe(ok)
    // innerhalb des Rings nicht eingreifen
    cancel.focus()
    const e = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    cancel.dispatchEvent(e)
    expect(e.defaultPrevented).toBe(false)
    const e2 = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true })
    ok.focus()
    ok.dispatchEvent(e2)
    expect(e2.defaultPrevented).toBe(false)
    // andere Tasten werden ignoriert
    const e3 = new KeyboardEvent('keydown', { key: 'a', bubbles: true, cancelable: true })
    ok.dispatchEvent(e3)
    expect(q('[role="alertdialog"]')).not.toBeNull()
  })

  it('gibt den Fokus an das auslösende Element zurück', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()
    const p = confirmDialog({ message: 'm' })
    await settle()
    expect(document.activeElement).not.toBe(trigger)
    q('[data-test="confirm-cancel"]').click()
    await p
    await settle()
    expect(document.activeElement).toBe(trigger)
  })

  it('ein zweiter Aufruf beantwortet den ersten mit false', async () => {
    const first = confirmDialog({ message: 'eins' })
    await settle()
    const second = confirmDialog({ message: 'zwei' })
    expect(await first).toBe(false)
    await settle()
    expect(q('#confirm-dialog-message').textContent).toBe('zwei')
    answerConfirm(true)
    expect(await second).toBe(true)
  })

  it('verwendet die Sprache der Oberfläche', async () => {
    i18n.global.locale.value = 'en'
    confirmDialog({ message: 'm' })
    await settle()
    expect(q('#confirm-dialog-title').textContent).toBe('Please confirm')
    expect(q('[data-test="confirm-cancel"]').textContent.trim()).toBe('Cancel')
    expect(q('[data-test="confirm-ok"]').textContent.trim()).toBe('Delete')
  })

  it('hat keine axe-Funde', async () => {
    confirmDialog({ message: 'Rolle „Viewer“ wirklich löschen?' })
    await settle()
    expect(await axe(q('.confirm-backdrop'))).toHaveNoViolations()
  })

  it('Esc schließt auch, wenn der Fokus nach einem Klick auf den Text auf dem Body liegt', async () => {
    const p = confirmDialog({ message: 'm' })
    await settle()
    q('#confirm-dialog-message').click()
    document.activeElement.blur()
    expect(document.activeElement).toBe(document.body)
    key(document.body, 'Escape')
    expect(await p).toBe(false)
  })

  it('Tab von außerhalb des Dialogs holt den Fokus in den Dialog zurück', async () => {
    confirmDialog({ message: 'm' })
    await settle()
    document.activeElement.blur()
    key(document.body, 'Tab')
    expect(document.activeElement).toBe(q('[data-test="confirm-cancel"]'))
    document.activeElement.blur()
    key(document.body, 'Tab', { shiftKey: true })
    expect(document.activeElement).toBe(q('[data-test="confirm-ok"]'))
  })

  it('sperrt die Seite dahinter (inert, kein Scrollen) und gibt sie beim Schließen frei', async () => {
    const app = document.createElement('div')
    app.id = 'app'
    document.body.appendChild(app)
    const p = confirmDialog({ message: 'm' })
    await settle()
    expect(app.inert).toBe(true)
    expect(document.body.style.overflow).toBe('hidden')
    answerConfirm(false)
    await p
    expect(app.inert).toBe(false)
    expect(document.body.style.overflow).toBe('')
  })

  it('übernimmt die Themeklasse des auslösenden Elements (Admin-Ebene bzw. öffentlich)', async () => {
    const admin = document.createElement('div')
    admin.className = 'theme-admin'
    const btn = document.createElement('button')
    admin.appendChild(btn)
    document.body.appendChild(admin)
    btn.focus()
    confirmDialog({ message: 'm' })
    await settle()
    expect(q('.confirm-backdrop').classList.contains('theme-admin')).toBe(true)
    answerConfirm(false)
    await settle()
    document.activeElement.blur()
    confirmDialog({ message: 'm' })
    await settle()
    expect(q('.confirm-backdrop').classList.contains('theme-public')).toBe(true)
  })

  it('Abmeldung (Token weg, z. B. 401) schließt den offenen Dialog mit false', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@b.de' })
    const p = confirmDialog({ message: 'm' })
    await settle()
    useAuth().logout()
    expect(await p).toBe(false)
    await settle()
    expect(q('[role="alertdialog"]')).toBeNull()
  })
})

describe('ConfirmDialog mit Router', () => {
  it('Navigation schließt den offenen Dialog mit false, ein späteres Bestätigen löst nichts mehr aus', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<i />' } }, { path: '/b', component: { template: '<i />' } }],
    })
    await router.push('/')
    await router.isReady()
    const w = mount(ConfirmDialog, { global: { plugins: [i18n, router] }, attachTo: document.body })
    const p = confirmDialog({ message: 'Alte Seite löschen?' })
    await settle()
    expect(q('[role="alertdialog"]')).not.toBeNull()
    await router.push('/b')
    expect(await p).toBe(false)
    await settle()
    expect(q('[role="alertdialog"]')).toBeNull()
    w.unmount()
    document.body.innerHTML = ''
  })
})
