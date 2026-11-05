import { describe, it, expect, afterEach } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import i18n from '../i18n'
import ExternalIncidentList from './ExternalIncidentList.vue'
import StatusBadge from './StatusBadge.vue'
import { axe } from '../test-support/axe.js'

enableAutoUnmount(afterEach)

const incident = (extra = {}) => ({
  id: 1, source_id: 2, source_name: 'Example Cloud', source_url: 'https://status.example.com', kind: 'incident',
  external_id: 'fx0001', title: 'Region B API errors', status: 'investigating', impact: 'major', closed: false,
  url: 'https://status.example.com/incidents/rr3j05', started_at: '2026-10-29T10:00:00Z', updated_at: '2026-10-29T10:30:00Z',
  resolved_at: null, scheduled_for: null, scheduled_until: null,
  affected_components: [{ id: 3, name: 'API' }], external_components: [],
  updates: [
    { id: 'a', status: 'investigating', body: 'Wir untersuchen.', display_at: '2026-10-29T10:00:00Z' },
    { id: 'b', status: 'identified', body: 'Ursache gefunden.', display_at: '2026-10-29T10:30:00Z' },
  ],
  ...extra,
})

const mountList = (items) => mount(ExternalIncidentList, { props: { items }, global: { plugins: [i18n] } })

describe('ExternalIncidentList', () => {
  it('kennzeichnet Meldungen als extern mit Quelle, Link und Updates, neueste zuerst', async () => {
    i18n.global.locale.value = 'de'
    const w = mountList([incident()])
    expect(w.text()).toContain('Meldungen externer Quellen')
    expect(w.text()).toContain('extern')
    expect(w.text()).toContain('Quelle: Example Cloud')
    expect(w.text()).toContain('Betrifft: API')
    const a = w.find('a')
    expect(a.attributes('href')).toBe('https://status.example.com/incidents/rr3j05')
    expect(a.attributes('target')).toBe('_blank')
    expect(a.attributes('rel')).toBe('noopener noreferrer nofollow')
    expect(a.text()).toContain('öffnet in neuem Tab')
    const bodies = w.findAll('.ei__body').map((p) => p.text())
    expect(bodies).toEqual(['Ursache gefunden.', 'Wir untersuchen.'])
    const badges = w.findAllComponents(StatusBadge)
    expect(badges[0].text()).toBe('Untersuchung')
    expect(await axe(w)).toHaveNoViolations()
  })

  it('zeigt Wartungen mit geplantem Fenster', () => {
    i18n.global.locale.value = 'de'
    const w = mountList([incident({ kind: 'maintenance', status: 'scheduled', started_at: null, scheduled_for: '2026-11-01T10:00:00Z', scheduled_until: '2026-11-01T12:00:00Z', updates: [] })])
    expect(w.text()).toContain('Wartung')
    expect(w.text()).toContain('Geplant:')
    expect(w.text()).toContain('Keine Updates veröffentlicht.')
  })

  it('zeigt behobene Meldungen mit Behoben-Zeit', () => {
    const w = mountList([incident({ status: 'resolved', closed: true, resolved_at: '2026-10-29T12:00:00Z' })])
    expect(w.text()).toContain('Behoben:')
    expect(w.findAllComponents(StatusBadge)[0].text()).toBe('Behoben')
  })

  it('verträgt fehlende Angaben: unbekannter Status roh, kein Beginn, keine Listen, Wartung ohne Fenster', () => {
    i18n.global.locale.value = 'de'
    const w = mountList([
      incident({ status: 'seltsam', started_at: null, updated_at: null, affected_components: undefined, updates: undefined }),
      incident({ id: 2, kind: 'maintenance', status: 'scheduled', started_at: '2026-10-29T10:00:00Z', scheduled_for: null, updated_at: null }),
      incident({ id: 3, status: 'identified', started_at: '2026-10-29T10:00:00Z', updated_at: null, resolved_at: null, updates: [{ id: 'u', status: 'seltsam', body: 'b', display_at: null }] }),
    ])
    const items = w.findAll('.ei__item')
    expect(items[0].find('.st-badge__text').text()).toBe('seltsam')
    expect(items[0].find('.ei__times').exists()).toBe(false)
    expect(items[0].text()).not.toContain('Betrifft')
    expect(items[0].text()).toContain('Keine Updates veröffentlicht.')
    expect(items[1].text()).toContain('Beginn:')
    expect(items[2].text()).toContain('—')
  })

  it('verlinkt nie javascript:-Adressen, fällt auf die Quellseite zurück oder lässt den Link weg', () => {
    const w1 = mountList([incident({ url: 'javascript:alert(1)' })])
    expect(w1.find('a').attributes('href')).toBe('https://status.example.com/')
    const w2 = mountList([incident({ url: 'javascript:alert(1)', source_url: 'data:text/html,x' })])
    expect(w2.find('a').exists()).toBe(false)
  })

  it('ist zweisprachig', () => {
    i18n.global.locale.value = 'en'
    const w = mountList([incident()])
    expect(w.text()).toContain('Reports from external sources')
    expect(w.text()).toContain('Source: Example Cloud')
    i18n.global.locale.value = 'de'
  })

  it('rendert Titel, Quelle und Update-Texte nie als HTML (Gegenprobe <script>/<img onerror>)', () => {
    const evil = '<script>window.__pwned = 1</script><img src=x onerror="window.__pwned = 2">'
    const w = mountList([incident({
      title: evil, source_name: evil,
      updates: [{ id: 'x', status: 'investigating', body: evil, display_at: '2026-10-29T10:00:00Z' }],
      affected_components: [{ id: 3, name: evil }],
    })])
    expect(w.find('script').exists()).toBe(false)
    expect(w.find('img').exists()).toBe(false)
    expect(w.findAll('img, script, [onerror]')).toHaveLength(0)
    expect(w.find('.ei__body').text()).toBe(evil)
    expect(window.__pwned).toBeUndefined()
  })

  // nur die neuesten Updates in der Karte, der Rest im Verlauf-Dialog.
  it('zeigt höchstens drei Updates und meldet open-history mit der Original-Meldung', async () => {
    i18n.global.locale.value = 'de'
    const updates = [1, 2, 3, 4].map((n) => ({ id: `u${n}`, status: 'identified', body: `U${n}`, display_at: `2026-10-29T1${n}:00:00Z` }))
    const item = incident({ updates })
    const w = mountList([item])
    expect(w.findAll('.ei__body').map((p) => p.text())).toEqual(['U4', 'U3', 'U2'])
    await w.find('.ei__history').trigger('click')
    expect(w.emitted('open-history')).toEqual([[item]])
  })

  it('zeigt ohne Updates keinen Verlauf-Knopf', () => {
    expect(mountList([incident({ updates: [] })]).find('.ei__history').exists()).toBe(false)
  })
})
