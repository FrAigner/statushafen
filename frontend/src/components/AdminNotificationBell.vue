<script setup>
/*
  Nachrichtenglocke der Admin-Kopfzeile. Knopf mit Badge (Zahl der
  ungelesenen Nachrichten, ab 10 als „9+“; die genaue Zahl steht im aria-label)
  und ein Panel darunter. Das Panel ist ein nicht-modaler Dialog: Der Fokus
  springt beim Öffnen hinein, Escape schließt und gibt den Fokus an den Knopf
  zurück, Tab aus dem Panel heraus und ein Klick daneben schließen es ebenfalls.
  Der Zähler wird alle 60 s abgefragt, aber nur bei sichtbarem Tab; beim
  Mandantenwechsel sofort neu (Antworten der alten Seite werden verworfen,
  siehe useAdminNotifications).
*/
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'
import EmptyState from './EmptyState.vue'
import RelativeTime from './RelativeTime.vue'
import { useTenant } from '../composables/useTenant'
import { useAdminNotifications } from '../composables/useAdminNotifications'

const POLL_MS = 60_000
const BADGE_MAX = 9
const SEVERITY_ICON = { error: 'alert-circle', warning: 'alert-triangle', info: 'info' }

const { t, te } = useI18n()
const { activeTenantId } = useTenant()
const { items, unreadCount, loading, failed, enabled, refreshCount, load, markRead, markAllRead, reset } = useAdminNotifications()

const open = ref(false)
const rootRef = ref(null)
const buttonRef = ref(null)
const panelRef = ref(null)

const badge = computed(() => (unreadCount.value > BADGE_MAX ? `${BADGE_MAX}+` : String(unreadCount.value)))
const buttonLabel = computed(() => (unreadCount.value > 0
  ? t('adminNotifications.bellLabelUnread', { n: unreadCount.value })
  : t('adminNotifications.bellLabel')))

const severityIcon = (item) => SEVERITY_ICON[item.severity] || SEVERITY_ICON.info
const severityText = (item) => t(`adminNotifications.severity.${SEVERITY_ICON[item.severity] ? item.severity : 'info'}`)
// Titel und Text kommen als i18n-Schlüssel vom Server; ein unbekannter
// Schlüssel (neuere Server-Version) zeigt einen neutralen Text statt des Schlüssels.
const textFor = (key, fallbackKey, params) => (te(key) ? t(key, params || {}) : t(fallbackKey))

const focusables = () => Array.from(panelRef.value?.querySelectorAll('a[href], button') || [])

const close = ({ restoreFocus = false } = {}) => {
  open.value = false
  if (restoreFocus) buttonRef.value?.focus()
}

const openPanel = async () => {
  open.value = true
  load()
  await nextTick()
  const first = focusables()[0] || panelRef.value
  first?.focus()
}

const toggle = () => (open.value ? close() : openPanel())

const onKeydown = (e) => {
  if (e.key === 'Escape' && open.value) {
    e.stopPropagation()
    close({ restoreFocus: true })
  }
}

const onFocusout = (e) => {
  if (e.relatedTarget && !rootRef.value?.contains(e.relatedTarget)) close()
}

const onDocumentClick = (e) => {
  if (open.value && !rootRef.value?.contains(e.target)) close()
}

// Eine Nachricht öffnen: Link folgen, als gelesen markieren, Panel schließen.
const onFollow = (item) => {
  markRead(item)
  close()
}

// --- Polling ---------------------------------------------------------------
let timer = null
const stopPolling = () => {
  if (timer) clearInterval(timer)
  timer = null
}
const startPolling = () => {
  stopPolling()
  if (document.visibilityState === 'visible') timer = setInterval(refreshCount, POLL_MS)
}
const onVisibility = () => {
  if (document.visibilityState === 'visible') {
    refreshCount()
    startPolling()
  } else {
    stopPolling()
  }
}

onMounted(() => {
  refreshCount()
  startPolling()
  document.addEventListener('visibilitychange', onVisibility)
  document.addEventListener('click', onDocumentClick)
})
onBeforeUnmount(() => {
  stopPolling()
  document.removeEventListener('visibilitychange', onVisibility)
  document.removeEventListener('click', onDocumentClick)
})

// Mandantenwechsel: Stand der alten Seite sofort verwerfen und neu abfragen.
watch(activeTenantId, () => {
  reset()
  refreshCount()
  if (open.value) load()
})
</script>

