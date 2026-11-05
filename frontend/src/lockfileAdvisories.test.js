import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

// Bekannte Sicherheitsmeldungen zu Paketen, die nur über Entwicklungswerkzeuge
// (ESLint, Test-Utils, Mutationstest) in die Lock-Datei kommen. Dependabot kann
// sie nicht von selbst beheben, weil die übergeordneten Pakete ältere Versionen
// verlangen; `overrides` in package.json hebt sie an. Dieser Test hält fest,
// dass keine Version aus dem verwundbaren Bereich in der Lock-Datei steht.
// Beim Eintrag neuer Meldungen die Grenze hier nachtragen.
const MINIMUM_SAFE = {
  glob: '14.0.2', // verwundbar: 9.0.0 bis 14.0.1
  minimatch: '11.0.1', // verwundbar: 7.0.0 bis 11.0.0
}

const toParts = (v) => v.split('-')[0].split('.').map(Number)

export function isBelow(version, minimum) {
  const a = toParts(version)
  const b = toParts(minimum)
  for (let i = 0; i < 3; i++) {
    if ((a[i] ?? 0) !== (b[i] ?? 0)) return (a[i] ?? 0) < (b[i] ?? 0)
  }
  return false
}

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const readJson = (name) => JSON.parse(readFileSync(resolve(frontendRoot, name), 'utf8'))
const lock = readJson('package-lock.json')

function installed(name) {
  return Object.entries(lock.packages)
    .filter(([path]) => path === `node_modules/${name}` || path.endsWith(`/node_modules/${name}`))
    .map(([path, pkg]) => ({ path, version: pkg.version }))
}

describe('Lock-Datei ohne bekannte verwundbare Versionen', () => {
  for (const [name, minimum] of Object.entries(MINIMUM_SAFE)) {
    it(`${name} liegt überall mindestens bei ${minimum}`, () => {
      const found = installed(name)
      expect(found.length).toBeGreaterThan(0)
      const bad = found.filter((p) => isBelow(p.version, minimum))
      expect(bad, `verwundbare Einträge: ${JSON.stringify(bad)}`).toEqual([])
    })
  }

  it('der Versionsvergleich erkennt Grenzfälle (Gegenprobe)', () => {
    expect(isBelow('14.0.1', '14.0.2')).toBe(true)
    expect(isBelow('14.0.2', '14.0.2')).toBe(false)
    expect(isBelow('14.1.0', '14.0.2')).toBe(false)
    expect(isBelow('10.5.0', '14.0.2')).toBe(true)
    expect(isBelow('11.0.0', '11.0.1')).toBe(true)
    expect(isBelow('11.0.4', '11.0.1')).toBe(false)
    expect(isBelow('9.0.9', '11.0.1')).toBe(true)
  })

  it('package.json hebt beide Pakete per overrides an', () => {
    const pkg = readJson('package.json')
    for (const name of Object.keys(MINIMUM_SAFE)) {
      expect(pkg.overrides?.[name], `${name} fehlt in overrides`).toBeTruthy()
    }
  })
})
