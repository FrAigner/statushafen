import { describe, it, expect, afterEach, vi } from 'vitest'
import { appBaseDomains, primaryBaseDomain } from './appDomains'

describe('appDomains', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('liefert ohne Konfiguration keine Basisdomain (kein fest verdrahteter Betreibername)', () => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', '')
    expect(appBaseDomains()).toEqual([])
    expect(primaryBaseDomain()).toBe('')
  })

  it('liest eine kommagetrennte Liste, trimmt und schreibt klein', () => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', ' Example.com , status.example.org ,, ')
    expect(appBaseDomains()).toEqual(['example.com', 'status.example.org'])
    expect(primaryBaseDomain()).toBe('example.com')
  })

  it('liest die Variable bei jedem Aufruf neu', () => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', 'a.example')
    expect(primaryBaseDomain()).toBe('a.example')
    vi.stubEnv('VITE_APP_BASE_DOMAINS', 'b.example')
    expect(primaryBaseDomain()).toBe('b.example')
  })
})
