<script setup>
/*
  Audit-Log (revisionssicherer Aktivitätsverlauf).
  Route:   { path: 'audit', component: AdminAuditLog }
  Backend: GET /api/admin/audit?page=&limit=&category=&q=   (handlers/audit.go)
  Zugriff: nur Global-Admin (middleware.RequireGlobalAdmin()).
*/
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import AppIcon from '../../components/AppIcon.vue'
import PageHeader from '../../components/PageHeader.vue'
import ListCard from '../../components/ListCard.vue'
import ListState from '../../components/ListState.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import SearchBox from '../../components/SearchBox.vue'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { toIntlLocale } from '../../utils/datetime'
import { formatRelative } from '../../utils/relativeTime'

const { apiFetch } = useApi()
const { t, te, locale } = useI18n()
const breadcrumbs = usePageCrumbs(computed(() => t('auditLog.title')), { global: true })

const columns = computed(() => [
  { key: 'created_at', label: t('auditLog.columnTime') },
  { key: 'actor_email', label: t('auditLog.columnActor') },
  { key: 'action', label: t('auditLog.columnAction') },
  { key: 'category', label: t('auditLog.columnCategory') },
  { key: 'ip', label: t('auditLog.columnIp') },
])

const items    = ref([])
const total    = ref(0)
const page     = ref(1)
const limit    = 25
const search   = ref('')
const activeCat = ref('')
const loading  = ref(false)
const errorMsg = ref('')

const CATS = computed(() => [
  { id: 'auth',     label: t('auditLog.catAuth') },
  { id: 'incident', label: t('auditLog.catIncident') },
  { id: 'config',   label: t('auditLog.catConfig') },
  { id: 'user',     label: t('auditLog.catUser') },
  // Auch die übrigen vom Backend geschriebenen Kategorien
  // (models.AuditCategories) haben einen Filter-Chip.
  { id: 'subscriber',  label: t('auditLog.catSubscriber') },
  { id: 'notice',      label: t('auditLog.catNotice') },
  { id: 'maintenance', label: t('auditLog.catMaintenance') },
  { id: 'system',      label: t('auditLog.catSystem') },
])
// Stufe 4d: Die Kategoriefarben stehen als Klassen (cat--<id>) im
// Style-Block auf Tokens, nicht mehr als Inline-Farbe; unbekannte Kategorien
// bleiben neutral (cat--other).
// Jede bekannte Kategorie hat einen Chip und eine übersetzte
// Beschriftung; wirklich unbekannte (neuer Wert im Backend) zeigen den Rohwert.
const catMeta = (id) => CATS.value.find(c => c.id === id) ?? { id: 'other', label: id }

// Die Aktion kommt als technischer Schlüssel ("subscriber.deleted")
// aus dem Backend und wird über auditLog.actions.<Schlüssel> übersetzt;
// unbekannte (neue) Aktionen zeigen weiter den Rohwert.
const actionLabel = (action) => (action && te(`auditLog.actions.${action}`) ? t(`auditLog.actions.${action}`) : action)

const initials = (email) => (email || 'SY').replace(/@.*/, '').split(/[.\-_]/).map(s => s[0]).join('').slice(0, 2).toUpperCase()
const isoTime = (unix) => (unix ? new Date(unix * 1000).toISOString() : undefined)
const fmtTime = (unix) => unix ? new Date(unix * 1000).toLocaleString(toIntlLocale(locale.value), { dateStyle: 'short', timeStyle: 'short' }) : '—'

