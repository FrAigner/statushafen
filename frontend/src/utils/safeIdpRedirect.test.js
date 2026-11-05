import { describe, it, expect, vi, afterEach } from 'vitest'
import { safeIdpRedirect } from './safeIdpRedirect'

afterEach(() => vi.unstubAllEnvs())

describe('safeIdpRedirect', () => {
  it('lässt https-Adressen durch', () => {
    expect(safeIdpRedirect('https://idp.example.com/authorize?x=1')).toBe('https://idp.example.com/authorize?x=1')
    expect(safeIdpRedirect('HTTPS://idp.example.com/a')).toBe('HTTPS://idp.example.com/a')
  })

  it.each([
    'javascript:alert(1)',
    'JaVaScRiPt:alert(1)',
    ' javascript:alert(1)',
    'java\tscript:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'vbscript:msgbox(1)',
    'file:///etc/passwd',
    'blob:https://idp.example.com/x',
    '//idp.example.com/authorize',
    '/authorize',
    'idp.example.com/authorize',
    'https://',
    '',
    null,
    undefined,
    42,
    { href: 'https://idp.example.com' },
    { toString: () => 'https://idp.example.com/authorize' }, // würde von new URL() zur Zeichenkette gemacht
  ])('lehnt %j ab', (value) => {
    expect(safeIdpRedirect(value)).toBeNull()
  })

  it('lehnt http ab, außer localhost im Entwicklungsmodus', () => {
    vi.stubEnv('DEV', false)
    expect(safeIdpRedirect('http://idp.example.com/authorize')).toBeNull()
    expect(safeIdpRedirect('http://localhost:9000/authorize')).toBeNull()

    vi.stubEnv('DEV', true)
    expect(safeIdpRedirect('http://localhost:9000/authorize')).toBe('http://localhost:9000/authorize')
    expect(safeIdpRedirect('http://127.0.0.1:9000/authorize')).toBe('http://127.0.0.1:9000/authorize')
    expect(safeIdpRedirect('http://[::1]:9000/authorize')).toBe('http://[::1]:9000/authorize')
    expect(safeIdpRedirect('http://idp.example.com/authorize')).toBeNull()
    expect(safeIdpRedirect('http://localhost.example.com/authorize')).toBeNull()
  })
})
