import { ref } from 'vue'
import i18n, { getBaseMessages, isLocaleLoaded, loadLocaleMessages } from './index'
import { useApi } from '../composables/useApi'

// UI-Übersetzungs-Overrides: Admin-editierbare
// Overrides für App-UI-Texte, ohne Code-Deploy. Merge-Reihenfolge
// Bundle < globaler Override < Seiten-Override, bei jeder Änderung komplett
// neu berechnet und per setLocaleMessage() (voller Ersatz statt
// mergeLocaleMessage) angewendet - inkrementelles Mergen böte keine saubere
// Möglichkeit, beim Seitenwechsel nur die Overrides der vorherigen Seite
// wieder zu entfernen.

// Modul-Scope-Singleton-State, analog zum Muster in useTenant.js.
const globalOverrides = ref({})
const pageOverrides = ref({})

// flatten wandelt ein verschachteltes Nachrichten-Objekt (wie de.json/
// en.json) in eine flache Map von "dotted" Key-Pfaden auf Werte um, z.B.
// { nav: { items: { sso: "SSO" } } } -> { "nav.items.sso": "SSO" }.
export function flatten(obj, prefix = '') {
  return Object.entries(obj).reduce((acc, [key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      Object.assign(acc, flatten(value, path))
    } else {
      acc[path] = value
    }
    return acc
  }, {})
}

// unflatten ist die Umkehrung von flatten - baut aus einer flachen
// "dotted"-Key-Map wieder das verschachtelte Nachrichten-Objekt, das
// vue-i18n als messages erwartet.
//
// Pfade mit __proto__/constructor/prototype-Segment werden
// verworfen, sonst würde ein bösartiger Override Object.prototype
// verschmutzen.
const FORBIDDEN_PARTS = new Set(['__proto__', 'constructor', 'prototype'])

//
// Review-Befund 1: Ein Schlüssel, der Pfadpräfix eines anderen Schlüssels
// ist (z. B. "nav" neben "nav.items.sso"), würde den Teilbaum ersetzen
// bzw. ein Blatt in ein Objekt verwandeln. Solche Präfix-Einträge werden
// reihenfolgeunabhängig verworfen, die tieferen Schlüssel bleiben.
export function unflatten(flatMap) {
  const result = {}
  const paths = Object.keys(flatMap)
  for (const [path, value] of Object.entries(flatMap)) {
    if (paths.some((other) => other.startsWith(`${path}.`))) continue
    const parts = path.split('.')
    if (parts.some((part) => FORBIDDEN_PARTS.has(part))) continue
    let node = result
    for (let i = 0; i < parts.length - 1; i++) {
      if (!node[parts[i]] || typeof node[parts[i]] !== 'object') {
        node[parts[i]] = {}
      }
      node = node[parts[i]]
    }
    node[parts.at(-1)] = value
  }
  return result
}

function recomputeAndApply(locale) {
  const base = getBaseMessages(locale)
  if (!base) return
  const merged = { ...flatten(base), ...globalOverrides.value, ...pageOverrides.value }
  i18n.global.setLocaleMessage(locale, unflatten(merged))
}

// applyGlobalOverrides lädt die globalen Overrides (öffentlicher Endpunkt,
// keine Auth nötig - auch anonyme Besucher sollen die global angepassten
// Texte sehen) und wendet sie an. Schlägt der Request fehl, bleibt es beim
// Bundle-Default statt die App am Start zu blockieren.
export async function applyGlobalOverrides(locale) {
  // Nachladbare Oberflächensprache: Texte vor dem Anwenden laden.
  if (!isLocaleLoaded(locale)) await loadLocaleMessages(locale)
  try {
    const res = await fetch(`/api/public/ui-overrides?language_code=${encodeURIComponent(locale)}`)
    globalOverrides.value = res.ok ? await res.json() : {}
  } catch {
    globalOverrides.value = {}
  }
  recomputeAndApply(locale)
}

// applyPageOverrides lädt die Overrides einer einzelnen Statusseite (Auth
// nötig, über useApi()) - wird ausschließlich aufgerufen, während der Nutzer
// im Admin-Bereich genau dieser Seite unterwegs ist (siehe AdminLayout.vue).
export async function applyPageOverrides(pageId, locale) {
  const { apiFetch } = useApi()
  try {
    const res = await apiFetch(`/api/pages/${pageId}/ui-overrides?language_code=${encodeURIComponent(locale)}`)
    pageOverrides.value = res || {}
  } catch {
    pageOverrides.value = {}
  }
  recomputeAndApply(locale)
}

// clearPageOverrides entfernt nur die Seiten-Ebene (z.B. beim Verlassen des
// Admin-Bereichs) - globale Overrides und Bundle-Default bleiben unberührt.
export function clearPageOverrides(locale) {
  pageOverrides.value = {}
  recomputeAndApply(locale)
}
