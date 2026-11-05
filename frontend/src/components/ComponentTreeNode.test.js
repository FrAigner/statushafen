import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import ComponentTreeNode from './ComponentTreeNode.vue'
import StatusBadge from './StatusBadge.vue'
import { axe } from '../test-support/axe.js'

function mountNode(status, extra = {}, depth = 0) {
  return mount(ComponentTreeNode, {
    props: { node: { ID: 1, Name: 'API', Status: status, Children: [], ...extra }, depth },
    global: { plugins: [i18n] },
  })
}

const group = (status = 'operational') => ({
  Status: status,
  Children: [{ ID: 2, Name: 'CDN', Status: 'operational', Children: [] }],
})

describe('ComponentTreeNode', () => {
  it('rendert ein übersetztes StatusBadge statt eines rohen i18n-Schlüssels', () => {
    i18n.global.locale.value = 'de'
    const wrapper = mountNode('operational')

    const badge = wrapper.findComponent(StatusBadge)
    expect(badge.text()).toBe('Betriebsbereit')
    expect(badge.text()).not.toContain('status.operational')
  })

  it('übersetzt alle vier Status-Werte korrekt und gibt sie an das Badge', () => {
    i18n.global.locale.value = 'de'
    const expectations = {
      operational: 'Betriebsbereit',
      degraded_performance: 'Beeinträchtigt',
      partial_outage: 'Teilausfall',
      major_outage: 'Großstörung',
    }
    for (const [status, label] of Object.entries(expectations)) {
      const badge = mountNode(status).findComponent(StatusBadge)
      expect(badge.props('status')).toBe(status)
      expect(badge.text()).toBe(label)
    }
  })

  it('zeigt Status mit Form (Symbol), nicht nur mit Farbe, und ohne alte badge-Klassen', () => {
    const wrapper = mountNode('major_outage')
    expect(wrapper.find('svg.status-icon[data-shape="octagon-cross"]').exists()).toBe(true)
    expect(wrapper.find('.badge').exists()).toBe(false)
    expect(wrapper.find('.tree-dot').exists()).toBe(false)
  })

  it('zeigt einen unbekannten Status neutral als Rohwert', () => {
    const wrapper = mountNode('weird_status')
    expect(wrapper.findComponent(StatusBadge).text()).toBe('weird_status')
  })

  it('rückt per Klasse ein statt per Inline-Style und deckelt die Tiefe', () => {
    expect(mountNode('operational', {}, 0).find('.tree-row').classes()).toContain('tree-d0')
    expect(mountNode('operational', {}, 3).find('.tree-row').classes()).toContain('tree-d3')
    const deep = mountNode('operational', {}, 9)
    expect(deep.find('.tree-row').classes()).toContain('tree-d5')
    expect(deep.html()).not.toContain('style=')
  })

  it('zeigt Beschreibung und Tags, den reservierten Tag „global“ aber nicht', () => {
    const wrapper = mountNode('operational', {
      Description: 'Hauptschnittstelle',
      Tags: [{ Name: 'DE', StatusPageID: 1 }, { Name: 'Global', StatusPageID: null }],
    })
    expect(wrapper.find('.tree-desc').text()).toBe('Hauptschnittstelle')
    expect(wrapper.find('.meta-tag').text()).toBe('DE')
    expect(mountNode('operational', { Tags: [{ Name: 'global', StatusPageID: null }] }).find('.meta-tag').exists()).toBe(false)
    expect(mountNode('operational').find('.tree-desc').exists()).toBe(false)
  })

  describe('Gruppen', () => {
    it('sind bei Störung aufgeklappt, bei Betrieb zugeklappt', () => {
      expect(mount(ComponentTreeNode, { props: { node: { ID: 1, Name: 'G', ...group('operational') } }, global: { plugins: [i18n] } })
        .find('.tree-children').exists()).toBe(false)
      expect(mount(ComponentTreeNode, { props: { node: { ID: 1, Name: 'G', ...group('degraded_performance') } }, global: { plugins: [i18n] } })
        .find('.tree-children').exists()).toBe(true)
    })

    it('lassen sich per Schaltfläche (Tastatur) auf- und zuklappen und melden aria-expanded', async () => {
      const wrapper = mount(ComponentTreeNode, { props: { node: { ID: 1, Name: 'Gruppe', ...group('operational') } }, global: { plugins: [i18n] } })
      const toggle = wrapper.find('button.tree-toggle')
      expect(toggle.attributes('aria-label')).toBe('Gruppe')
      expect(toggle.attributes('aria-expanded')).toBe('false')
      await toggle.trigger('click')
      expect(toggle.attributes('aria-expanded')).toBe('true')
      expect(wrapper.find('.tree-children').exists()).toBe(true)
      await toggle.trigger('click')
      expect(toggle.attributes('aria-expanded')).toBe('false')
    })

    it('klappen auch beim Klick auf die Zeile um', async () => {
      const wrapper = mount(ComponentTreeNode, { props: { node: { ID: 1, Name: 'Gruppe', ...group('operational') } }, global: { plugins: [i18n] } })
      await wrapper.find('.tree-row').trigger('click')
      expect(wrapper.find('.tree-children').exists()).toBe(true)
    })

    it('zeigen ohne Kinder keine Schaltfläche, ein Klick auf die Zeile bleibt wirkungslos', async () => {
      const wrapper = mountNode('operational')
      expect(wrapper.find('button.tree-toggle').exists()).toBe(false)
      expect(wrapper.find('.tree-toggle--spacer').attributes('aria-hidden')).toBe('true')
      await wrapper.find('.tree-row').trigger('click')
      expect(wrapper.find('.tree-children').exists()).toBe(false)
    })
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = mountNode('degraded_performance')
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  const ext = (over = {}) => ({ external_component_id: 1, source_id: 2, source_name: 'Example Cloud', name: 'VM', status: 'major_outage', url: 'https://status.example.com', vanished: false, ...over })

  it('kennzeichnet gekoppelte externe Quellen mit „extern“ und Link', async () => {
    i18n.global.locale.value = 'de'
    const wrapper = mountNode('major_outage', { external_sources: [ext()] })
    const note = wrapper.find('.tree-ext')
    expect(note.exists()).toBe(true)
    expect(note.text()).toContain('extern')
    expect(note.find('a').attributes('href')).toBe('https://status.example.com/')
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('zeigt ohne Kopplung oder mit verschwundenem Baustein keine Kennzeichnung', () => {
    expect(mountNode('operational').find('.tree-ext').exists()).toBe(false)
    expect(mountNode('operational', { external_sources: [ext({ vanished: true })] }).find('.tree-ext').exists()).toBe(false)
  })

  it('hat auch als aufgeklappte Gruppe keine axe-Funde', async () => {
    const wrapper = mount(ComponentTreeNode, { props: { node: { ID: 1, Name: 'Gruppe', ...group('major_outage') } }, global: { plugins: [i18n] } })
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
