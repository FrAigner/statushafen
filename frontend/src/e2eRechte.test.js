import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve, join } from 'path'
import { RESOURCES, ACTIONS } from './utils/permissions'

// Nach der Umstellung auf Create/Read/Update/Delete
// verwies `e2e/admin-roles.spec.js` weiter auf das entfernte Recht
// `incidents:write`. Playwright-Specs laufen nur in der E2E-Umgebung und
// fallen deshalb erst spät auf. Dieser schnelle Test liest alle E2E-Specs und
// verlangt, dass jedes dort genannte Recht `<ressource>:<aktion>` im
// Rechtekatalog existiert (Ressourcen und Aktionen wie im Backend).
const e2eDir = resolve(dirname(fileURLToPath(import.meta.url)), '../e2e')
const resources = new Set(RESOURCES.map(r => r.key))
const actions = new Set(ACTIONS)

// Alle `ressource:aktion`-Kandidaten, deren Aktionsteil wie eine Rechte-Aktion
// aussieht (auch das entfernte `write`), damit Veraltetes auffällt.
const findePermissionRefs = (source) =>
  [...source.matchAll(/\b([a-z][a-z_]*):(read|write|create|update|delete)\b/g)].map(m => ({
    resource: m[1], action: m[2], text: m[0],
  }))
const unbekannteRechte = (source) =>
  findePermissionRefs(source).filter(r => !resources.has(r.resource) || !actions.has(r.action)).map(r => r.text)

describe('E2E-Specs verwenden nur existierende Rechte', () => {
  it('erkennt das entfernte Recht incidents:write und unbekannte Ressourcen', () => {
    expect(unbekannteRechte('value="incidents:write" incidents:update foo:read')).toEqual(['incidents:write', 'foo:read'])
  })

  const specs = readdirSync(e2eDir).filter(f => f.endsWith('.js'))
  it('findet E2E-Dateien', () => {
    expect(specs.length).toBeGreaterThan(5)
  })
  for (const file of specs) {
    it(`${file}: nur Rechte aus dem Katalog`, () => {
      expect(unbekannteRechte(readFileSync(join(e2eDir, file), 'utf8'))).toEqual([])
    })
  }
})
