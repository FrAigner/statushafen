import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { useAutoDismissAlerts, ALERT_AUTO_DISMISS_MS } from './useAutoDismissAlerts'

// Erfolgsmeldungen verschwinden nach einigen Sekunden, Fehler/Warnungen bleiben (Betreiber 31.10.2026).

const flushObserver = () => Promise.resolve().then(() => Promise.resolve())

function mountHost() {
  const Host = defineComponent({
    setup() {
      const el = ref(null)
      useAutoDismissAlerts(el)
      return () => h('main', { ref: el })
    },
  })
  return mount(Host, { attachTo: document.body })
}

function addAlert(root, kind) {
  const el = document.createElement('div')
  el.className = `alert alert-${kind}`
  const btn = document.createElement('button')
  btn.className = 'alert-close'
  const closed = vi.fn()
  btn.addEventListener('click', closed)
  el.appendChild(btn)
  root.appendChild(el)
  return closed
}

describe('useAutoDismissAlerts', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers(); document.body.innerHTML = '' })

  it('schließt eine Erfolgsmeldung nach der Frist über ihr Schließen-Kreuz', async () => {
    const w = mountHost()
    const closed = addAlert(w.element, 'success')
    await flushObserver()
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS - 1)
    expect(closed).not.toHaveBeenCalled()
    vi.advanceTimersByTime(2)
    expect(closed).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('lässt Fehler und Warnungen stehen', async () => {
    const w = mountHost()
    const err = addAlert(w.element, 'error')
    const warn = addAlert(w.element, 'warning')
    await flushObserver()
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS * 5)
    expect(err).not.toHaveBeenCalled()
    expect(warn).not.toHaveBeenCalled()
    w.unmount()
  })

  it('schließt eine verschachtelte Erfolgsmeldung und je Meldung nur einmal', async () => {
    const w = mountHost()
    const wrapper = document.createElement('div')
    w.element.appendChild(wrapper)
    await flushObserver()
    const closed = addAlert(wrapper, 'success')
    await flushObserver()
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS + 10)
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS * 3)
    expect(closed).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('erfasst auch eine bereits vorhandene Erfolgsmeldung beim Einhängen', () => {
    const Host = defineComponent({
      setup() {
        const el = ref(null)
        useAutoDismissAlerts(el, 1000)
        return () => h('main', { ref: el }, [h('div', { class: 'alert alert-success' }, [h('button', { class: 'alert-close', onClick: () => { Host.closed = (Host.closed || 0) + 1 } })])])
      },
    })
    const w = mount(Host, { attachTo: document.body })
    vi.advanceTimersByTime(1001)
    expect(Host.closed).toBe(1)
    w.unmount()
  })

  it('ignoriert Textknoten und Meldungen ohne Schließen-Kreuz ohne Fehler', async () => {
    const w = mountHost()
    w.element.appendChild(document.createTextNode('Text'))
    const bare = document.createElement('div')
    bare.className = 'alert alert-success'
    w.element.appendChild(bare)
    await flushObserver()
    expect(() => vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS + 1)).not.toThrow()
    w.unmount()
  })

  it('räumt beim Aushängen Timer auf: danach wird nichts mehr geklickt', async () => {
    const w = mountHost()
    const closed = addAlert(w.element, 'success')
    await flushObserver()
    w.unmount()
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS * 2)
    expect(closed).not.toHaveBeenCalled()
  })

  // Review: Die Ansichten rendern EINEN Meldungsknoten; der Wechsel Fehler -> Erfolg patcht nur Klasse/Text.
  it('bewaffnet die Frist, wenn dasselbe Element von Warnung auf Erfolg wechselt', async () => {
    const w = mountHost()
    const closed = addAlert(w.element, 'warning')
    await flushObserver()
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS * 2)
    expect(closed).not.toHaveBeenCalled() // Warnung bleibt

    const el = w.element.querySelector('.alert')
    el.className = 'alert alert-success'
    await flushObserver()
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS + 10)
    expect(closed).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('löscht den Timer, wenn aus Erfolg wieder ein Fehler wird', async () => {
    const w = mountHost()
    const closed = addAlert(w.element, 'success')
    await flushObserver()
    w.element.querySelector('.alert').className = 'alert alert-error'
    await flushObserver()
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS * 2)
    expect(closed).not.toHaveBeenCalled()
    w.unmount()
  })

  it('startet die Frist neu, wenn im Fenster eine zweite Erfolgsmeldung im selben Element erscheint', async () => {
    const w = mountHost()
    const closed = addAlert(w.element, 'success')
    await flushObserver()
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS - 1000)
    w.element.querySelector('.alert').insertBefore(document.createTextNode('Zweite Meldung'), null) // Textwechsel
    await flushObserver()
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS - 1000) // alte Frist wäre jetzt abgelaufen
    expect(closed).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1100)
    expect(closed).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('löscht den Timer einer entfernten Meldung (kein Klick auf ein ausgehängtes Element)', async () => {
    const w = mountHost()
    const closed = addAlert(w.element, 'success')
    await flushObserver()
    w.element.querySelector('.alert').remove()
    await flushObserver()
    vi.advanceTimersByTime(ALERT_AUTO_DISMISS_MS * 2)
    expect(closed).not.toHaveBeenCalled()
    w.unmount()
  })

  it('tut ohne gebundenen Container nichts', () => {
    const Host = defineComponent({
      setup() {
        useAutoDismissAlerts(ref(null))
        return () => h('main')
      },
    })
    const w = mount(Host)
    expect(() => w.unmount()).not.toThrow()
  })

  it('Meldungen kleben im Scrollbereich oben (sticky) und sind sichtbar', () => {
    const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '..', 'style.css'), 'utf-8')
    const rule = css.match(/\.admin-content \.alert\s*\{([^}]*)\}/)[1]
    expect(rule).toMatch(/position:\s*sticky/)
    expect(rule).toMatch(/top:\s*var\(--space-sm\)/)
    expect(rule).toMatch(/z-index:\s*50/)
  })
})
