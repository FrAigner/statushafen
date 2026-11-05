import { describe, it, expect, afterEach, vi } from 'vitest'
import { INVITATION_TOKEN_KEY, readInvitationToken, storeInvitationToken, clearInvitationToken } from './invitationToken'

// Einladungs-Token nur in sessionStorage, jeder Zugriff abgesichert.
describe('invitationToken', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    sessionStorage.clear()
  })

  it('speichert, liest und löscht unter dem festen Schlüssel', () => {
    expect(INVITATION_TOKEN_KEY).toBe('invitationToken')
    storeInvitationToken('abc')
    expect(sessionStorage.getItem('invitationToken')).toBe('abc')
    expect(readInvitationToken()).toBe('abc')
    clearInvitationToken()
    expect(readInvitationToken()).toBe('')
  })

  it('liefert bei blockiertem Speicher einen leeren String und wirft nirgends', () => {
    const boom = () => { throw new Error('blockiert') }
    vi.stubGlobal('sessionStorage', { getItem: boom, setItem: boom, removeItem: boom })
    expect(readInvitationToken()).toBe('')
    expect(() => storeInvitationToken('x')).not.toThrow()
    expect(() => clearInvitationToken()).not.toThrow()
  })
})
