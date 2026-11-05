import { describe, it, expect, beforeEach } from 'vitest'
import { vi } from 'vitest'
import { useAuth } from './useAuth'
import { useTenant } from './useTenant'
import i18n, { LANGUAGE_STORAGE_KEY, SUPPORTED_LOCALES, registerLocale } from '../i18n'

// Baut ein unsigniertes JWT (Header/Payload reichen, loginWithToken prüft die
// Signatur nicht) mit dem gegebenen Payload - analog zum echten Backend-Token.
function makeJwt(payload) {
  const base64url = (obj) =>
    btoa(JSON.stringify(obj)).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '')
  return `${base64url({ alg: 'none', typ: 'JWT' })}.${base64url(payload)}.sig`
}

describe('useAuth', () => {
  beforeEach(() => {
    useAuth().logout()
    localStorage.clear()
  })

  describe('setAuth / logout', () => {
    it('setzt Token und User und persistiert das Token im localStorage', () => {
      const { setAuth, token, currentUser, isLoggedIn } = useAuth()

      setAuth('jwt-123', { email: 'a@b.de', is_global_admin: false })

      expect(token.value).toBe('jwt-123')
      expect(currentUser.value.email).toBe('a@b.de')
      expect(localStorage.getItem('jwt_token')).toBe('jwt-123')
      expect(isLoggedIn.value).toBe(true)
    })

    it('logout löscht den Einladungs-Token aus sessionStorage', () => {
      sessionStorage.setItem('invitationToken', 'tok')
      useAuth().logout()
      expect(sessionStorage.getItem('invitationToken')).toBeNull()
    })

    it('logout klappt auch bei blockiertem sessionStorage', () => {
      vi.stubGlobal('sessionStorage', { removeItem() { throw new Error('blockiert') } })
      expect(() => useAuth().logout()).not.toThrow()
      vi.unstubAllGlobals()
    })

    it('logout räumt Token, User und localStorage', () => {
      const { setAuth, logout, token, currentUser, isLoggedIn } = useAuth()
      setAuth('jwt-123', { email: 'a@b.de' })

      logout()

      expect(token.value).toBe('')
      expect(currentUser.value).toBeNull()
      expect(localStorage.getItem('jwt_token')).toBeNull()
      expect(isLoggedIn.value).toBe(false)
    })

    it('isLoggedIn ist false, solange Token oder User fehlt', () => {
      const { setAuth, token, isLoggedIn } = useAuth()

      expect(isLoggedIn.value).toBe(false)

      token.value = 'jwt-only-no-profile'
      expect(isLoggedIn.value).toBe(false)
    })
  })

  describe('isGlobalAdmin', () => {
    it('ist true, wenn currentUser.is_global_admin gesetzt ist', () => {
      const { setAuth, isGlobalAdmin } = useAuth()
      setAuth('jwt-123', { is_global_admin: true })

      expect(isGlobalAdmin.value).toBe(true)
    })

    it('ist false ohne User oder ohne Admin-Flag', () => {
      const { setAuth, isGlobalAdmin } = useAuth()
      expect(isGlobalAdmin.value).toBe(false)

      setAuth('jwt-123', { is_global_admin: false })
      expect(isGlobalAdmin.value).toBe(false)
    })
  })

  describe('hasPerm', () => {
    it('bypasst die Permission-Prüfung für Global Admins', () => {
      const { setAuth, hasPerm } = useAuth()
      setAuth('jwt-123', { is_global_admin: true, permissions: '' })

      expect(hasPerm('anything')).toBe(true)
    })

    it('ist false ohne eingeloggten User', () => {
      const { hasPerm } = useAuth()

      expect(hasPerm('view')).toBe(false)
    })

    // wie das Backend zählt nur die Rolle auf der aktiven Seite.
    it('prüft gegen die Rechte der aktiven Statusseite, nicht gegen die globale Rolle', () => {
      const { setAuth, hasPerm } = useAuth()
      const { setMyPages, setActiveTenant } = useTenant()
      setAuth('jwt-123', { is_global_admin: false, permissions: 'view,edit,delete' })
      setMyPages([{ ID: 1, Permissions: 'view, edit' }, { ID: 2, Permissions: 'delete' }])
      setActiveTenant(1)

      expect(hasPerm('view')).toBe(true)
      expect(hasPerm('edit')).toBe(true)
      expect(hasPerm('delete')).toBe(false)
    })

    it('wertet ohne aktive Seite keine globalen Rechte aus', () => {
      const { setAuth, hasPerm } = useAuth()
      const { setMyPages, setActiveTenant } = useTenant()
      setMyPages([])
      setActiveTenant(null)
      setAuth('jwt-123', { is_global_admin: false, permissions: 'components:write' })

      expect(hasPerm('components:write')).toBe(false)
    })

    it('lässt write das read-Recht desselben Bereichs einschließen', () => {
      const { setAuth, hasPerm } = useAuth()
      const { setMyPages, setActiveTenant } = useTenant()
      setAuth('jwt-123', { is_global_admin: false })
      setMyPages([{ ID: 1, Permissions: 'components:write' }])
      setActiveTenant(1)

      expect(hasPerm('components:read')).toBe(true)
      expect(hasPerm('components:write')).toBe(true)
      expect(hasPerm('incidents:read')).toBe(false)
    })

    it('schließt read nicht als write ein', () => {
      const { setAuth, hasPerm } = useAuth()
      const { setMyPages, setActiveTenant } = useTenant()
      setAuth('jwt-123', { is_global_admin: false })
      setMyPages([{ ID: 1, Permissions: 'components:read' }])
      setActiveTenant(1)

      expect(hasPerm('components:write')).toBe(false)
    })

    // je Tab Create/Read/Update/Delete.
    it('lässt create, update und delete das read-Recht einschließen, aber nicht einander', () => {
      const { setAuth, hasPerm } = useAuth()
      const { setMyPages, setActiveTenant } = useTenant()
      setAuth('jwt-123', { is_global_admin: false })
      setMyPages([{ ID: 1, Permissions: 'notices:create,incidents:update' }])
      setActiveTenant(1)

      expect(hasPerm('notices:read')).toBe(true)
      expect(hasPerm('notices:create')).toBe(true)
      expect(hasPerm('notices:update')).toBe(false)
      expect(hasPerm('notices:delete')).toBe(false)
      expect(hasPerm('incidents:read')).toBe(true)
      expect(hasPerm('incidents:create')).toBe(false)
      expect(hasPerm('components:read')).toBe(false)
    })

    it('zählt den Altwert :write (Alt-Rolle, Alt-Token) als alle vier Aktionen', () => {
      const { setAuth, hasPerm } = useAuth()
      const { setMyPages, setActiveTenant } = useTenant()
      setAuth('jwt-123', { is_global_admin: false })
      setMyPages([{ ID: 1, Permissions: 'incidents:write' }])
      setActiveTenant(1)

      for (const a of ['create', 'read', 'update', 'delete']) expect(hasPerm(`incidents:${a}`)).toBe(true)
      expect(hasPerm('maintenance:read')).toBe(false)
    })

    it('gibt globale Ressourcen nur Global Admins, auch wenn die Seitenrolle den String trägt', () => {
      const { setAuth, hasPerm } = useAuth()
      const { setMyPages, setActiveTenant } = useTenant()
      setAuth('jwt-123', { is_global_admin: false })
      setMyPages([{ ID: 1, Permissions: 'users:update,roles:read,audit:read' }])
      setActiveTenant(1)

      expect(hasPerm('users:read')).toBe(false)
      expect(hasPerm('roles:read')).toBe(false)
      expect(hasPerm('audit:read')).toBe(false)

      setAuth('jwt-123', { is_global_admin: true })
      expect(hasPerm('users:delete')).toBe(true)
    })

    it('behandelt das Sonderrecht admin_dashboard exakt', () => {
      const { setAuth, hasPerm } = useAuth()
      const { setMyPages, setActiveTenant } = useTenant()
      setAuth('jwt-123', { is_global_admin: false })
      setMyPages([{ ID: 1, Permissions: 'admin_dashboard' }])
      setActiveTenant(1)
      expect(hasPerm('admin_dashboard')).toBe(true)
      expect(hasPerm('components:read')).toBe(false)
    })
  })

  describe('loginWithToken (SSO)', () => {
    it('dekodiert ein gültiges JWT und übernimmt Token + Profil', () => {
      const { loginWithToken, token, currentUser, isGlobalAdmin } = useAuth()
      const jwt = makeJwt({
        email: 'sso@example.com',
        role: 'Admin',
        permissions: 'view,edit',
        is_global_admin: true,
        sso: 'okta',
        name: 'SSO User',
        user_id: 7,
      })

      loginWithToken(jwt)

      expect(token.value).toBe(jwt)
      expect(localStorage.getItem('jwt_token')).toBe(jwt)
      expect(currentUser.value).toMatchObject({
        email: 'sso@example.com',
        role: 'Admin',
        permissions: 'view,edit',
        is_global_admin: true,
        sso_provider: 'okta',
        name: 'SSO User',
        id: 7,
      })
      expect(isGlobalAdmin.value).toBe(true)
    })

    it('übernimmt die Tooltip-Einstellung aus dem SSO-Token', () => {
      const { loginWithToken, currentUser } = useAuth()

      loginWithToken(makeJwt({ email: 'sso@example.com', hide_help_tooltips: true }))
      expect(currentUser.value.help_tooltips).toBe(false)

      // Claim fehlt (omitempty bei Default): Tooltips an.
      loginWithToken(makeJwt({ email: 'sso@example.com' }))
      expect(currentUser.value.help_tooltips).toBe(true)
    })

    it('lässt Token/User unverändert bei einem strukturell ungültigen Token', () => {
      const { loginWithToken, token, currentUser } = useAuth()

      loginWithToken('not-a-valid-jwt')

      expect(token.value).toBe('')
      expect(currentUser.value).toBeNull()
    })

    it('übernimmt preferred_language aus dem SSO-Token in die aktive UI-Sprache', () => {
      i18n.global.locale.value = 'de'
      const { loginWithToken } = useAuth()
      const jwt = makeJwt({
        email: 'sso@example.com',
        preferred_language: 'en',
      })

      loginWithToken(jwt)

      expect(i18n.global.locale.value).toBe('en')
      expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe('en')
    })

    it('lässt die aktive UI-Sprache unverändert, wenn preferred_language fehlt oder nicht unterstützt wird', () => {
      i18n.global.locale.value = 'de'
      const { loginWithToken } = useAuth()

      loginWithToken(makeJwt({ email: 'a@b.de' }))
      expect(i18n.global.locale.value).toBe('de')

      loginWithToken(makeJwt({ email: 'a@b.de', preferred_language: 'fr' }))
      expect(i18n.global.locale.value).toBe('de')
    })
  })
})

// Teil 1: Eine nachladbare Profil-Sprache wird erst nach dem Laden ihrer Texte aktiv.
describe('useAuth: nachladbare Profil-Sprache', () => {
  it('aktiviert die Sprache erst, wenn ihre Texte geladen sind', async () => {
    localStorage.clear()
    i18n.global.locale.value = 'de'
    let finish
    registerLocale('zx', () => new Promise((resolve) => { finish = () => resolve({ default: { common: { save: 'ZX' } } }) }))
    useAuth().setAuth('jwt-1', { email: 'a@b.de', preferred_language: 'zx' })
    expect(i18n.global.locale.value).toBe('de')
    finish()
    await vi.waitFor(() => expect(i18n.global.locale.value).toBe('zx'))
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe('zx')
    expect(i18n.global.t('common.save')).toBe('ZX')
    i18n.global.locale.value = 'de'
    SUPPORTED_LOCALES.splice(SUPPORTED_LOCALES.indexOf('zx'), 1)
  })
})
