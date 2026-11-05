import { describe, it, expect } from 'vitest'
import { incidentListUrl, mergeIncidents, PUBLIC_PAGE_SIZE, ADMIN_PAGE_SIZE } from './incidentPaging'

describe('incidentListUrl', () => {
  it('baut die URL ohne und mit Parametern', () => {
    expect(incidentListUrl(3)).toBe('/api/pages/3/incidents')
    expect(incidentListUrl(3, { limit: ADMIN_PAGE_SIZE })).toBe('/api/pages/3/incidents?limit=200')
    expect(incidentListUrl(3, { maintenance: true, limit: 200, offset: 400 })).toBe('/api/pages/3/incidents?is_maintenance=true&limit=200&offset=400')
    expect(incidentListUrl(3, { maintenance: 'any', state: 'resolved', limit: PUBLIC_PAGE_SIZE, offset: 50 }))
      .toBe('/api/pages/3/incidents?is_maintenance=any&state=resolved&limit=50&offset=50')
    expect(incidentListUrl(3, { maintenance: false })).toBe('/api/pages/3/incidents?is_maintenance=false')
  })

  it('lässt offset 0 weg', () => {
    expect(incidentListUrl(1, { limit: 50, offset: 0 })).toBe('/api/pages/1/incidents?limit=50')
  })
})

describe('mergeIncidents', () => {
  it('hängt neue an und behält die Reihenfolge', () => {
    expect(mergeIncidents([{ ID: 1 }, { ID: 2 }], [{ ID: 3 }])).toEqual([{ ID: 1 }, { ID: 2 }, { ID: 3 }])
  })

  it('ignoriert schon vorhandene IDs und liefert dann dieselbe Liste', () => {
    const base = [{ ID: 1 }, { ID: 2 }]
    expect(mergeIncidents(base, [{ ID: 2 }, { ID: 1 }])).toBe(base)
    expect(mergeIncidents(base, [{ ID: 2 }, { ID: 4 }])).toEqual([{ ID: 1 }, { ID: 2 }, { ID: 4 }])
  })
})
