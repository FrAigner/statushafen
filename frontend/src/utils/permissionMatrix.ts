// löst die Matrix ab: Berechtigungs-Matrix für
// Rollen. Zeilen sind die Tabs der Navigation (utils/permissions.js
// RESOURCES), Spalten die Aktionen C R U D. Das Modell bleibt ein Array der
// Rechte-Strings (v-model); fremde Einträge (z. B. admin_dashboard oder vom
// Backend gelieferte, hier unbekannte Rechte) bleiben beim Bearbeiten erhalten.
//
// Regeln: Anlegen, Ändern und Löschen schließen Lesen ein. Wer eine dieser
// Aktionen setzt, setzt Lesen automatisch mit; wer Lesen entzieht, entzieht
// auch die drei anderen (sonst bliebe ein Recht stehen, das ohne Lesen nicht
// nutzbar ist).

import { ACTIONS, perm, normalizePermissions } from './permissions'

export const MATRIX_ACTIONS = ACTIONS
const IMPLIES_READ = ['create', 'update', 'delete']

// Kürzel der Spaltenköpfe.
export const ACTION_ABBREVIATION = { create: 'C', read: 'R', update: 'U', delete: 'D' }

// Steht die Aktion in der Zelle an? Lesen gilt auch, wenn nur Anlegen,
// Ändern oder Löschen gesetzt ist (Implikation).
export function isOn(model, resource, action) {
  if (model.includes(perm(resource, action))) return true
  return action === 'read' && IMPLIES_READ.some(a => model.includes(perm(resource, a)))
}

function without(model, perms) {
  return model.filter(p => !perms.includes(p))
}

// Setzt oder entzieht eine Aktion einer Ressource samt Folgen für Lesen.
export function setCell(model, resource, action, enabled) {
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

// Eine Aktion für mehrere Ressourcen (Spaltenschalter).
export function setColumn(model, resources, action, enabled) {
  return resources.reduce((acc, resource) => setCell(acc, resource, action, enabled), model)
}

// Alle Aktionen einer Ressource (Zeilenschalter).
export function setRow(model, resource, enabled) {
  return ACTIONS.reduce((acc, action) => setCell(acc, resource, action, enabled), model)
}

// 'all' | 'some' | 'none' – Grundlage für checked/indeterminate der Schalter.
export function stateOf(flags) {
  const on = flags.filter(Boolean).length
  if (on === 0) return 'none'
  return on === flags.length ? 'all' : 'some'
}

export const rowState = (model, resource) => stateOf(ACTIONS.map(a => isOn(model, resource, a)))
export const columnState = (model, resources, action) => stateOf(resources.map(r => isOn(model, r, action)))
export const matrixState = (model, resources) => stateOf(resources.flatMap(r => ACTIONS.map(a => isOn(model, r, a))))

// Sonderrechte (z. B. admin_dashboard): einzeln an/aus.
export function setStandalone(model, key, enabled) {
  if (!enabled) return without(model, [key])
  return model.includes(key) ? model : [...model, key]
}

// Kompakte Darstellung einer Rolle für die Liste: je Ressource die gesetzten
// Aktionen als Buchstaben ("CRUD", "R", "RU" ...) in Reihenfolge der
// Navigation, danach Sonderrechte. Lesen steht auch dann, wenn nur Anlegen,
// Ändern oder Löschen vergeben ist.
export function summarizePermissions(list, resources) {
  const model = normalizePermissions(list)
  const rows = []
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
