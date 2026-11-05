import { describe, it, expect, afterEach } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import i18n from '../i18n'
import AvailabilityBars from './AvailabilityBars.vue'
import { axe } from '../test-support/axe.js'

enableAutoUnmount(afterEach)

const day = (date, status, availability = 100) => ({ date, status, availability, downtime_seconds: 0 })
const withImpairments = (d, impairments) => ({ ...d, impairments })
const days = [
  day('2026-10-26', 'no_data', null),
  day('2026-10-27', 'operational'),
  day('2026-10-28', 'degraded_performance', 98.5),
  day('2026-10-29', 'under_maintenance', 100),
]

const mountBars = (props = {}) => {
  i18n.global.locale.value = 'de'
  return mount(AvailabilityBars, { props: { name: 'Gesamtstatus', days, uptimePercent: 99.84, ...props }, global: { plugins: [i18n] } })
}

// 4a: 90-Tage-Balken aus dem Availability-Endpunkt.
describe('AvailabilityBars', () => {
  it('zeichnet einen Balken je Tag, ältester links, mit Statusklasse', () => {
    const w = mountBars()
    const bars = w.findAll('.avail__day')
    expect(bars).toHaveLength(4)
    expect(bars.map(b => b.classes().find(c => c.startsWith('avail__day--')))).toEqual([
      'avail__day--none', 'avail__day--ok', 'avail__day--deg', 'avail__day--maint',
    ])
  })

  it('zeigt no_data neutral und ohne Prozentzahl, nie als Betrieb', () => {
    const w = mountBars()
    const label = w.findAll('.avail__day')[0].attributes('aria-label')
    expect(label).toContain('Keine Daten')
    expect(label).not.toContain('%')
    expect(label).not.toContain('Betriebsbereit')
  })

  it('nennt Status und Prozent nur bei Tagen mit Einschränkung', () => {
    const bars = mountBars().findAll('.avail__day')
    expect(bars[1].attributes('aria-label')).toMatch(/: Betriebsbereit$/)
    expect(bars[2].attributes('aria-label')).toMatch(/: Beeinträchtigt, 98,50\s%\sverfügbar$/)
    expect(bars[3].attributes('aria-label')).toMatch(/: Wartung$/)
  })

  it('macht nur Tage mit Beeinträchtigung oder Wartung fokussierbar', () => {
    const bars = mountBars().findAll('.avail__day')
    expect(bars.map(b => b.attributes('tabindex'))).toEqual(['-1', '-1', '0', '0'])
  })

  it('zeigt die Gesamtzahl und benennt die Gruppe mit Name und Prozent', () => {
    const w = mountBars()
    expect(w.find('.avail__percent').text()).toMatch(/^99,84\s%\sverfügbar$/)
    expect(w.find('[role="group"]').attributes('aria-label')).toMatch(/^Gesamtstatus: 99,84\s%\sverfügbar in den letzten 90 Tagen$/)
  })

  it('zeigt bei uptimePercent null keine Prozentzahl und sagt „keine Daten“', () => {
    const w = mountBars({ uptimePercent: null })
    expect(w.find('.avail__percent').exists()).toBe(false)
    expect(w.find('.avail__nodata').text()).toBe('Keine Daten')
    expect(w.text()).not.toContain('0,00')
    expect(w.find('[role="group"]').attributes('aria-label')).toBe('Gesamtstatus: noch keine Daten für die letzten 90 Tage')
  })

  it('behandelt einen unbekannten Tagesstatus wie no_data', () => {
    const w = mountBars({ days: [day('2026-10-29', 'unbekannt', null)] })
    expect(w.find('.avail__day').classes()).toContain('avail__day--none')
  })

  it('zeigt die Zeitachse, im kompakten Modus nicht', () => {
    expect(mountBars().find('.avail__axis').text()).toContain('Heute')
    const compact = mountBars({ compact: true })
    expect(compact.find('.avail__axis').exists()).toBe(false)
    expect(compact.classes()).toContain('avail--compact')
  })

  it('übersetzt ins Englische', () => {
    const w = mountBars()
    i18n.global.locale.value = 'en'
    return w.vm.$nextTick().then(() => {
      expect(w.find('.avail__axis').text()).toContain('Today')
      expect(w.findAll('.avail__day')[2].attributes('aria-label')).toContain('98.50')
      i18n.global.locale.value = 'de'
    })
  })

  it('hat keine axe-Funde', async () => {
    expect(await axe(mountBars())).toHaveNoViolations()
  })

  // Schweregrad im Verlauf (Backend gewichtet, Tagesfarbe = schlimmster Status).
  describe('Schweregrad', () => {
    const sev = [
      withImpairments(day('2026-10-26', 'degraded_performance', 92.5), [{ status: 'degraded_performance', seconds: 21600 }]),
      withImpairments(day('2026-10-27', 'partial_outage', 85), [{ status: 'partial_outage', seconds: 9000 }, { status: 'degraded_performance', seconds: 1800 }]),
      withImpairments(day('2026-10-28', 'major_outage', 50), [{ status: 'major_outage', seconds: 43200 }]),
    ]

    it('färbt Teilausfall und Großstörung eigen und macht sie fokussierbar', () => {
      const bars = mountBars({ days: sev }).findAll('.avail__day')
      expect(bars.map(b => b.classes().find(c => c.startsWith('avail__day--')))).toEqual([
        'avail__day--deg', 'avail__day--part', 'avail__day--major',
      ])
      expect(bars.map(b => b.attributes('tabindex'))).toEqual(['0', '0', '0'])
    })

    it('nennt im Tooltip Status, Prozent und die Dauer je Schweregrad', () => {
      const bars = mountBars({ days: sev }).findAll('.avail__day')
      expect(bars[0].attributes('aria-label')).toMatch(/: Beeinträchtigt, 92,50\s%\sverfügbar\. Dauer: Beeinträchtigt 6 Std\. 0 Min\.$/)
      expect(bars[1].attributes('aria-label')).toMatch(/: Teilausfall, 85,00\s%\sverfügbar\. Dauer: Teilausfall 2 Std\. 30 Min\., Beeinträchtigt 30 Min\.$/)
      expect(bars[2].attributes('aria-label')).toMatch(/: Großstörung, 50,00\s%\sverfügbar\. Dauer: Großstörung 12 Std\. 0 Min\.$/)
      // sichtbarer Tooltip und Screenreader-Name sind derselbe Text
      expect(bars[1].find('.avail__tip').text()).toBe(bars[1].attributes('aria-label'))
    })

    it('lässt die Dauer weg, wenn das Backend keine Beeinträchtigungen liefert (alter Stand, Wartung)', () => {
      const bars = mountBars().findAll('.avail__day')
      expect(bars[2].attributes('aria-label')).not.toContain('Dauer')
      expect(bars[3].attributes('aria-label')).not.toContain('Dauer')
    })

    it('übergeht Einträge ohne Dauer und ungültige impairments', () => {
      const bars = mountBars({ days: [
        withImpairments(day('2026-10-26', 'degraded_performance', 99), [{ status: 'degraded_performance', seconds: 0 }, null]),
        withImpairments(day('2026-10-27', 'degraded_performance', 99), 'kaputt'),
      ] }).findAll('.avail__day')
      for (const b of bars) expect(b.attributes('aria-label')).not.toContain('Dauer')
    })

    it('zeigt Dauer unter einer Minute lesbar', () => {
      const bars = mountBars({ days: [withImpairments(day('2026-10-26', 'degraded_performance', 99.99), [{ status: 'degraded_performance', seconds: 30 }])] }).findAll('.avail__day')
      expect(bars[0].attributes('aria-label')).toContain('Beeinträchtigt unter 1 Min.')
    })

    it('übersetzt Tooltip und Dauer ins Englische', async () => {
      const w = mountBars({ days: sev })
      i18n.global.locale.value = 'en'
      await w.vm.$nextTick()
      expect(w.findAll('.avail__day')[1].attributes('aria-label')).toMatch(/: Partial Outage, 85\.00\s%\savailable\. Duration: Partial Outage 2 h 30 min, Degraded Performance 30 min$/)
      i18n.global.locale.value = 'de'
    })

    it('hält den Tooltip am linken und rechten Rand im Fenster', () => {
      const many = Array.from({ length: 90 }, (_, i) => day(new Date(Date.UTC(2026, 7, 1 + i)).toISOString().slice(0, 10), 'operational'))
      const tips = mountBars({ days: many }).findAll('.avail__tip')
      expect(tips[0].classes()).toContain('avail__tip--start')
      expect(tips[19].classes()).toContain('avail__tip--start')
      expect(tips[20].classes()).not.toContain('avail__tip--start')
      expect(tips[69].classes()).not.toContain('avail__tip--end')
      expect(tips[70].classes()).toContain('avail__tip--end')
      expect(tips[89].classes()).toContain('avail__tip--end')
    })

    it('hat keine axe-Funde mit allen Stufen', async () => {
      expect(await axe(mountBars({ days: [...sev, ...days] }))).toHaveNoViolations()
    })
  })
})