<template>
  <div v-if="enabled" ref="rootRef" class="notif" @keydown="onKeydown" @focusout="onFocusout">
    <button
      ref="buttonRef"
      class="icon-btn notif-button"
      type="button"
      :aria-label="buttonLabel"
      aria-haspopup="dialog"
      aria-controls="admin-notifications-panel"
      :aria-expanded="open"
      @click="toggle"
    >
      <AppIcon name="bell" :size="16" />
      <span v-if="unreadCount > 0" class="notif-badge" aria-hidden="true">{{ badge }}</span>
    </button>

    <div
      v-if="open"
      id="admin-notifications-panel"
      ref="panelRef"
      class="notif-panel"
      role="dialog"
      tabindex="-1"
      :aria-label="t('adminNotifications.title')"
    >
      <div class="notif-head">
        <strong class="notif-title">{{ t('adminNotifications.title') }}</strong>
        <button v-if="unreadCount > 0" type="button" class="notif-action" @click="markAllRead">
          {{ t('adminNotifications.markAllRead') }}
        </button>
      </div>

      <p v-if="loading && items.length === 0" class="notif-state" role="status" aria-busy="true">{{ t('common.loading') }}</p>
      <p v-else-if="failed && items.length === 0" class="notif-state" role="alert">{{ t('adminNotifications.loadError') }}</p>
      <EmptyState
        v-else-if="items.length === 0"
        icon="bell"
        :title="t('adminNotifications.emptyTitle')"
        :text="t('adminNotifications.emptyText')"
      />
      <ul v-else class="notif-list">
        <li v-for="item in items" :key="`${item.base}-${item.id}`" class="notif-item" :class="{ 'is-unread': !item.read && !item.resolved_at }">
          <div class="notif-meta">
            <span class="notif-severity" :class="`is-${SEVERITY_ICON[item.severity] ? item.severity : 'info'}`">
              <AppIcon :name="severityIcon(item)" :size="14" />
              {{ severityText(item) }}
            </span>
            <span v-if="item.resolved_at" class="notif-resolved">
              <AppIcon name="check-circle" :size="14" />
              {{ t('adminNotifications.resolved') }}
            </span>
            <span v-if="item.count > 1" class="notif-count">{{ t('adminNotifications.count', { n: item.count }) }}</span>
            <RelativeTime class="notif-time" :value="item.last_seen_at" />
          </div>
          <p class="notif-item-title">{{ textFor(item.title_key, 'adminNotifications.unknownTitle', item.params) }}</p>
          <p class="notif-item-body">{{ textFor(item.body_key, 'adminNotifications.unknownBody', item.params) }}</p>
          <div class="notif-item-actions">
            <router-link v-if="item.link_path" class="notif-action" :to="item.link_path" @click="onFollow(item)">
              {{ t('adminNotifications.open') }}
            </router-link>
            <button v-if="!item.read" type="button" class="notif-action" @click="markRead(item)">
              {{ t('adminNotifications.markRead') }}
            </button>
          </div>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.notif { position: relative; }
.notif-button { position: relative; }
.notif-badge {
  position: absolute;
  inset-block-start: -6px;
  inset-inline-end: -6px;
  min-inline-size: 18px;
  block-size: 18px;
  padding: 0 4px;
  box-sizing: border-box;
  display: grid;
  place-items: center;
  border-radius: 9px;
  background: var(--major);
  color: var(--icon-on);
  font-size: 0.7rem;
  font-weight: 700;
  line-height: 1;
}

.notif-panel {
  position: absolute;
  inset-block-start: calc(100% + 6px);
  inset-inline-end: 0;
  z-index: 400;
  inline-size: 380px;
  max-inline-size: calc(100vw - 24px);
  max-block-size: min(70vh, 560px);
  overflow-y: auto;
  background: var(--surface);
  color: var(--text);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-pop);
}
.notif-panel:focus { outline: none; }
.notif-panel :focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }

.notif-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-2);
  padding: var(--sp-2) var(--sp-3);
  border-block-end: 1px solid var(--border);
}
.notif-title { font-size: 0.9rem; font-weight: 600; }
.notif-state { margin: 0; padding: var(--sp-6) var(--sp-4); text-align: center; color: var(--text-2); }

.notif-list { list-style: none; margin: 0; padding: 0; }
.notif-item {
  display: flex;
  flex-direction: column;
  gap: var(--sp-1);
  padding: var(--sp-3);
  border-block-end: 1px solid var(--border);
}
.notif-item:last-child { border-block-end: 0; }
.notif-item.is-unread { background: var(--primary-soft); }

.notif-meta { display: flex; flex-wrap: wrap; align-items: center; gap: var(--sp-1) var(--sp-3); font-size: 0.78rem; color: var(--text-2); }
.notif-severity, .notif-resolved { display: inline-flex; align-items: center; gap: 4px; font-weight: 600; }
.notif-severity.is-error { color: var(--major-text); }
.notif-severity.is-warning { color: var(--deg-text); }
.notif-severity.is-info { color: var(--primary-text); }
.notif-resolved { color: var(--ok-text); }
.notif-time { margin-inline-start: auto; color: var(--text-2); }
.notif-item-title { margin: 0; font-size: 0.9rem; font-weight: 600; overflow-wrap: anywhere; }
.notif-item-body { margin: 0; font-size: 0.85rem; color: var(--text-2); overflow-wrap: anywhere; }
.notif-item-actions { display: flex; flex-wrap: wrap; gap: var(--sp-2); margin-block-start: var(--sp-1); }

.notif-action {
  display: inline-flex;
  align-items: center;
  min-block-size: 32px;
  padding: 0 var(--sp-3);
  background: var(--surface);
  color: var(--text);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-md);
  font-family: var(--font-sans);
  font-size: 0.82rem;
  text-decoration: none;
  cursor: pointer;
}
.notif-action:hover { background: var(--surface-2); text-decoration: none; }

/* Schmale Bildschirme: Das Panel hängt an der Kopfzeile statt am Knopf, damit es
   die volle Breite nutzen kann (der Knopf sitzt mitten in der Zeile). */
@media (max-width: 768px) {
  .notif { position: static; }
  .notif-panel {
    /* unterhalb der umbrochenen Steuerzeile (Theme, Glocke, Sprache, Benutzer) */
    inset-block-start: calc(var(--ctl-h) * 2 + var(--sp-6));
    inset-inline: var(--sp-2);
    inline-size: auto;
    max-inline-size: none;
  }
}

@media (prefers-reduced-motion: no-preference) {
  .notif-action { transition: background 0.15s; }
}
</style>
