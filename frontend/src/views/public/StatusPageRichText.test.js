// jsdom statt happy-dom: Die Bereinigung (DOMPurify) braucht ein vollständiges DOM.
// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, DOMWrapper } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import StatusPage from './StatusPage.vue'

// Die öffentliche Statusseite zeigt formatierte Nachrichten
// (eingeschränktes Markdown), bereinigt, in beiden Layouts und im Verlauf.

const XSS = '<img src=x onerror="alert(1)"><script>alert(2)</script> [böse](javascript:alert(3)) [gut](https://example.com/a)'

function dashboard(layout, extra = {}) {
  return {
    page: { ID: 1, Name: 'Acme', PageLayout: layout, HeaderStyle: 'classic', PrimaryColor: '', SecondaryColor: '' },
    components: [{ ID: 10, Name: 'API', Status: 'operational', Tags: [], Children: [] }],
    incidents: [
      { ID: 100, Title: 'API down', Message: 'Das ist **fett**\nzweite Zeile\n\n- eins\n- zwei', Status: 'investigating', IsMaintenance: false, Tags: [], Components: [{ Name: 'API' }], CreatedAt: '2026-07-01T10:00:00Z' },
      { ID: 101, Title: 'Geplantes Upgrade', Message: 'Wartung mit *Kursiv*', Status: 'scheduled', IsMaintenance: true, ScheduledStartAt: '2099-08-01T10:00:00Z', ScheduledEndAt: '2099-08-01T12:00:00Z', Components: [] },
    ],
    notices: [{ ID: 5, Title: 'Hinweis', Message: 'Bitte **beachten**: [Details](https://example.com/n)', Level: 'info', Collapsed: false, CreatedAt: '2026-07-01T10:00:00Z' }],
    ...extra,
  }
}

const json = (body) => ({ ok: true, status: 200, json: async () => body })
const routed = (dash, updates = []) => vi.fn(async (url) => {
  if (url.includes('/availability')) return { ok: false, status: 404, json: async () => ({}) }
  if (url.includes('/update-translations')) return json({ updates: {} })
  if (url.includes('/incidents/')) return json({ updates })
  return json(dash)
})

let wrapper = null
async function mountPage(dash, updates) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/status/:slug', component: { template: '<div/>' } }] })
  router.push('/status/acme')
  await router.isReady()
  global.fetch = routed(dash, updates)
  wrapper = mount(StatusPage, {
    props: { slug: 'acme' },
    global: {
      plugins: [i18n, router],
      stubs: {
        ComponentTreeNode: { props: ['node'], template: '<div />' },
        SubscribeWizard: true,
        SubscriptionManager: { template: '<div />' },
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('StatusPage zeigt formatierte Texte', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.restoreAllMocks()
  })

  it('Standard-Layout: Vorfall, Wartung und Ankündigung formatiert', async () => {
    const w = await mountPage(dashboard('standard'))
    const incident = w.find('.sp-incident-message')
    expect(incident.find('strong').text()).toBe('fett')
    expect(incident.find('br').exists()).toBe(true)
    expect(incident.findAll('li').map(li => li.text())).toEqual(['eins', 'zwei'])
    expect(w.findAll('.sp-incident-message')[1].find('em').text()).toBe('Kursiv')
    const notice = w.find('.notice-message')
    expect(notice.find('strong').text()).toBe('beachten')
    const link = notice.find('a')
    expect(link.attributes('href')).toBe('https://example.com/n')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noopener noreferrer nofollow')
  })

  it('Layout liste: Zeitleiste zeigt die Nachricht formatiert', async () => {
    const w = await mountPage(dashboard('liste'))
    expect(w.find('.pl-incident-msg strong').text()).toBe('fett')
  })

  it('Update-Nachrichten im Verlauf (Zeitleiste und Dialog) sind formatiert', async () => {
    const updates = [{ id: 1, status: 'monitoring', message: 'Update: **beobachten**', created_at: '2026-07-01T11:00:00Z' }]
    const w = await mountPage(dashboard('standard'), updates)
    expect(w.find('.sp-timeline-message strong').text()).toBe('beobachten')
    await w.find('.incident-history-btn').trigger('click')
    await flushPromises()
    // Der Dialog hängt per Teleport am body.
    const rows = new DOMWrapper(document.body).findAll('.tl-msg')
    expect(rows[0].find('strong').text()).toBe('beobachten')
    // Die Erstmeldung steht ebenfalls formatiert im Verlauf.
    expect(rows.some(m => m.find('li').exists())).toBe(true)
  })

  it('führt kein Markup aus einer Nachricht aus (XSS)', async () => {
    const d = dashboard('standard')
    d.incidents[0].Message = XSS
    d.notices[0].Message = XSS
    const w = await mountPage(d)
    // Als DOM geprüft, nicht als Text: Der maskierte Quelltext ist sichtbar, aber inaktiv.
    expect(w.element.querySelectorAll('script, img, [onerror]')).toHaveLength(0)
    expect(w.element.querySelectorAll('a[href^="javascript"]')).toHaveLength(0)
    expect(w.find('.sp-incident-message').text()).toContain('böse (javascript:alert(3))')
    expect(w.find('.sp-incident-message a').attributes('href')).toBe('https://example.com/a')
  })

  it('Suche findet Text hinter der Formatierung, nicht die Markdown-Zeichen', async () => {
    const w = await mountPage(dashboard('standard'))
    const search = async (text) => {
      await w.find('.search-input').setValue(text)
      await new Promise(resolve => setTimeout(resolve, 350)) // Entprellung der Suche
      await flushPromises()
    }
    await search('fett zweite')
    expect(w.findAll('.sp-incident').some(a => a.text().includes('API down'))).toBe(true)
    await search('**fett**')
    // Die Markdown-Zeichen selbst gehören nicht zum durchsuchten Text.
    expect(w.findAll('.sp-incident').some(a => a.text().includes('API down'))).toBe(false)
  })
})
