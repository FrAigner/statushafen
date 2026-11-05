import i18n from '../i18n'

// Hilfetexte für Felder und Funktionen.
//
// Katalog-Schema in src/i18n/locales/{de,en}.json:
//
//   "help": {
//     "<bereich>": {            z. B. "components" (eine Admin-Seite)
//       "<feld>": {             z. B. "name", "tags", "save"
//         "what":    "Was man hier tun kann",
//         "effect":  "Welche Auswirkung das hat",
//         "example": "Ein konkretes Beispiel"
//       }
//     }
//   }
//
// Der Schlüssel, den Views an v-help übergeben, ist "<bereich>.<feld>" (ohne
// "help."). Fehlt einer der drei Teile in der aktiven Sprache, gibt es keinen
// Hilfetext: v-help hängt dann weder Listener noch aria-describedby an (kein
// leerer Tooltip, kein Verweis auf ein fehlendes Element). In DEV und Tests
// erscheint eine Warnung, damit die Lücke auffällt. Der Abdeckungs-Test
// (src/views/helpCoverage.test.js) erzwingt die Vollständigkeit für fertige
// Seiten.
//
// Bewusst über i18n.global statt useI18n(): v-help und die Tooltip-Ebene
// laufen außerhalb eines Komponenten-setup (Direktive bzw. eigener
// render()-Baum ohne App-Kontext).

export const HELP_PARTS = ['what', 'effect', 'example']

// DOM-ID des Tooltips zu einem Schlüssel. Deterministisch, damit Feld
// (aria-describedby) und Tooltip-Ebene (id) unabhängig voneinander dieselbe
// ID bilden.
export function helpDomId(key) {
  return `help-${key.replace(/[^\w-]/g, '-')}`
}

// { what, effect, example } in der aktiven Sprache oder null.
export function helpTextFor(key) {
  const { t, te } = i18n.global
  const base = `help.${key}`
  const missing = HELP_PARTS.filter(part => !te(`${base}.${part}`))
  if (missing.length) {
    if (import.meta.env.DEV) console.warn(`[v-help] Hilfetext fehlt: ${base}.{${missing.join(',')}}`)
    return null
  }
  return Object.fromEntries(HELP_PARTS.map(part => [part, t(`${base}.${part}`)]))
}
