// Replaces the matrix: permission matrix for roles.
// Rows are the tabs of the navigation (utils/permissions.ts
// RESOURCES), columns the actions C R U D. The model stays an array of
// permission strings (v-model); foreign entries (e.g. admin_dashboard or
// permissions delivered by the backend that are unknown here) are kept while editing.
//
// Rules: create, update and delete include read. Whoever sets one of these
// actions sets read automatically too; whoever revokes read also revokes the
// other three (otherwise a permission would remain that is not usable
// without read).

import { ACTIONS, perm, normalizePermissions } from './permissions'
import type { PermissionAction, PermissionList } from './permissions'

/** State of a group of checkboxes: basis for checked/indeterminate. */
export type MatrixState = 'all' | 'some' | 'none'

export interface PermissionSummaryRow {
  resource: string
  actions: PermissionAction[]
  letters: string
}

export const MATRIX_ACTIONS = ACTIONS
const IMPLIES_READ: string[] = ['create', 'update', 'delete']

// Abbreviations of the column headers.
export const ACTION_ABBREVIATION: Record<PermissionAction, string> = { create: 'C', read: 'R', update: 'U', delete: 'D' }

// Is the action on in the cell? Read also counts if only create,
// update or delete is set (implication).
export function isOn(model: readonly string[], resource: string, action: string): boolean {
  if (model.includes(perm(resource, action))) return true
  return action === 'read' && IMPLIES_READ.some(a => model.includes(perm(resource, a)))
}

function without(model: readonly string[], perms: readonly string[]): string[] {
  return model.filter(p => !perms.includes(p))
}

// Sets or revokes an action of a resource including the consequences for read.
export function setCell(model: readonly string[], resource: string, action: string, enabled: boolean): string[] {
  const target = perm(resource, action)
  if (enabled) {
    const add = [target]
    if (IMPLIES_READ.includes(action)) add.push(perm(resource, 'read'))
    return [...model, ...add.filter(p => !model.includes(p))]
  }
  if (action === 'read') {
    return without(model, ACTIONS.map(a => perm(resource, a)))
  }
  return without(model, [target])
}

// One action for several resources (column toggle).
export function setColumn(model: readonly string[], resources: readonly string[], action: string, enabled: boolean): string[] {
  return resources.reduce<string[]>((acc, resource) => setCell(acc, resource, action, enabled), model as string[])
}

// All actions of a resource (row toggle).
export function setRow(model: readonly string[], resource: string, enabled: boolean): string[] {
  return ACTIONS.reduce<string[]>((acc, action) => setCell(acc, resource, action, enabled), model as string[])
}

// 'all' | 'some' | 'none' – basis for checked/indeterminate of the toggles.
export function stateOf(flags: readonly boolean[]): MatrixState {
  const on = flags.filter(Boolean).length
  if (on === 0) return 'none'
  return on === flags.length ? 'all' : 'some'
}

export const rowState = (model: readonly string[], resource: string): MatrixState => stateOf(ACTIONS.map(a => isOn(model, resource, a)))
export const columnState = (model: readonly string[], resources: readonly string[], action: string): MatrixState => stateOf(resources.map(r => isOn(model, r, action)))
export const matrixState = (model: readonly string[], resources: readonly string[]): MatrixState => stateOf(resources.flatMap(r => ACTIONS.map(a => isOn(model, r, a))))

// Special permissions (e.g. admin_dashboard): individually on/off.
export function setStandalone(model: readonly string[], key: string, enabled: boolean): string[] {
  if (!enabled) return without(model, [key])
  return model.includes(key) ? model as string[] : [...model, key]
}

// Compact representation of a role for the list: per resource the set
// actions as letters ("CRUD", "R", "RU" ...) in the order of the
// navigation, then special permissions. Read is shown even if only create,
// update or delete is granted.
export function summarizePermissions(list: PermissionList, resources: readonly string[]): { rows: PermissionSummaryRow[]; special: string[] } {
  const model = normalizePermissions(list)
  const rows: PermissionSummaryRow[] = []
  for (const resource of resources) {
    const actions = ACTIONS.filter(a => isOn(model, resource, a))
    if (actions.length) {
      rows.push({ resource, actions, letters: actions.map(a => ACTION_ABBREVIATION[a]).join('') })
    }
  }
  const known = new Set(resources.flatMap(r => ACTIONS.map(a => perm(r, a))))
  const special = model.filter(p => !known.has(p))
  return { rows, special }
}
