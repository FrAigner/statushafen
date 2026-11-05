import { ref, computed } from 'vue'
import { useApi } from './useApi'
import { useAuth } from './useAuth'
import { useTenant } from './useTenant'

// Admin-Nachrichtencenter (Glocke in der Kopfzeile). Zustand und
// Abrufe für die Glocke. Nachrichten der aktiven Statusseite kommen von
// /api/pages/:id/admin-notifications, systemweite (nur Global Admins) von
// /api/admin-notifications; beide Quellen werden gemischt.
//
// Antworten, die erst nach einem Mandantenwechsel eintreffen, werden
// verworfen (Muster Ausfall-Banner): jede Anfrage merkt sich die
// Seite und eine Laufnummer und schreibt ihr Ergebnis nur, wenn beides noch
// stimmt.

const LIST_LIMIT = 20

export function useAdminNotifications() {
  const { apiFetch } = useApi()
  const { isGlobalAdmin, hasPerm } = useAuth()
  const { activeTenantId } = useTenant()

  const items = ref([])
  const unreadCount = ref(0)
  const loading = ref(false)
  const failed = ref(false)

  // Die Glocke gibt es für Global Admins und Rollen, die das Backend ebenfalls
  // bedient (external_sources:update oder subscribers:update auf der aktiven Seite).
  const enabled = computed(() => isGlobalAdmin.value || hasPerm('external_sources:update') || hasPerm('subscribers:update'))

  const sources = () => {
    const list = []
    if (activeTenantId.value) list.push(`/api/pages/${activeTenantId.value}/admin-notifications`)
    if (isGlobalAdmin.value) list.push('/api/admin-notifications')
    return list
  }

  let countRun = 0
  let listRun = 0
  const stillCurrent = (pageId) => String(pageId) === String(activeTenantId.value)

  const refreshCount = async () => {
    const pageId = activeTenantId.value
    const run = ++countRun
    const bases = enabled.value ? sources() : []
    if (!bases.length) {
      unreadCount.value = 0
      return
    }
    try {
      const results = await Promise.all(bases.map(b => apiFetch(`${b}/unread-count`)))
      if (run !== countRun || !stillCurrent(pageId)) return
      unreadCount.value = results.reduce((sum, r) => sum + (Number.isFinite(r?.unread_count) ? r.unread_count : 0), 0)
    } catch (err) {
      // Das Polling darf die Seite nicht stören: Der letzte Stand bleibt stehen,
      // die nächste Runde versucht es erneut.
      if (run === countRun) console.error('Failed to load admin notification count', err)
    }
  }

  const load = async () => {
    const pageId = activeTenantId.value
    const run = ++listRun
    const bases = enabled.value ? sources() : []
    loading.value = true
    failed.value = false
    try {
      const results = await Promise.all(bases.map(async (base) => {
        const res = await apiFetch(`${base}?limit=${LIST_LIMIT}`)
        return (Array.isArray(res?.items) ? res.items : []).map(it => ({ ...it, base }))
      }))
      if (run !== listRun || !stillCurrent(pageId)) return
      items.value = results.flat()
        .sort((a, b) => String(b.last_seen_at).localeCompare(String(a.last_seen_at)) || b.id - a.id)
        .slice(0, LIST_LIMIT)
    } catch (err) {
      if (run !== listRun) return
      failed.value = true
      console.error('Failed to load admin notifications', err)
    } finally {
      if (run === listRun) loading.value = false
    }
  }

  const markRead = async (item) => {
    if (item.read) return
    item.read = true
    if (!item.resolved_at) unreadCount.value = Math.max(0, unreadCount.value - 1)
    try {
      await apiFetch(`${item.base}/${item.id}/read`, { method: 'POST' })
    } catch (err) {
      item.read = false
      console.error('Failed to mark notification as read', err)
      await refreshCount()
    }
  }

  const markAllRead = async () => {
    const before = items.value.map(it => it.read)
    items.value.forEach((it) => { it.read = true })
    unreadCount.value = 0
    try {
      await Promise.all(sources().map(b => apiFetch(`${b}/read-all`, { method: 'POST' })))
    } catch (err) {
      items.value.forEach((it, i) => { it.read = before[i] })
      console.error('Failed to mark all notifications as read', err)
      await refreshCount()
    }
  }

  // Beim Mandantenwechsel: alte Liste und Zähler sofort leeren, laufende
  // Antworten werden über die Laufnummern ungültig.
  const reset = () => {
    countRun++
    listRun++
    items.value = []
    unreadCount.value = 0
    failed.value = false
    loading.value = false
  }

  return { items, unreadCount, loading, failed, enabled, refreshCount, load, markRead, markAllRead, reset }
}
