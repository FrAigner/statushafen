import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { useAuth } from './useAuth'
import { useAuthConfig, resetAuthConfig } from './useAuthConfig'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// Public auth configuration (GET /api/public/auth-config): which sign-in methods
// the backend allows. Loaded once, then served from the cache.
describe('useAuthConfig', () => {
  beforeEach(() => {
    resetAuthConfig()
    useAuth().logout()
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads the config without a token and takes over the password switch and providers', async () => {
    useAuth().token.value = 'jwt-alt'
    global.fetch = vi.fn(async () => jsonResponse({ password_login: true, providers: [{ id: 'gitlab', name: 'GitLab' }] }))
    const { authConfig, loadAuthConfig } = useAuthConfig()

    await loadAuthConfig()

    expect(global.fetch).toHaveBeenCalledWith('/api/public/auth-config', expect.anything())
    expect(global.fetch.mock.calls[0][1].headers['X-Auth-Token']).toBeUndefined()
    expect(authConfig.value).toEqual({ passwordLogin: true, providers: [{ id: 'gitlab', name: 'GitLab' }] })
  })

  it('loads only once, even for concurrent calls from several components', async () => {
    global.fetch = vi.fn(async () => jsonResponse({ password_login: false, providers: [] }))

    await Promise.all([useAuthConfig().loadAuthConfig(), useAuthConfig().loadAuthConfig()])
    await useAuthConfig().loadAuthConfig()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect(useAuthConfig().authConfig.value).toEqual({ passwordLogin: false, providers: [] })
  })

  // Only an explicit true enables passwords; broken provider entries are dropped
  // instead of producing buttons without a target or name.
  it.each([
    [{}, { passwordLogin: false, providers: [] }],
    [{ password_login: 'true', providers: 'gitlab' }, { passwordLogin: false, providers: [] }],
    [
      { password_login: true, providers: [{ id: 'gitlab', name: 'GitLab' }, { id: '', name: 'Empty' }, { id: 'x' }, { name: 'No id' }, null, { id: 7, name: 'Number' }] },
      { passwordLogin: true, providers: [{ id: 'gitlab', name: 'GitLab' }] },
    ],
  ])('validates the response %j', async (body, expected) => {
    global.fetch = vi.fn(async () => jsonResponse(body))
    const { authConfig, loadAuthConfig } = useAuthConfig()

    await loadAuthConfig()

    expect(authConfig.value).toEqual(expected)
  })

  it('does not ask when the config is already known', async () => {
    global.fetch = vi.fn()
    useAuthConfig().authConfig.value = { passwordLogin: true, providers: [] }

    await useAuthConfig().loadAuthConfig()

    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('reports a load error without made-up values and reloads on the next call', async () => {
    global.fetch = vi.fn(async () => jsonResponse({ error: 'broken' }, 500))
    const { authConfig, loadFailed, loadAuthConfig } = useAuthConfig()

    await loadAuthConfig()

    expect(authConfig.value).toBeNull()
    expect(loadFailed.value).toBe(true)

    global.fetch = vi.fn(async () => jsonResponse({ password_login: true, providers: [] }))
    await loadAuthConfig()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    expect(loadFailed.value).toBe(false)
    expect(authConfig.value).toEqual({ passwordLogin: true, providers: [] })
  })
})
