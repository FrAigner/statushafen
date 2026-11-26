import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// GHSA-cg2g-xh64-v4q7, GHSA-mrv2-3pqr-j5jr, GHSA-c6gr-qvv4-5v3f: dompurify <= 3.4.16
// has three XSS vulnerabilities (SAFE_FOR_TEMPLATES bypass via shadow DOM, incomplete
// neutralisation in IN_PLACE hooks, missing lowercasing of attributeName in
// XHTML mode). Patched as of 3.4.17 (npm audit --audit-level=high).
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const MIN_PATCHED_VERSION = [3, 4, 17]

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

describe('dompurify Sicherheits-Patch (npm audit)', () => {
  it('installierte dompurify-Version liegt über der verwundbaren 3.4.16 (GHSA-cg2g-xh64-v4q7, GHSA-mrv2-3pqr-j5jr, GHSA-c6gr-qvv4-5v3f)', () => {
    const pkg = JSON.parse(readFileSync(resolve(root, 'node_modules/dompurify/package.json'), 'utf-8'))
    expect(isAtLeast(parseVersion(pkg.version), MIN_PATCHED_VERSION)).toBe(true)
  })

  it('isAtLeast erkennt ältere Versionen korrekt als nicht gepatcht', () => {
    expect(isAtLeast(parseVersion('3.4.16'), MIN_PATCHED_VERSION)).toBe(false)
    expect(isAtLeast(parseVersion('3.4.0'), MIN_PATCHED_VERSION)).toBe(false)
    expect(isAtLeast(parseVersion('3.4.17'), MIN_PATCHED_VERSION)).toBe(true)
    expect(isAtLeast(parseVersion('3.5.0'), MIN_PATCHED_VERSION)).toBe(true)
    expect(isAtLeast(parseVersion('4.0.0'), MIN_PATCHED_VERSION)).toBe(true)
  })
})
