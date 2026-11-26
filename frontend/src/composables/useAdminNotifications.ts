import { ref, computed } from 'vue'
import { useApi } from './useApi'
import { useAuth } from './useAuth'
import { useTenant } from './useTenant'
import type { TenantId } from './useTenant'
import type { AdminNotification } from '../types/api'

/** A message of the bell plus the route base it came from (page-specific or system-wide). */
export interface AdminNotificationEntry extends AdminNotification {
  base: string
}

// Admin notification center (bell in the header). State and requests for
// the bell. Messages of the active status page come from
// /api/pages/:id/admin-notifications, system-wide ones (global admins only)
// from /api/admin-notifications; both sources are merged.
//
// Responses that only arrive after a tenant switch are discarded (pattern of
// the outage banner): every request remembers the page and a run number and
// only writes its result if both still match.

const LIST_LIMIT = 20

export function useAdminNotifications() {
  const { apiFetch } = useApi()
  const { isGlobalAdmin, hasPerm } = useAuth()
  const { activeTenantId } = useTenant()

  const items = ref<AdminNotificationEntry[]>([])
  const unreadCount = ref(0)
  const loading = ref(false)
  const failed = ref(false)

  // The bell exists for global admins and for roles the backend serves as well
  // (external_sources:update or subscribers:update on the active page).
  const enabled = computed(() => isGlobalAdmin.value || hasPerm('external_sources:update') || hasPerm('subscribers:update'))

  const sources = () => {
    const list: string[] = []
    if (activeTenantId.value) list.push(`/api/pages/${activeTenantId.value}/admin-notifications`)
    if (isGlobalAdmin.value) list.push('/api/admin-notifications')
    return list
  }

  let countRun = 0
  let listRun = 0
  const stillCurrent = (pageId: TenantId | null) => String(pageId) === String(activeTenantId.value)

  const refreshCount = async () => {
    const pageId = activeTenantId.value
    const run = ++countRun
    const bases = enabled.value ? sources() : []
    if (!bases.length) {
      unreadCount.value = 0
      return
    }
    try {
      const results = await Promise.all(bases.map(b => apiFetch<{ unread_count?: number } | null>(`${b}/unread-count`)))
      if (run !== countRun || !stillCurrent(pageId)) return
      unreadCount.value = results.reduce((sum, r) => sum + (Number.isFinite(r?.unread_count) ? r!.unread_count! : 0), 0)
    } catch (err) {
      // Polling must not disturb the page: the last state stays,
      // the next round tries again.
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
        const res = await apiFetch<{ items?: AdminNotification[] } | null>(`${base}?limit=${LIST_LIMIT}`)
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

  const markRead = async (item: AdminNotificationEntry) => {
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
      items.value.forEach((it, i) => { it.read = before[i]! })
      console.error('Failed to mark all notifications as read', err)
      await refreshCount()
    }
  }

  // On a tenant switch: clear the old list and counter immediately; pending
  // responses become invalid via the run numbers.
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
