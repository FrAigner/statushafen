import { describe, it, expect } from 'vitest'
import { RESOLVED_INCIDENT_STATUSES, isResolvedStatus, isUpcomingMaintenance, incidentStatusMeta } from './incidentStatus'

// 4a: Vorfall-Status -> Form/Farbe und Text der Statusseite.
describe('incidentStatusMeta', () => {
  it.each([
    ['investigating', 'major', 'incidentDetail.statusInvestigating'],
    ['identified', 'partial', 'incidentDetail.statusIdentified'],
    ['monitoring', 'degraded', 'incidentDetail.statusMonitoring'],
    ['resolved', 'operational', 'incidentDetail.statusResolved'],
    ['scheduled', 'maintenance', 'statusPage.statusScheduled'],
    ['in_progress', 'maintenance', 'statusPage.statusInProgress'],
  ])('%s -> %s', (status, level, labelKey) => {
    expect(incidentStatusMeta(status)).toEqual({ level, labelKey })
  })

  it('behandelt den migrierten Altwert completed wie Unbekanntes', () => {
    expect(incidentStatusMeta('completed')).toEqual({ level: null, labelKey: null })
  })

  it('lässt Unbekanntes neutral (keine Statusfarbe) und ignoriert Prototyp-Schlüssel', () => {
    expect(incidentStatusMeta('foo')).toEqual({ level: null, labelKey: null })
    expect(incidentStatusMeta(undefined)).toEqual({ level: null, labelKey: null })
    expect(incidentStatusMeta('toString')).toEqual({ level: null, labelKey: null })
  })
})

// die zentrale Regel „welcher Vorfall-Status gilt als behoben“.
describe('incidentStatus', () => {
  it.each([
    ['resolved', true],
    ['completed', false], // Altwert, seit der Umstellung migriert
    ['investigating', false],
    ['identified', false],
    ['monitoring', false],
    ['in_progress', false],
    ['scheduled', false],
    ['foo_bar', false],
    ['', false],
    [undefined, false],
  ])('isResolvedStatus(%j) = %s', (status, expected) => {
    expect(isResolvedStatus(status)).toBe(expected)
  })

  it('lässt die Liste der behobenen Status nicht verändern', () => {
    expect(Object.isFrozen(RESOLVED_INCIDENT_STATUSES)).toBe(true)
  })

  it.each([
    [{ IsMaintenance: true, Status: 'scheduled' }, true],
    [{ IsMaintenance: true, Status: 'in_progress' }, false],
    [{ IsMaintenance: true, Status: 'resolved' }, false],
    [{ IsMaintenance: false, Status: 'scheduled' }, false],
  ])('isUpcomingMaintenance(%o) = %s', (incident, expected) => {
    expect(isUpcomingMaintenance(incident)).toBe(expected)
  })
})
