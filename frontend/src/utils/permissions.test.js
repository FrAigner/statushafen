import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { resolve, dirname } from 'path'
import { fileURLToPath } from 'url'
import { ACTIONS, RESOURCES, GROUPS, grants, normalizePermissions, isGlobalResource, perm } from './permissions'
import { NAV_GROUPS, firstAllowedItem } from './adminNav'

describe('grants', () => {
  it('create, update und delete schließen read ein, aber nicht einander', () => {
    for (const act of ['create', 'update', 'delete']) {
      expect(grants(`notices:${act}`, 'notices:read')).toBe(true)
      expect(grants(`notices:${act}`, `notices:${act}`)).toBe(true)
    }
    expect(grants('notices:read', 'notices:update')).toBe(false)
    expect(grants('notices:create', 'notices:update')).toBe(false)
    expect(grants('notices:update', 'notices:delete')).toBe(false)
    expect(grants('notices:delete', 'notices:create')).toBe(false)
  })

  it('gilt nur für dieselbe Ressource', () => {
    expect(grants('notices:update', 'components:read')).toBe(false)
    expect(grants('', 'components:read')).toBe(false)
    expect(grants(null, 'components:read')).toBe(false)
  })

  it('akzeptiert Liste als String (mit Leerzeichen) und als Array', () => {
    expect(grants(' notices:read , components:create', 'components:read')).toBe(true)
    expect(grants(['components:create'], 'components:read')).toBe(true)
  })

  // Kompatibilität: der Altwert :write gilt als alle vier Aktionen.
  it('zählt den Altwert :write als create, read, update und delete derselben Ressource', () => {
    for (const act of ACTIONS) expect(grants('components:write', perm('components', act))).toBe(true)
    expect(grants('components:write', 'incidents:read')).toBe(false)
  })

  it('behandelt Sonderrechte ohne Aktion nur exakt', () => {
    expect(grants('admin_dashboard', 'admin_dashboard')).toBe(true)
    expect(grants('components:read', 'admin_dashboard')).toBe(false)
  })
})

describe('normalizePermissions', () => {
  it('ersetzt :write durch die vier Aktionen und ergänzt read', () => {
    expect(normalizePermissions('components:write')).toEqual(
      ['components:create', 'components:read', 'components:update', 'components:delete'])
    expect(normalizePermissions('notices:update,admin_dashboard')).toEqual(
      ['notices:update', 'admin_dashboard', 'notices:read'])
    expect(normalizePermissions('')).toEqual([])
  })
})

describe('Ressourcenliste', () => {
  it('ist eindeutig und vollständig gruppiert', () => {
    const keys = RESOURCES.map(r => r.key)
    expect(new Set(keys).size).toBe(keys.length)
    for (const r of RESOURCES) expect(GROUPS).toContain(r.group)
    expect(isGlobalResource('users')).toBe(true)
    expect(isGlobalResource('components')).toBe(false)
    expect(isGlobalResource('unbekannt')).toBe(false)
  })

  // Drift-Test: backend/permissions/permissions.go ist die Quelle.
  it('entspricht der Ressourcenliste des Backends', () => {
    const go = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../../../backend/permissions/permissions.go'), 'utf8')
    const groups = { GroupOperations: 'operations', GroupContent: 'content', GroupManagement: 'management', GroupSystem: 'system' }
    const backend = [...go.matchAll(/\{"(\w+)", (Group\w+), Scope(Page|Global)\},/g)]
      .map(m => ({ key: m[1], group: groups[m[2]], scope: m[3].toLowerCase() }))
    expect(backend.length).toBeGreaterThan(15)
    expect(RESOURCES).toEqual(backend)
  })
})

describe('Navigation als Daten', () => {
  it('jede Ressource ist genau ein Tab und liegt im Bereich der Ressource', () => {
    const seen = []
    for (const group of NAV_GROUPS) {
      for (const item of group.items) {
        if (!item.resource) continue
        seen.push(item.resource)
        expect(RESOURCES.find(r => r.key === item.resource)?.group).toBe(group.key)
      }
    }
    expect(seen.sort()).toEqual(RESOURCES.map(r => r.key).sort())
  })

  it('firstAllowedItem nimmt den ersten lesbaren Tab und ignoriert die Doku', () => {
    expect(firstAllowedItem(() => true).to).toBe('/admin/overview')
    expect(firstAllowedItem(p => p === 'subscribers:read').to).toBe('/admin/subscribers')
    expect(firstAllowedItem(p => p === 'notification_templates:read').to).toBe('/admin/notification-templates')
    expect(firstAllowedItem(() => false)).toBeNull()
  })
})
