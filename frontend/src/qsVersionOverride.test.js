import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// GHSA-q8mj-m7cp-5q26 und GHSA-x5fp-wj9c-mxmx: qs 6.11.1-6.15.1 bzw.
// 6.14.2-6.15.3 hat zwei DoS-Lücken (qs.stringify-Crash, array-limit-Bypass).
// qs ist keine direkte Dependency, sondern kommt über
// @stryker-mutator/core -> typed-rest-client@~2.3.0 -> qs@6.15.1 (verwundbar).
// typed-rest-client selbst bumpt das nicht; gefixt per "overrides" in
// package.json auf die gepatchte Version.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const MIN_PATCHED_VERSION = [6, 16, 0]

function parseVersion(v) {
  return v.split('.').map(Number)
}

function isAtLeast(version, min) {
  for (let i = 0; i < min.length; i++) {
    const part = version[i] ?? 0
    if (part > min[i]) return true
    if (part < min[i]) return false
  }
  return true
}

describe('qs Sicherheits-Override (npm audit, via @stryker-mutator/core)', () => {
  it('installierte qs-Version liegt über den verwundbaren 6.15.1/6.15.3 (GHSA-q8mj-m7cp-5q26, GHSA-x5fp-wj9c-mxmx)', () => {
    const pkg = JSON.parse(readFileSync(resolve(root, 'node_modules/qs/package.json'), 'utf-8'))
    expect(isAtLeast(parseVersion(pkg.version), MIN_PATCHED_VERSION)).toBe(true)
  })
})
