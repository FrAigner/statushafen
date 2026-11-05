import { describe, it, expect } from 'vitest'
import {
  MATRIX_ACTIONS,
  ACTION_ABBREVIATION,
  isOn,
  setCell,
  setColumn,
  setRow,
  stateOf,
  rowState,
  columnState,
  matrixState,
  setStandalone,
  summarizePermissions,
} from './permissionMatrix'

describe('permissionMatrix', () => {
  it('kennt die vier Aktionen in der Reihenfolge C R U D', () => {
    expect(MATRIX_ACTIONS).toEqual(['create', 'read', 'update', 'delete'])
    expect(MATRIX_ACTIONS.map(a => ACTION_ABBREVIATION[a]).join('')).toBe('CRUD')
  })

  it('zeigt Lesen auch an, wenn nur Anlegen, Ändern oder Löschen gesetzt ist', () => {
    for (const act of ['create', 'update', 'delete']) {
      expect(isOn([`notices:${act}`], 'notices', 'read')).toBe(true)
      expect(isOn([`notices:${act}`], 'components', 'read')).toBe(false)
    }
    expect(isOn(['notices:read'], 'notices', 'update')).toBe(false)
  })

  it('setzt Lesen mit, wenn Anlegen, Ändern oder Löschen gesetzt wird', () => {
    for (const act of ['create', 'update', 'delete']) {
      expect(setCell([], 'notices', act, true)).toEqual([`notices:${act}`, 'notices:read'])
    }
    // Lesen selbst setzt nur sich selbst; doppelte Einträge entstehen nicht.
    expect(setCell([], 'notices', 'read', true)).toEqual(['notices:read'])
    expect(setCell(['notices:read'], 'notices', 'update', true)).toEqual(['notices:read', 'notices:update'])
    expect(setCell(['notices:update'], 'notices', 'update', true)).toEqual(['notices:update', 'notices:read'])
  })

  it('entzieht mit Lesen auch Anlegen, Ändern und Löschen derselben Ressource', () => {
    const all = setRow([], 'notices', true)
    expect(all).toEqual(['notices:create', 'notices:read', 'notices:update', 'notices:delete'])
    expect(setCell([...all, 'components:read'], 'notices', 'read', false)).toEqual(['components:read'])
    // Ein anderes Recht entziehen lässt Lesen stehen.
    expect(setCell(all, 'notices', 'delete', false)).toEqual(['notices:create', 'notices:read', 'notices:update'])
  })

  it('lässt fremde Einträge unangetastet', () => {
    const model = ['admin_dashboard', 'beta:feature', 'notices:update', 'notices:read']
    expect(setRow(model, 'notices', false)).toEqual(['admin_dashboard', 'beta:feature'])
    expect(setCell(model, 'components', 'create', true)).toEqual([...model, 'components:create', 'components:read'])
  })

  it('setzt Spalten und Zeilen für mehrere Ressourcen', () => {
    const resources = ['a', 'b']
    const onCol = setColumn([], resources, 'update', true)
    expect(onCol).toEqual(['a:update', 'a:read', 'b:update', 'b:read'])
    expect(columnState(onCol, resources, 'update')).toBe('all')
    expect(columnState(onCol, resources, 'delete')).toBe('none')
    expect(columnState(['a:update'], resources, 'update')).toBe('some')
    expect(setColumn(onCol, resources, 'update', false)).toEqual(['a:read', 'b:read'])
  })

  it('bestimmt den Zustand einer Zeile, Spalte und der ganzen Matrix', () => {
    expect(stateOf([])).toBe('none')
    expect(stateOf([true, false])).toBe('some')
    expect(stateOf([true, true])).toBe('all')
    expect(rowState([], 'a')).toBe('none')
    expect(rowState(['a:read'], 'a')).toBe('some')
    expect(rowState(setRow([], 'a', true), 'a')).toBe('all')
    expect(matrixState(setRow([], 'a', true), ['a', 'b'])).toBe('some')
    expect(matrixState(setRow(setRow([], 'a', true), 'b', true), ['a', 'b'])).toBe('all')
  })

  it('schaltet Sonderrechte einzeln', () => {
    expect(setStandalone([], 'admin_dashboard', true)).toEqual(['admin_dashboard'])
    expect(setStandalone(['admin_dashboard'], 'admin_dashboard', true)).toEqual(['admin_dashboard'])
    expect(setStandalone(['admin_dashboard', 'x:read'], 'admin_dashboard', false)).toEqual(['x:read'])
  })

  it('fasst eine Rolle als Buchstaben je Ressource zusammen', () => {
    const { rows, special } = summarizePermissions(
      'maintenance:update,components:create,components:delete,notices:read,admin_dashboard,foo:bar',
      ['components', 'maintenance', 'notices', 'sso'])
    expect(rows).toEqual([
      { resource: 'components', actions: ['create', 'read', 'delete'], letters: 'CRD' },
      { resource: 'maintenance', actions: ['read', 'update'], letters: 'RU' },
      { resource: 'notices', actions: ['read'], letters: 'R' },
    ])
    expect(special).toEqual(['admin_dashboard', 'foo:bar'])
  })

  it('zählt den Altwert :write in der Zusammenfassung als CRUD', () => {
    const { rows, special } = summarizePermissions('components:write', ['components'])
    expect(rows[0].letters).toBe('CRUD')
    expect(special).toEqual([])
    expect(summarizePermissions('', ['components'])).toEqual({ rows: [], special: [] })
  })
})
