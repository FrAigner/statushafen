// `returnTo` sits in the query and is therefore attacker input.
// Passed unchecked to router.push/location.assign, `/login?returnTo=https://angreifer.example`
// would be an open redirect (phishing after a successful login). This helper is the ONLY place
// that turns a returnTo value into a navigation target.
//
// Design decision: allowlist instead of blocklist. Only an internal path is accepted that
// (1) is a string of reasonable length, (2) contains no control characters, spaces and
// no backslash (browsers read `/\host` like `//host`), (3) starts with exactly ONE
// slash, also after decoding once (`/%2F/host`), and (4) points to a known route via
// router.resolve. Everything else falls back to the fallback.
//
// The default fallback is the landing page "/": the admin area is only a target
// when it was chosen explicitly (returnTo=/admin or a direct requiresAuth route).

import type { Router } from 'vue-router'

const MAX_LENGTH = 2048
// C0/C1 control characters, spaces and Unicode line separators, plus the backslash.
const VERBOTEN = new RegExp("[\\u0000-\\u0020\\u007f-\\u009f\\u2028\\u2029\\\\]")

function istInternerPfad(wert: string): boolean {
  if (wert.length > MAX_LENGTH || VERBOTEN.test(wert)) return false
  if (!wert.startsWith('/')) return false
  let dekodiert: string
  try {
    dekodiert = decodeURIComponent(wert)
  } catch {
    return false // broken percent encoding
  }
  // A single check on the decoded value covers `//host` and `/%2F/host` (no double check, otherwise equivalent mutants); VERBOTEN catches `/\host` and `/%5Chost`.
  return !dekodiert.startsWith('//') && !VERBOTEN.test(dekodiert.split('?')[0] as string)
}

/**
 * Returns a safe, internal navigation target for a returnTo value.
 * @param raw value from the query (can be anything: array, undefined, ...)
 * @param router to match against known routes
 * @param fallback target when the value is unusable
 */
export function safeReturnTo(raw: unknown, router: Pick<Router, 'resolve'>, fallback = '/'): string {
  if (typeof raw !== 'string' || !istInternerPfad(raw)) return fallback
  const ziel = router.resolve(raw)
  return ziel.matched.length > 0 ? ziel.fullPath : fallback
}