let debounce
const load = async () => {
  loading.value = true
  errorMsg.value = ''
  try {
    const params = new URLSearchParams({ page: page.value, limit })
    if (activeCat.value) params.set('category', activeCat.value)
    if (search.value)    params.set('q', search.value)
    const data = await apiFetch(`/api/admin/audit?${params.toString()}`)
    items.value = data.items || []
    total.value = data.total || 0
  } catch (err) {
    errorMsg.value = t('auditLog.loadError') + err.message
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch([activeCat, page], load)
watch(search, () => { clearTimeout(debounce); debounce = setTimeout(() => { page.value = 1; load() }, 300) })

const toggleCat = (id) => { activeCat.value = activeCat.value === id ? '' : id; page.value = 1 }
const totalPages = () => Math.max(1, Math.ceil(total.value / limit))
</script>

<template>
  <div>
    <PageHeader :level="2" :title="t('auditLog.title')" :subtitle="t('auditLog.subtitle')" :breadcrumbs="breadcrumbs" />

    <div v-if="errorMsg" class="alert alert-error mb-16">{{ errorMsg }}</div>

    <!-- Richtung C: Karte mit Suche und Kategorie-Filter im Kopf. -->
    <ListCard :title="t('auditLog.cardTitle')">
      <template #tools>
        <SearchBox v-model="search" :label="t('auditLog.searchLabel')" :placeholder="t('auditLog.searchPlaceholder')" />
        <div class="cat-chips" role="group" :aria-label="t('auditLog.columnCategory')">
          <button
            v-for="c in CATS" :key="c.id"
            type="button"
            class="chip" :class="[`cat--${c.id}`, { active: activeCat === c.id }]"
            :aria-pressed="activeCat === c.id"
            @click="toggleCat(c.id)"
          >{{ c.label }}</button>
        </div>
      </template>

      <!-- Tabelle (ab 860 px) / Karten (darunter) -->
      <ListState v-if="items.length === 0" :loading="loading" :error="!!errorMsg" :count="0" :empty-text="t('auditLog.empty')" icon="clock" />
      <ResponsiveTable v-else data-table :columns="columns" :items="items" row-key="id">
        <template #cell-created_at="{ item }">
          <time class="time-cell" :datetime="isoTime(item.created_at)">
            <span class="ent-mono">{{ fmtTime(item.created_at) }}</span>
            <span class="time-cell__rel">{{ formatRelative(isoTime(item.created_at), locale) }}</span>
          </time>
        </template>
        <template #cell-actor_email="{ item }">
          <div class="actor">
            <span class="avatar" aria-hidden="true">{{ initials(item.actor_email) }}</span>
            <div class="min0">
              <div class="a-name">{{ item.actor_email || t('auditLog.systemActor') }}</div>
              <div class="a-role">{{ item.actor_role || t('auditLog.automationRole') }}</div>
            </div>
          </div>
        </template>
        <template #cell-action="{ item }">
          <div class="action">
            {{ actionLabel(item.action) }} <strong v-if="item.target_name">{{ item.target_name }}</strong>
          </div>
        </template>
        <template #cell-category="{ item }">
          <span class="ent-pill" :class="`cat--${catMeta(item.category).id}`">
            {{ catMeta(item.category).label }}
          </span>
        </template>
        <template #cell-ip="{ item }">
          <span class="ent-mono muted">{{ item.ip }}</span>
        </template>
      </ResponsiveTable>

      <!-- Pagination -->
      <div v-if="total > limit" class="pager">
        <button class="action-btn pager-btn" :disabled="page <= 1" @click="page--">
          <AppIcon name="arrow-left" :size="13" /> {{ t('auditLog.pagerBack') }}
        </button>
        <span class="pager-info">{{ t('auditLog.pagerInfo', { page, totalPages: totalPages(), total }) }}</span>
        <button class="action-btn pager-btn" :disabled="page >= totalPages()" @click="page++">
          {{ t('auditLog.pagerNext') }} <AppIcon name="arrow-right" :size="13" />
        </button>
      </div>
    </ListCard>
  </div>
</template>

<style scoped>
.mb-16 { margin-bottom: var(--sp-4); }

.cat-chips { display: flex; gap: 6px; flex-wrap: wrap; }
.time-cell { display: flex; flex-direction: column; gap: 2px; white-space: nowrap; }
.time-cell__rel { font-size: var(--fs-xs); color: var(--text-2); }

/* Kategorien: je ein Status-Ton (Text + weiche Fläche) aus den Tokens. Der
   Text steht immer im Chip bzw. in der Pille, die Farbe ist nur Zusatz. */
.cat--auth     { --cat-text: var(--maint-text);   --cat-soft: var(--maint-soft); }
.cat--incident { --cat-text: var(--major-text);   --cat-soft: var(--major-soft); }
.cat--config   { --cat-text: var(--primary-text); --cat-soft: var(--primary-soft); }
.cat--user     { --cat-text: var(--ok-text);      --cat-soft: var(--ok-soft); }
.cat--subscriber  { --cat-text: var(--maint-text);   --cat-soft: var(--maint-soft); }
.cat--notice      { --cat-text: var(--primary-text); --cat-soft: var(--primary-soft); }
.cat--maintenance { --cat-text: var(--major-text);   --cat-soft: var(--major-soft); }
.cat--system      { --cat-text: var(--ok-text);      --cat-soft: var(--ok-soft); }
.cat--other    { --cat-text: var(--text-2);       --cat-soft: var(--surface-2); }

.chip {
  font-size: var(--fs-xs); font-weight: 600; padding: 5px 11px; border-radius: 20px;
  border: 1px solid color-mix(in srgb, var(--cat-text) 35%, transparent);
  color: var(--cat-text); background: transparent; cursor: pointer; font-family: var(--font-sans);
}
.chip.active { background: var(--cat-soft); border-color: var(--cat-text); }
.chip:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
.ent-pill.cat--auth, .ent-pill.cat--incident, .ent-pill.cat--config, .ent-pill.cat--user,
.ent-pill.cat--subscriber, .ent-pill.cat--notice, .ent-pill.cat--maintenance, .ent-pill.cat--system, .ent-pill.cat--other {
  color: var(--cat-text); background: var(--cat-soft);
}

.muted { color: var(--text-3); }

.actor { display: flex; align-items: center; gap: 9px; min-width: 0; }
.avatar {
  width: 26px; height: 26px; border-radius: 50%; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: var(--fs-xs); font-weight: 600; color: var(--on-primary); background: var(--primary);
}
.min0 { min-width: 0; }
.a-name { font-size: var(--fs-sm); font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.a-role { font-size: var(--fs-xs); color: var(--text-3); }
.action { font-size: var(--fs-sm); min-width: 0; }

.pager { display: flex; align-items: center; justify-content: center; gap: var(--sp-4); padding: var(--sp-3) var(--sp-4); border-top: 1px solid var(--border); }
.pager-info { font-size: var(--fs-sm); color: var(--text-3); }
.pager-btn { display: inline-flex; align-items: center; gap: 6px; }
</style>
