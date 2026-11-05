import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import i18n from '../i18n'
import { axe } from '../test-support/axe.js'

function mockIsMobile(isMobile) {
  vi.doMock('../composables/useMediaQuery', () => ({
    useIsMobile: () => ref(isMobile),
  }))
}

// Tests assertieren auf den deutschen Wortlaut der übersetzten Default-Texte
// (loadingText/emptyText); ohne gespeicherte Präferenz würde
// detectInitialLocale() in der Testumgebung (happy-dom navigator.language
// === 'en-US') sonst auf 'en' fallen.
beforeEach(() => {
  i18n.global.locale.value = 'de'
})

const columns = [
  { key: 'name', label: 'Name' },
  { key: 'status', label: 'Status' },
]
const items = [
  { id: 1, name: 'Database', status: 'operational' },
  { id: 2, name: 'API', status: 'degraded' },
]

describe('ResponsiveTable', () => {
  describe('Desktop/Tablet (>=768px): Tabellen-Darstellung', () => {
    it('rendert eine Tabelle mit Spalten-Headern', async () => {
      vi.resetModules()
      mockIsMobile(false)
      const { default: ResponsiveTableDesktop } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableDesktop, { global: { plugins: [i18n] }, props: { columns, items, rowKey: 'id' } })

      expect(wrapper.find('table').exists()).toBe(true)
      expect(wrapper.find('.card-grid').exists()).toBe(false)
      expect(wrapper.text()).toContain('Name')
      expect(wrapper.text()).toContain('Status')
    })

    it('rendert eine Tabellenzeile pro Item mit den Werten', async () => {
      vi.resetModules()
      mockIsMobile(false)
      const { default: ResponsiveTableDesktop } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableDesktop, { global: { plugins: [i18n] }, props: { columns, items, rowKey: 'id' } })

      const rows = wrapper.findAll('tbody tr')
      expect(rows).toHaveLength(2)
      expect(rows[0].text()).toContain('Database')
      expect(rows[0].text()).toContain('operational')
      expect(rows[1].text()).toContain('API')
      expect(rows[1].text()).toContain('degraded')
    })

    it('wrapped die Tabelle in einen scrollbaren Container', async () => {
      vi.resetModules()
      mockIsMobile(false)
      const { default: ResponsiveTableDesktop } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableDesktop, { global: { plugins: [i18n] }, props: { columns, items, rowKey: 'id' } })

      expect(wrapper.find('.table-wrapper table').exists()).toBe(true)
    })
  })

  describe('Mobile (<768px): Card-Darstellung', () => {
    it('rendert Cards statt einer Tabelle', async () => {
      vi.resetModules()
      mockIsMobile(true)
      const { default: ResponsiveTableMobile } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableMobile, { global: { plugins: [i18n] }, props: { columns, items, rowKey: 'id' } })

      expect(wrapper.find('.card-grid').exists()).toBe(true)
      expect(wrapper.find('table').exists()).toBe(false)
    })

    it('rendert eine Card pro Item mit Label/Value-Paaren je Spalte', async () => {
      vi.resetModules()
      mockIsMobile(true)
      const { default: ResponsiveTableMobile } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableMobile, { global: { plugins: [i18n] }, props: { columns, items, rowKey: 'id' } })

      const cards = wrapper.findAll('.card')
      expect(cards).toHaveLength(2)

      const firstCardRows = cards[0].findAll('.card-row')
      expect(firstCardRows).toHaveLength(2)
      expect(firstCardRows[0].text()).toContain('Name')
      expect(firstCardRows[0].text()).toContain('Database')
      expect(firstCardRows[1].text()).toContain('Status')
      expect(firstCardRows[1].text()).toContain('operational')
    })
  })

  describe('Leerer Zustand & Ladezustand', () => {
    it('zeigt einen Leer-Hinweis, wenn items leer ist (Desktop)', async () => {
      vi.resetModules()
      mockIsMobile(false)
      const { default: ResponsiveTableDesktop } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableDesktop, { global: { plugins: [i18n] }, props: { columns, items: [], rowKey: 'id' } })

      expect(wrapper.text()).toContain('Keine Einträge gefunden.')
    })

    it('zeigt einen Leer-Hinweis, wenn items leer ist (Mobile)', async () => {
      vi.resetModules()
      mockIsMobile(true)
      const { default: ResponsiveTableMobile } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableMobile, { global: { plugins: [i18n] }, props: { columns, items: [], rowKey: 'id' } })

      expect(wrapper.text()).toContain('Keine Einträge gefunden.')
    })

    it('zeigt einen eigenen emptyText, wenn übergeben', async () => {
      vi.resetModules()
      mockIsMobile(false)
      const { default: ResponsiveTableDesktop } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableDesktop, {
        global: { plugins: [i18n] },
        props: { columns, items: [], rowKey: 'id', emptyText: 'Nichts hier.' },
      })

      expect(wrapper.text()).toContain('Nichts hier.')
    })

    it('zeigt einen Lade-Hinweis, wenn loading true ist, statt Items oder Leer-Hinweis', async () => {
      vi.resetModules()
      mockIsMobile(false)
      const { default: ResponsiveTableDesktop } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableDesktop, {
        global: { plugins: [i18n] },
        props: { columns, items: [], rowKey: 'id', loading: true },
      })

      expect(wrapper.text()).toContain('Lädt…')
      expect(wrapper.text()).not.toContain('Keine Einträge gefunden.')
    })
  })

  describe('Custom Cell-Slots', () => {
    it('nutzt einen benannten cell-<key>-Slot statt des Rohwerts, wenn vorhanden (Desktop)', async () => {
      vi.resetModules()
      mockIsMobile(false)
      const { default: ResponsiveTableDesktop } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableDesktop, {
        global: { plugins: [i18n] },
        props: { columns, items, rowKey: 'id' },
        slots: {
          'cell-status': '<template #cell-status="{ item }"><span class="badge">{{ item.status.toUpperCase() }}</span></template>',
        },
      })

      expect(wrapper.find('.badge').exists()).toBe(true)
      expect(wrapper.text()).toContain('OPERATIONAL')
    })

    it('nutzt einen benannten cell-<key>-Slot statt des Rohwerts, wenn vorhanden (Mobile)', async () => {
      vi.resetModules()
      mockIsMobile(true)
      const { default: ResponsiveTableMobile } = await import('./ResponsiveTable.vue')
      const wrapper = mount(ResponsiveTableMobile, {
        global: { plugins: [i18n] },
        props: { columns, items, rowKey: 'id' },
        slots: {
          'cell-status': '<template #cell-status="{ item }"><span class="badge">{{ item.status.toUpperCase() }}</span></template>',
        },
      })

      expect(wrapper.find('.badge').exists()).toBe(true)
      expect(wrapper.text()).toContain('OPERATIONAL')
    })
  })

  it('hat keine axe-Funde als Tabelle und als Cards', async () => {
    for (const isMobile of [false, true]) {
      vi.resetModules()
      mockIsMobile(isMobile)
      const { default: Table } = await import('./ResponsiveTable.vue')
      const wrapper = mount(Table, { global: { plugins: [i18n] }, props: { columns, items, rowKey: 'id' } })
      expect(await axe(wrapper)).toHaveNoViolations()
    }
  })
})
