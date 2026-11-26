import { ref } from 'vue'
import i18n, { getBaseMessages, isLocaleLoaded, loadLocaleMessages } from './index'
import type { LocaleMessages } from './index'
import { useApi } from '../composables/useApi'

// UI translation overrides: admin-editable overrides for app UI texts,
// without a code deploy. Merge order bundle < global override < page override,
// recomputed completely on every change and applied via setLocaleMessage()
// (full replacement instead of mergeLocaleMessage) - incremental merging would
// offer no clean way to remove only the previous page's overrides again when
// switching pages.

/** Flat map of dotted key paths to texts, e.g. { "nav.items.sso": "SSO" }. */
export type FlatMessages = Record<string, unknown>

/** Nested message object as vue-i18n expects it. */
export interface NestedMessages {
  [key: string]: unknown
}

// Module-scope singleton state, analogous to the pattern in useTenant.ts.
const globalOverrides = ref<FlatMessages>({})
const pageOverrides = ref<FlatMessages>({})

// flatten turns a nested message object (like de.json/en.json) into a flat
// map of "dotted" key paths to values, e.g.
// { nav: { items: { sso: "SSO" } } } -> { "nav.items.sso": "SSO" }.
export function flatten(obj: object, prefix = ''): FlatMessages {
  return Object.entries(obj).reduce<FlatMessages>((acc, [key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      Object.assign(acc, flatten(value as object, path))
    } else {
      acc[path] = value
    }
    return acc
  }, {})
}

// unflatten is the inverse of flatten - it rebuilds from a flat
// "dotted" key map the nested message object that vue-i18n expects as
// messages.
//
// Paths with a __proto__/constructor/prototype segment are
// dropped, otherwise a malicious override would pollute
// Object.prototype.
const FORBIDDEN_PARTS = new Set(['__proto__', 'constructor', 'prototype'])

//
// Review finding 1: a key that is the path prefix of another key
// (e.g. "nav" next to "nav.items.sso") would replace the subtree or
// turn a leaf into an object. Such prefix entries are dropped
// regardless of order; the deeper keys stay.
export function unflatten(flatMap: FlatMessages): NestedMessages {
  const result: NestedMessages = {}
  const paths = Object.keys(flatMap)
  for (const [path, value] of Object.entries(flatMap)) {
    if (paths.some((other) => other.startsWith(`${path}.`))) continue
    const parts = path.split('.')
    if (parts.some((part) => FORBIDDEN_PARTS.has(part))) continue
    let node = result
    for (let i = 0; i < parts.length - 1; i++) {
      const part = parts[i] as string
      if (!node[part] || typeof node[part] !== 'object') {
        node[part] = {}
      }
      node = node[part] as NestedMessages
    }
    node[parts.at(-1) as string] = value
  }
  return result
}

function recomputeAndApply(locale: string): void {
  const base = getBaseMessages(locale)
  if (!base) return
  const merged = { ...flatten(base), ...globalOverrides.value, ...pageOverrides.value }
  // The bundle texts with overrides on top: still the shape of the bundle.
  i18n.global.setLocaleMessage(locale, unflatten(merged) as LocaleMessages)
}

// applyGlobalOverrides loads the global overrides (public endpoint,
// no auth needed - anonymous visitors should see the globally customized
// texts too) and applies them. If the request fails, the bundle default
// stays instead of blocking the app at startup.
export async function applyGlobalOverrides(locale: string): Promise<void> {
  // Lazily loadable UI language: load the texts before applying.
  if (!isLocaleLoaded(locale)) await loadLocaleMessages(locale)
  try {
    const res = await fetch(`/api/public/ui-overrides?language_code=${encodeURIComponent(locale)}`)
    globalOverrides.value = res.ok ? await res.json() : {}
  } catch {
    globalOverrides.value = {}
  }
  recomputeAndApply(locale)
}

// applyPageOverrides loads the overrides of a single status page (auth
// needed, via useApi()) - only called while the user is in the admin area of
// exactly this page (see AdminLayout.vue).
export async function applyPageOverrides(pageId: number | string, locale: string): Promise<void> {
  const { apiFetch } = useApi()
  try {
    const res = await apiFetch<FlatMessages | null>(`/api/pages/${pageId}/ui-overrides?language_code=${encodeURIComponent(locale)}`)
    pageOverrides.value = res || {}
  } catch {
    pageOverrides.value = {}
  }
  recomputeAndApply(locale)
}

// clearPageOverrides removes only the page layer (e.g. when leaving the
// admin area) - global overrides and the bundle default stay untouched.
export function clearPageOverrides(locale: string): void {
  pageOverrides.value = {}
  recomputeAndApply(locale)
}
