import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve, join } from 'path'
import { RESOURCES, ACTIONS } from './utils/permissions'

// After the switch to Create/Read/Update/Delete,
// `e2e/admin-roles.spec.ts` still referred to the removed permission
// `incidents:write`. Playwright specs only run in the E2E environment and
// therefore fail late. This fast test reads all E2E specs and requires that
// every permission `<resource>:<action>` named there exists in the
// permission catalog (resources and actions as in the backend).
const e2eDir = resolve(dirname(fileURLToPath(import.meta.url)), '../e2e')
const resources = new Set(RESOURCES.map(r => r.key))
const actions = new Set<string>(ACTIONS)

// All `resource:action` candidates whose action part looks like a permission
// action (including the removed `write`), so stale ones stand out.
const findePermissionRefs = (source: string) =>
  [...source.matchAll(/\b([a-z][a-z_]*):(read|write|create|update|delete)\b/g)].map(m => ({
    resource: m[1], action: m[2], text: m[0],
  }))
const unbekannteRechte = (source: string) =>
  findePermissionRefs(source).filter(r => !resources.has(r.resource) || !actions.has(r.action)).map(r => r.text)

describe('E2E-Specs verwenden nur existierende Rechte', () => {
  it('erkennt das entfernte Recht incidents:write und unbekannte Ressourcen', () => {
    expect(unbekannteRechte('value="incidents:write" incidents:update foo:read')).toEqual(['incidents:write', 'foo:read'])
  })

  const specs = readdirSync(e2eDir).filter(f => f.endsWith('.ts'))
  it('findet E2E-Dateien', () => {
    expect(specs.length).toBeGreaterThan(5)
  })
  for (const file of specs) {
    it(`${file}: nur Rechte aus dem Katalog`, () => {
      expect(unbekannteRechte(readFileSync(join(e2eDir, file), 'utf8'))).toEqual([])
    })
  }
})
