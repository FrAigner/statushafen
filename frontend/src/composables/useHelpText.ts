import i18n from '../i18n'

// Help texts for fields and functions.
//
// Catalog schema in src/i18n/locales/{de,en}.json:
//
//   "help": {
//     "<area>": {               e.g. "components" (an admin page)
//       "<field>": {            e.g. "name", "tags", "save"
//         "what":    "What you can do here",
//         "effect":  "What effect it has",
//         "example": "A concrete example"
//       }
//     }
//   }
//
// The key views pass to v-help is "<area>.<field>" (without "help."). If one
// of the three parts is missing in the active language, there is no help
// text: v-help then attaches neither listeners nor aria-describedby (no empty
// tooltip, no reference to a missing element). In DEV and tests a warning
// appears so the gap is noticed. The coverage test
// (src/views/helpCoverage.test.ts) enforces completeness for finished pages.
//
// Deliberately via i18n.global instead of useI18n(): v-help and the tooltip
// layer run outside a component setup (directive and a separate render()
// tree without app context).

export type HelpPart = 'what' | 'effect' | 'example'
export type HelpText = Record<HelpPart, string>

export const HELP_PARTS: HelpPart[] = ['what', 'effect', 'example']

// DOM ID of the tooltip for a key. Deterministic, so the field
// (aria-describedby) and the tooltip layer (id) form the same ID
// independently of each other.
export function helpDomId(key: string): string {
  return `help-${key.replace(/[^\w-]/g, '-')}`
}

// { what, effect, example } in the active language, or null.
export function helpTextFor(key: string): HelpText | null {
  const { t, te } = i18n.global
  const base = `help.${key}`
  const missing = HELP_PARTS.filter(part => !te(`${base}.${part}`))
  if (missing.length) {
    if (import.meta.env.DEV) console.warn(`[v-help] Hilfetext fehlt: ${base}.{${missing.join(',')}}`)
    return null
  }
  return Object.fromEntries(HELP_PARTS.map(part => [part, t(`${base}.${part}`)])) as HelpText
}
