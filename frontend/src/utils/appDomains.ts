// Base domain(s) of the installation (VITE_APP_BASE_DOMAINS, comma-separated).
// A tenant's status page is reachable under <slug>.<base domain>.
// Without configuration there is no subdomain convention: status pages are then
// reachable under /status/<slug> or via a custom domain. There is deliberately
// no default value, so that no operator name appears in the code.
// The variable is read on every call (testable via vi.stubEnv).
export function appBaseDomains(): string[] {
  return (import.meta.env.VITE_APP_BASE_DOMAINS || '')
    .split(',')
    .map(d => d.trim().toLowerCase())
    .filter(Boolean)
}

// First base domain, or '' without configuration.
export function primaryBaseDomain(): string {
  return appBaseDomains()[0] || ''
}
