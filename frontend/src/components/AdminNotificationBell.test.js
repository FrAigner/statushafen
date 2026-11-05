import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'
import { useTenant } from '../composables/useTenant'
import { setAuthAsPageRole } from '../test-support/pageRole'
import { axe } from '../test-support/axe.js'
import AdminNotificationBell from './AdminNotificationBell.vue'

// Nachrichtenglocke der Admin-Kopfzeile.

const note = (over = {}) => ({
  id: 1, severity: 'error', kind: 'external_source_outage',
  title_key: 'adminNotifications.external_source_outage.title',
  body_key: 'adminNotifications.external_source_outage.body',
  params: { source: 'Example Cloud', source_id: 3 }, link_path: '/admin/external-sources',
  count: 1, last_seen_at: '2026-10-30T10:00:00Z', first_seen_at: '2026-10-30T10:00:00Z',
  resolved_at: null, read: false, ...over,
})

let server
function mockApi() {
  server = { count: 3, items: [note()], calls: [], failList: false }
  global.fetch = vi.fn(async (url, opts = {}) => {
    server.calls.push(`${opts.method || 'GET'} ${url}`)
    const json = async () => {
      const system = url.startsWith('/api/admin-notifications')
      if (url.includes('/unread-count')) return { unread_count: system ? 0 : server.count }
      if (url.includes('/admin-notifications?')) return { items: system ? [] : server.items }
      return null
    }
    if (url.includes('/admin-notifications?') && server.failList) return { ok: false, status: 500, json: async () => ({}) }
    if (opts.method === 'POST') return { ok: true, status: 204, json }
    return { ok: true, status: 200, json }
  })
}

const router = () => createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/', component: { template: '<div/>' } }, { path: '/admin/external-sources', component: { template: '<div/>' } }, { path: '/admin/subscribers', component: { template: '<div/>' } }],
})

async function mountBell() {
  const r = router()
  r.push('/')
  await r.isReady()
  const wrapper = mount(AdminNotificationBell, { global: { plugins: [i18n, r] }, attachTo: document.body })
  await flushPromises()
  return wrapper
}

