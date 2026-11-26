import { computed, toValue } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTenant } from './useTenant'

/** A breadcrumb; entries with `to` are links. */
export interface Crumb {
  label: string
  to?: string
}

// Direction C: breadcrumbs "Page name › Page" for all admin pages (replaces
// useAdminPageHead, useAdminCrumbs and the version from the system PR).
// Page-specific pages start with the name of the active status page (link to
// the overview), global pages (global: true) with "Administration". Without a
// known page name the first crumb is dropped. `title`, `global` and `extra`
// may be a value, ref or getter (so they react to language switches); `extra`
// inserts further crumbs before the current page (entries with `to` are links).
export function usePageCrumbs(
  title: MaybeRefOrGetter<string>,
  { global = false, extra = [] }: { global?: MaybeRefOrGetter<boolean>; extra?: MaybeRefOrGetter<Crumb[]> } = {},
) {
  const { t } = useI18n()
  const { activeTenantId, myPages } = useTenant()
  return computed<Crumb[]>(() => {
    const pageName = myPages.value.find(p => String(p.ID) === String(activeTenantId.value))?.Name || ''
    const root = toValue(global) ? t('patterns.adminRoot') : pageName
    return [
      ...(root ? [{ label: root, to: '/admin' }] : []),
      ...toValue(extra),
      { label: toValue(title) },
    ]
  })
}
