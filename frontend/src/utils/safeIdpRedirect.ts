// The authorize URL for linking and test sign-in comes from the server and
// ultimately from the discovery document of the provider a page admin
// enters. Passed unchecked to location.assign, `javascript:` or `data:` would
// be a script or phishing target. This helper is the only place that turns
// such an address into a navigation target (allowlist: https; http only for
// localhost in development mode, like the mock provider from docker-compose.yml).

const LOCAL_HOSTS = new Set(['localhost', '127.0.0.1', '[::1]'])

/**
 * @param raw address from the server response
 * @returns the unchanged address, or null if it is unusable
 */
export function safeIdpRedirect(raw: unknown): string | null {
  if (typeof raw !== 'string') return null
  let u: URL
  try {
    u = new URL(raw)
  } catch {
    return null // relative, empty or broken
  }
  if (u.protocol === 'https:') return raw
  if (u.protocol === 'http:' && import.meta.env.DEV && LOCAL_HOSTS.has(u.hostname)) return raw
  return null
}
