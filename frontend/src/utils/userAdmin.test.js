import { describe, it, expect } from 'vitest'
import { pageRoleOptions, userAdminErrorText } from './userAdmin'

describe('pageRoleOptions', () => {
  const roles = [{ ID: 1, Name: 'Global Admin', GlobalOnly: true }, { ID: 3, Name: 'Editor', GlobalOnly: false }]

  it('blendet GlobalOnly-Rollen aus', () => {
    expect(pageRoleOptions(roles).map(r => r.ID)).toEqual([3])
  })

  it('behält eine schon vergebene GlobalOnly-Rolle (Altbestand)', () => {
    expect(pageRoleOptions(roles, 1).map(r => r.ID)).toEqual([1, 3])
  })

  it('verträgt fehlende Rollen', () => {
    expect(pageRoleOptions(undefined)).toEqual([])
  })
})

describe('userAdminErrorText', () => {
  const t = (key) => `T:${key}`

  it('übersetzt bekannte Fehlercodes', () => {
    expect(userAdminErrorText({ message: 'x', data: { code: 'cannot_delete_self' } }, t)).toBe('T:users.errors.cannot_delete_self')
  })

  it('fällt bei unbekanntem Code auf die Meldung zurück', () => {
    expect(userAdminErrorText({ message: 'kaputt', data: { code: 'anders' } }, t)).toBe('kaputt')
  })

  it('liefert ohne Fehlerobjekt einen leeren Text', () => {
    expect(userAdminErrorText(undefined, t)).toBe('')
  })
})
