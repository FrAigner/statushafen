import { describe, it, expect, afterEach } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import i18n from '../i18n'
import ExternalSourceNote from './ExternalSourceNote.vue'
import StatusBadge from './StatusBadge.vue'
import { axe } from '../test-support/axe.js'

enableAutoUnmount(afterEach)

const ref = (extra = {}) => ({
  external_component_id: 5, source_id: 2, source_name: 'Example Cloud', name: 'Service 03', group_name: 'Region A',
  status: 'partial_outage', url: 'https://status.example.com/c/vm', vanished: false, is_group: false, source_outage: false, ...extra,
})

const mountNote = (sources) => mount(ExternalSourceNote, { props: { sources, name: 'API' }, global: { plugins: [i18n] } })

describe('ExternalSourceNote', () => {
  it('kennzeichnet als „extern“ mit Text, Quelle und sicherem Link', async () => {
    i18n.global.locale.value = 'de'
    const w = mountNote([ref()])
    const a = w.find('a')
    expect(a.text()).toContain('extern')
    expect(a.text()).toContain('Example Cloud')
    expect(a.attributes('href')).toBe('https://status.example.com/c/vm')
    expect(a.attributes('target')).toBe('_blank')
    expect(a.attributes('rel')).toBe('noopener noreferrer nofollow')
    expect(a.attributes('aria-label')).toContain('öffnet in neuem Tab')
    expect(a.attributes('title')).toContain('Teilausfall')
    // Status als Text und Form (StatusBadge), nicht nur Farbe
    const badge = w.findComponent(StatusBadge)
    expect(badge.props('status')).toBe('partial_outage')
    expect(w.text()).toContain('Service 03')
    expect(w.text()).toContain('Teilausfall')
    expect(await axe(w)).toHaveNoViolations()
  })

  it.each(['javascript:alert(1)', 'data:text/html,x', '', 'ftp://x.org'])('verlinkt %j nicht', (url) => {
    const w = mountNote([ref({ url })])
    expect(w.find('a').exists()).toBe(false)
    expect(w.text()).toContain('extern')
    expect(w.text()).toContain('Example Cloud')
  })

  it('nennt bei einer Gruppe die schlechteste Komponente', () => {
    i18n.global.locale.value = 'de'
    const w = mountNote([ref({ is_group: true, name: 'Database', status: 'major_outage', worst: { name: 'Service 04', status: 'major_outage' } })])
    expect(w.text()).toContain('Gruppe Database')
    expect(w.text()).toContain('schlechteste Komponente: Service 04')
    expect(w.findComponent(StatusBadge).props('status')).toBe('major_outage')
    expect(w.find('a').attributes('title')).toContain('Service 04')
    expect(w.find('a').attributes('title')).toContain('Großstörung')
  })

  it('zeigt eine Gruppe ohne Worst (alles unbekannt) neutral, ohne erfundene Statusfarbe', () => {
    const w = mountNote([ref({ is_group: true, name: 'Leer', status: 'unknown' })])
    expect(w.findComponent(StatusBadge).exists()).toBe(false)
    expect(w.text()).toContain('Status unbekannt')
  })

  it('zeigt bei ausgefallener Quelle „Daten veraltet“ statt eines Status', () => {
    i18n.global.locale.value = 'de'
    const w = mountNote([ref({ source_outage: true })])
    expect(w.findComponent(StatusBadge).exists()).toBe(false)
    expect(w.text()).toContain('Daten veraltet')
  })

  it('ist zweisprachig', () => {
    i18n.global.locale.value = 'en'
    const w = mountNote([ref()])
    expect(w.text()).toContain('external')
    expect(w.find('a').attributes('aria-label')).toContain('opens in a new tab')
    i18n.global.locale.value = 'de'
  })

  it('rendert Fremdtexte nie als HTML', () => {
    const w = mountNote([ref({ source_name: '<img src=x onerror=alert(1)>', name: '<script>alert(1)</script>' })])
    expect(w.find('img').exists()).toBe(false)
    expect(w.find('script').exists()).toBe(false)
    expect(w.text()).toContain('<script>alert(1)</script>')
  })
})
