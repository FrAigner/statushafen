// `returnTo` steht in der Query und ist damit Angreifer-Eingabe.
// Ungeprüft in router.push/location.assign gegeben, wäre `/login?returnTo=https://angreifer.example`
// ein Open Redirect (Phishing nach erfolgreichem Login). Dieser Helfer ist die EINZIGE Stelle,
// die einen returnTo-Wert in ein Navigationsziel verwandelt.
//
// Design-Entscheidung: Allowlist statt Blocklist. Akzeptiert wird nur ein interner Pfad,
// der (1) ein String in vernünftiger Länge ist, (2) keine Steuerzeichen, Leerzeichen und
// keinen Backslash enthält (Browser deuten `/\host` wie `//host`), (3) mit genau EINEM
// Slash beginnt, auch nach einmaligem Dekodieren (`/%2F/host`), und (4) per router.resolve
// auf eine bekannte Route zeigt. Alles andere fällt auf den Fallback.

const MAX_LENGTH = 2048
// Steuerzeichen C0/C1, Leerzeichen und Unicode-Zeilentrenner sowie der Backslash.
const VERBOTEN = new RegExp("[\\u0000-\\u0020\\u007f-\\u009f\\u2028\\u2029\\\\]")

function istInternerPfad(wert) {
  if (wert.length > MAX_LENGTH || VERBOTEN.test(wert)) return false
  if (!wert.startsWith('/')) return false
  let dekodiert
  try {
    dekodiert = decodeURIComponent(wert)
  } catch {
    return false // kaputte Prozent-Kodierung
  }
  // Ein einziger Check auf dem dekodierten Wert deckt `//host` und `/%2F/host` ab (kein doppelter Check, sonst äquivalente Mutanten); `/\host` und `/%5Chost` fängt VERBOTEN.
  return !dekodiert.startsWith('//') && !VERBOTEN.test(dekodiert.split('?')[0])
}

/**
 * Liefert ein sicheres, internes Navigationsziel für einen returnTo-Wert.
 * @param {unknown} raw Wert aus der Query (kann alles sein: Array, undefined, ...)
 * @param {import('vue-router').Router} router zum Abgleich gegen bekannte Routen
 * @param {string} [fallback] Ziel, wenn der Wert nicht taugt
 * @returns {string}
 */
export function safeReturnTo(raw, router, fallback = '/admin') {
  if (typeof raw !== 'string' || !istInternerPfad(raw)) return fallback
  const ziel = router.resolve(raw)
  return ziel.matched.length > 0 ? ziel.fullPath : fallback
}
