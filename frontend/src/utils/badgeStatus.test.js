import { describe, it, expect } from 'vitest'
import { incidentBadge, maintenanceBadge, noticeBadge, actionBadge } from './badgeStatus'

// Der Übersetzer gibt den Schlüssel zurück: geprüft wird die Zuordnung.
const t = key => `t:${key}`

describe('badgeStatus (Stufe 4b)', () => {
  it.each([
    ['investigating', 'major', 'incidentDetail.statusInvestigating'],
    ['identified', 'partial', 'incidentDetail.statusIdentified'],
    ['monitoring', 'degraded', 'incidentDetail.statusMonitoring'],
    ['resolved', 'operational', 'incidentDetail.statusResolved'],
  ])('Vorfall %s -> %s', (value, status, key) => {
    expect(incidentBadge(value, t)).toEqual({ status, label: `t:${key}` })
  })

  it.each([
    ['scheduled', 'maintenance', 'maintenance.statusScheduled'],
    ['in_progress', 'degraded', 'maintenance.statusInProgress'],
    ['resolved', 'operational', 'maintenance.statusResolved'],
  ])('Wartung %s -> %s', (value, status, key) => {
    expect(maintenanceBadge(value, t)).toEqual({ status, label: `t:${key}` })
  })

  it.each([
    ['info', 'maintenance', 'notices.levelInfo'],
    ['warning', 'degraded', 'notices.levelWarning'],
    ['critical', 'major', 'notices.levelCritical'],
  ])('Ankündigung %s -> %s', (value, status, key) => {
    expect(noticeBadge(value, t)).toEqual({ status, label: `t:${key}` })
  })

  it.each([
    ['done', 'operational', 'incidentDetail.actionDone'],
    ['in_progress', 'degraded', 'incidentDetail.actionInProgress'],
    ['open', null, 'incidentDetail.actionOpen'],
  ])('Folgemaßnahme %s -> %s', (value, status, key) => {
    expect(actionBadge(value, t)).toEqual({ status, label: `t:${key}` })
  })

  it('zeigt unbekannte Werte neutral mit dem Rohwert, nie mit Statusfarbe', () => {
    expect(incidentBadge('seltsam', t)).toEqual({ status: null, label: 'seltsam' })
    expect(noticeBadge(undefined, t)).toEqual({ status: null, label: '' })
    // Prototyp-Schlüssel sind keine Status.
    expect(maintenanceBadge('constructor', t)).toEqual({ status: null, label: 'constructor' })
  })
})
