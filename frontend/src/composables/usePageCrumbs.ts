import { computed, toValue } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTenant } from './useTenant'

// Richtung C: Brotkrumen „Seitenname › Seite“ für alle Admin-Seiten
// (ersetzt useAdminPageHead, useAdminCrumbs und die Fassung aus dem System-PR).
// Seitenbezogene Seiten beginnen mit dem Namen der aktiven Statusseite (Link
// zur Übersicht), globale Seiten (global: true) mit „Administration“. Ohne
// bekannten Seitennamen entfällt die erste Krume. `title`, `global` und `extra`
// dürfen Wert, Ref oder Getter sein (reagieren so auf Sprachwechsel); `extra`
// hängt weitere Krumen vor die aktuelle Seite (Einträge mit `to` sind Links).
export function usePageCrumbs(title, { global = false, extra = [] } = {}) {
  const { t } = useI18n()
  const { activeTenantId, myPages } = useTenant()
  return computed(() => {
    const pageName = myPages.value.find(p => String(p.ID) === String(activeTenantId.value))?.Name || ''
    const root = toValue(global) ? t('patterns.adminRoot') : pageName
    return [
      ...(root ? [{ label: root, to: '/admin' }] : []),
      ...toValue(extra),
      { label: toValue(title) },
    ]
  })
}
