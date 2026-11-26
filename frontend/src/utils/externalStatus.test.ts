import { describe, it, expect } from 'vitest'
import { applyEffectiveStatus, safeHttpUrl, visibleExternalSources } from './externalStatus'

describe('applyEffectiveStatus', () => {
  it('setzt den effektiven Status ein, rekursiv, ohne die Eingabe zu verändern', () => {
    const input = [
      { ID: 1, Status: 'group', effective_status: 'major_outage', Children: [
        { ID: 2, Status: 'operational', effective_status: 'major_outage', Children: [] },
        { ID: 3, Status: 'degraded_performance', effective_status: 'degraded_performance' },
        { ID: 4, Status: 'operational' },
      ] },
    ]
    const out = applyEffectiveStatus(input)
    expect(out[0].Status).toBe('group')
    expect(out[0].Children.map((c) => c.Status)).toEqual(['major_outage', 'degraded_performance', 'operational'])
    expect(input[0].Children[0].Status).toBe('operational')
  })

  it('verträgt fehlende Listen', () => {
    expect(applyEffectiveStatus(undefined)).toEqual([])
    expect(applyEffectiveStatus([{ ID: 1, Status: 'operational' }])[0].Status).toBe('operational')
  })
})

describe('safeHttpUrl', () => {
  it('lässt nur http und https durch', () => {
    expect(safeHttpUrl('https://status.example.com/x')).toBe('https://status.example.com/x')
    expect(safeHttpUrl('http://example.org/')).toBe('http://example.org/')
    expect(safeHttpUrl('  https://example.org  ')).toBe('https://example.org/')
  })
  it.each(['javascript:alert(1)', 'JaVaScRiPt:alert(1)', 'data:text/html,<script>', 'ftp://x.org', '//evil.org', 'kein url', '', '   ', null, undefined, 42])(
    'lehnt %s ab', (v) => { expect(safeHttpUrl(v)).toBeNull() })
})

describe('visibleExternalSources', () => {
  it('blendet verschwundene Bausteine aus', () => {
    expect(visibleExternalSources({ external_sources: [{ name: 'a' }, { name: 'b', vanished: true }] })).toEqual([{ name: 'a' }])
    expect(visibleExternalSources({})).toEqual([])
    expect(visibleExternalSources(null)).toEqual([])
  })
})