describe('AdminNotificationBell', () => {
  beforeEach(() => {
    useAuth().logout()
    localStorage.clear()
    i18n.global.locale.value = 'de'
    useTenant().setActiveTenant(1)
    useTenant().setMyPages([])
    mockApi()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('zeigt Badge mit Zahl und nennt Zahl und Text im aria-label', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    const w = await mountBell()
    const btn = w.find('.notif-button')
    expect(btn.attributes('aria-label')).toBe('Nachrichten, 3 ungelesen')
    expect(w.find('.notif-badge').text()).toBe('3')
    expect(btn.attributes('aria-expanded')).toBe('false')
    w.unmount()
  })

  it('kappt das Badge bei 9+, das aria-label behält die genaue Zahl', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    mockApi(); server.count = 25
    const w = await mountBell()
    expect(w.find('.notif-badge').text()).toBe('9+')
    expect(w.find('.notif-button').attributes('aria-label')).toBe('Nachrichten, 25 ungelesen')
    w.unmount()
  })

  it('zeigt bei 0 ungelesenen kein Badge und das schlichte Label', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    mockApi(); server.count = 0
    const w = await mountBell()
    expect(w.find('.notif-badge').exists()).toBe(false)
    expect(w.find('.notif-button').attributes('aria-label')).toBe('Nachrichten')
    w.unmount()
  })

  it('fehlt ganz ohne passendes Recht (Betrachter), sonst da (external_sources:update / subscribers:update)', async () => {
    setAuthAsPageRole('t', { email: 'v@x.de', is_global_admin: false, permissions: 'components:read' })
    let w = await mountBell()
    expect(w.find('.notif').exists()).toBe(false)
    expect(server.calls).toEqual([])
    w.unmount()
    setAuthAsPageRole('t', { email: 'v@x.de', is_global_admin: false, permissions: 'subscribers:update' })
    w = await mountBell()
    expect(w.find('.notif').exists()).toBe(true)
    w.unmount()
  })

  it('öffnet das Panel mit Dialog-Rolle, Fokus im Panel, Schweregrad als Text, Zähler, Link und Zeit', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    server.items = [note({ count: 4 })]
    const w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()

    const panel = w.find('[role="dialog"]')
    expect(panel.exists()).toBe(true)
    expect(w.find('.notif-button').attributes('aria-expanded')).toBe('true')
    expect(panel.element.contains(document.activeElement)).toBe(true)
    expect(panel.text()).toContain('Fehler')
    expect(panel.text()).toContain('Externe Quelle nicht erreichbar')
    expect(panel.text()).toContain('Example Cloud')
    expect(panel.text()).toContain('4-mal')
    expect(panel.find('time').exists()).toBe(true)
    expect(panel.find('a.notif-action').attributes('href')).toBe('/admin/external-sources')
    w.unmount()
  })

  it('Escape schließt und gibt den Fokus an die Glocke zurück', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    const w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    await w.find('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(w.find('.notif-button').element)
    w.unmount()
  })

  it('schließt bei Klick daneben, bleibt bei Klick im Panel offen und schließt bei Fokus nach außen', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    const w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    await w.find('.notif-item-title').trigger('click')
    expect(w.find('[role="dialog"]').exists()).toBe(true)
    await w.find('[role="dialog"]').trigger('focusout', { relatedTarget: document.body })
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    document.body.click()
    await flushPromises()
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    w.unmount()
  })

  it('Glocke schließt das Panel beim zweiten Klick', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    const w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    await w.find('.notif-button').trigger('click')
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    w.unmount()
  })

  it('Als gelesen markiert eine Nachricht und senkt den Zähler', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    const w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    const readBtn = w.findAll('.notif-action').find(b => b.text() === 'Als gelesen')
    await readBtn.trigger('click')
    await flushPromises()
    expect(server.calls).toContain('POST /api/pages/1/admin-notifications/1/read')
    expect(w.find('.notif-button').attributes('aria-label')).toBe('Nachrichten, 2 ungelesen')
    expect(w.findAll('.notif-action').some(b => b.text() === 'Als gelesen')).toBe(false)
    w.unmount()
  })

  it('Als gelesen scheitert: Zustand wird zurückgesetzt und der Zähler neu geholt', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    const w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    const ok = global.fetch
    global.fetch = vi.fn(async (url, opts = {}) => (opts.method === 'POST'
      ? { ok: false, status: 500, json: async () => ({}) }
      : ok(url, opts)))
    await w.findAll('.notif-action').find(b => b.text() === 'Als gelesen').trigger('click')
    await flushPromises()
    expect(w.findAll('.notif-action').some(b => b.text() === 'Als gelesen')).toBe(true)
    expect(w.find('.notif-button').attributes('aria-label')).toBe('Nachrichten, 3 ungelesen')
    w.unmount()
  })

  it('Alle als gelesen ruft beide Quellen (Global Admin) und leert den Zähler', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    const w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    await w.findAll('.notif-action').find(b => b.text() === 'Alle als gelesen').trigger('click')
    await flushPromises()
    expect(server.calls).toContain('POST /api/pages/1/admin-notifications/read-all')
    expect(server.calls).toContain('POST /api/admin-notifications/read-all')
    expect(w.find('.notif-badge').exists()).toBe(false)
    expect(w.findAll('.notif-action').some(b => b.text() === 'Alle als gelesen')).toBe(false)
    w.unmount()
  })

  it('Alle als gelesen scheitert: Stand wird zurückgesetzt', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: false, permissions: '' })
    setAuthAsPageRole('t', { email: 'a@x.de', is_global_admin: false, permissions: 'external_sources:update' })
    const w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    const ok = global.fetch
    global.fetch = vi.fn(async (url, opts = {}) => (opts.method === 'POST'
      ? { ok: false, status: 500, json: async () => ({}) }
      : ok(url, opts)))
    await w.findAll('.notif-action').find(b => b.text() === 'Alle als gelesen').trigger('click')
    await flushPromises()
    expect(w.find('.notif-badge').text()).toBe('3')
    expect(w.findAll('.notif-action').some(b => b.text() === 'Als gelesen')).toBe(true)
    w.unmount()
  })

  it('Folgen des Links markiert als gelesen und schließt das Panel', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    const w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    await w.find('a.notif-action').trigger('click')
    await flushPromises()
    expect(server.calls).toContain('POST /api/pages/1/admin-notifications/1/read')
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    w.unmount()
  })

  it('zeigt Leerzustand, Ladefehler und markiert Erledigtes, mehrfach und unbekannte Schlüssel neutral', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    server.items = []
    let w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    expect(w.find('.empty-state').text()).toContain('Keine Nachrichten')
    w.unmount()

    mockApi(); server.failList = true
    w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    expect(w.find('.notif-state[role="alert"]').text()).toContain('konnten nicht geladen werden')
    w.unmount()

    mockApi()
    server.items = [
      note({ id: 5, resolved_at: '2026-10-30T11:00:00Z', read: true, severity: 'info' }),
      note({ id: 6, title_key: 'adminNotifications.neu.title', body_key: 'adminNotifications.neu.body', severity: 'merkwuerdig' }),
    ]
    w = await mountBell()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    const text = w.find('[role="dialog"]').text()
    expect(text).toContain('Erledigt')
    expect(text).toContain('Neue Nachricht')
    expect(text).not.toContain('adminNotifications.neu')
    expect(text).toContain('Info')
    w.unmount()
  })

  it('zeigt englische Texte in der englischen Oberfläche', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    i18n.global.locale.value = 'en'
    const w = await mountBell()
    expect(w.find('.notif-button').attributes('aria-label')).toBe('Messages, 3 unread')
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    expect(w.find('[role="dialog"]').text()).toContain('External source unreachable')
    w.unmount()
    i18n.global.locale.value = 'de'
  })

  it('führt systemweite Nachrichten für Global Admins mit den Seiten-Nachrichten zusammen, neueste zuerst', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    global.fetch = vi.fn(async (url) => ({
      ok: true, status: 200,
      json: async () => {
        if (url.includes('/unread-count')) return { unread_count: 1 }
        return url.startsWith('/api/admin-notifications')
          ? { items: [note({ id: 9, last_seen_at: '2026-10-30T12:00:00Z', title_key: 'adminNotifications.webhook_rejected.title', body_key: 'adminNotifications.webhook_rejected.body', params: { channel: 'google_chat', subscriber_id: 7 } })] }
          : { items: [note({ id: 9, last_seen_at: '2026-10-30T09:00:00Z' })] }
      },
    }))
    const w = await mountBell()
    expect(w.find('.notif-badge').text()).toBe('2')
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    const titles = w.findAll('.notif-item-title').map(t => t.text())
    expect(titles).toEqual(['Webhook wird abgelehnt', 'Externe Quelle nicht erreichbar'])
    expect(w.text()).toContain('Abonnent 7')
    w.unmount()
  })

  describe('Polling', () => {
    it('fragt alle 60 s den Zähler ab, aber nur bei sichtbarem Tab', async () => {
      vi.useFakeTimers()
      useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
      const w = await mountBell()
      const counts = () => server.calls.filter(c => c.includes('unread-count')).length
      const start = counts()
      await vi.advanceTimersByTimeAsync(60_000)
      expect(counts()).toBe(start + 2) // Seite + systemweit
      Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(180_000)
      expect(counts()).toBe(start + 2)
      Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(0)
      expect(counts()).toBe(start + 4) // sofort beim Sichtbarwerden
      await vi.advanceTimersByTimeAsync(60_000)
      expect(counts()).toBe(start + 6)
      w.unmount()
      await vi.advanceTimersByTimeAsync(120_000)
      expect(counts()).toBe(start + 6)
    })

    it('startet das Polling nicht bei unsichtbarem Tab', async () => {
      vi.useFakeTimers()
      Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
      useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
      const w = await mountBell()
      const start = server.calls.length
      await vi.advanceTimersByTimeAsync(120_000)
      expect(server.calls.length).toBe(start)
      w.unmount()
    })

    it('ein Fehler beim Polling lässt den letzten Stand stehen', async () => {
      vi.useFakeTimers()
      useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
      const w = await mountBell()
      global.fetch = vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) }))
      await vi.advanceTimersByTimeAsync(60_000)
      expect(w.find('.notif-badge').text()).toBe('3')
      w.unmount()
    })
  })

  describe('Mandantenwechsel', () => {
    it('holt den Zähler sofort neu und lädt eine offene Liste der neuen Seite', async () => {
      useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
      const w = await mountBell()
      await w.find('.notif-button').trigger('click')
      await flushPromises()
      server.calls.length = 0
      useTenant().setActiveTenant(2)
      await flushPromises()
      expect(server.calls).toContain('GET /api/pages/2/admin-notifications/unread-count')
      expect(server.calls).toContain('GET /api/pages/2/admin-notifications?limit=20')
      w.unmount()
    })

    it('verwirft Antworten der alten Seite nach dem Wechsel (Zähler und Liste)', async () => {
      useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: false })
      setAuthAsPageRole('t', { email: 'a@x.de', is_global_admin: false, permissions: 'external_sources:update' })
      useTenant().setMyPages([{ ID: 1, Permissions: 'external_sources:update' }, { ID: 2, Permissions: 'external_sources:update' }])
      const pending = {}
      global.fetch = vi.fn((url) => new Promise((resolve) => {
        pending[url] = () => resolve({
          ok: true, status: 200,
          json: async () => (url.includes('/unread-count')
            ? { unread_count: url.includes('/pages/1/') ? 7 : 1 }
            : { items: [note({ id: url.includes('/pages/1/') ? 1 : 2 })] }),
        })
      }))
      const w = mount(AdminNotificationBell, { global: { plugins: [i18n, router()] }, attachTo: document.body })
      await flushPromises()
      await w.find('.notif-button').trigger('click')
      useTenant().setActiveTenant(2)
      await flushPromises()
      // neue Seite antwortet zuerst, die alte erst danach
      Object.keys(pending).filter(u => u.includes('/pages/2/')).forEach(u => pending[u]())
      await flushPromises()
      Object.keys(pending).filter(u => u.includes('/pages/1/')).forEach(u => pending[u]())
      await flushPromises()
      expect(w.find('.notif-badge').text()).toBe('1')
      expect(w.findAll('.notif-item').length).toBe(1)
      w.unmount()
    })
  })

  it('hat keine axe-Funde, geschlossen und geöffnet', async () => {
    useAuth().setAuth('t', { email: 'a@x.de', is_global_admin: true })
    server.items = [note(), note({ id: 2, severity: 'warning', count: 3 }), note({ id: 3, severity: 'info', resolved_at: '2026-10-30T11:00:00Z', read: true })]
    const w = await mountBell()
    expect(await axe(w)).toHaveNoViolations()
    await w.find('.notif-button').trigger('click')
    await flushPromises()
    expect(await axe(w)).toHaveNoViolations()
    w.unmount()
  })
})
