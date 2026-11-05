<script setup>
/*
  Mandanten-Übersicht (Kern-Screen der Enterprise-App), nur für Global Admins.
  Route: { path: 'overview', component: AdminOverview }  in router/index.js
  Backend: GET /api/admin/overview  (handlers/overview.go, RequireGlobalAdmin)

  Redesign Stufe 4b: PageHeader (Seitentitel = h1), KpiCard
  (nur „Offene Vorfälle“ > 0 ist rot), Tabelle über ResponsiveTable/dataTable,
  StatusBadge statt Inline-Farben, ListState/EmptyState für Lade-/Leerfall.

  Richtung C: Die Tabelle steht in einer Karte „Alle Statusseiten“
  mit Suche im Kartenkopf (ListCard/SearchBox), wie die übrigen Admin-Listen.
  Die Übersicht ist mandantenübergreifend und hat deshalb keine Brotkrumen
  mit Seitenname; Aktionen je Zeile gibt es nicht (kein Zeilenmenü).
*/
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { statusMeta } from '../../utils/status'
import PageHeader from '../../components/PageHeader.vue'
import KpiCard from '../../components/KpiCard.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import ListState from '../../components/ListState.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import EmptyState from '../../components/EmptyState.vue'
import { useListQuery } from '../../composables/useListQuery'

const { apiFetch } = useApi()
const { t } = useI18n()

const pages   = ref([])
const totals  = ref({ total_pages: 0, total_open_incidents: 0, total_subscribers: 0, worst_status: 'operational' })
const loading = ref(true)
const errorMsg = ref('')

// Suche im Kartenkopf: Name und Kurzname der Statusseite.
const { query, rows, searching } = useListQuery(pages, { textOf: p => `${p.name} ${p.slug}` })

const initials = (name) => name.split(' ').map(w => w[0]).join('').slice(0, 2).toUpperCase()

const loadData = async () => {
  loading.value = true
  errorMsg.value = ''
  try {
    const data = await apiFetch('/api/admin/overview')
    pages.value  = data.pages
    totals.value = data
  } catch (err) {
    errorMsg.value = t('overview.loadError') + err.message
  } finally {
    loading.value = false
  }
}
onMounted(loadData)

function fmt(n) { return n >= 1000 ? (n / 1000).toFixed(1).replace('.', ',') + 'k' : String(n) }

// Gesamtstatus als Text (ein unbekannter Wert zählt wie bisher als „Betriebsbereit“).
const overallLabel = computed(() => t(statusMeta(totals.value.worst_status).labelKey ?? 'status.operational'))
const openIncidents = computed(() => totals.value.total_open_incidents ?? 0)

const columns = computed(() => [
  { key: 'name', label: t('overview.tableStatusPage') },
  { key: 'status', label: t('overview.tableStatus') },
  { key: 'components', label: t('overview.tableComponents'), numeric: true },
  { key: 'subscribers', label: t('overview.tableSubscribers'), numeric: true },
  { key: 'open_incidents', label: t('overview.tableOpenIncidents'), numeric: true },
])
</script>

<template>
  <div>
    <PageHeader :level="2" :title="t('overview.title')" :subtitle="t('overview.subtitle')" />

    <div v-if="errorMsg" class="alert alert-error mb-16">{{ errorMsg }}</div>

    <!-- KPI-Zeile: nur „Offene Vorfälle“ > 0 schlägt Alarm -->
    <div class="kpi-grid">
      <KpiCard :label="t('overview.kpiActivePages')" :value="String(totals.total_pages ?? 0)" />
      <KpiCard :label="t('overview.kpiOpenIncidents')" :value="String(openIncidents)" :alert="openIncidents > 0" />
      <KpiCard :label="t('overview.kpiTotalSubscribers')" :value="fmt(totals.total_subscribers ?? 0)" />
      <KpiCard :label="t('overview.kpiOverallStatus')" :value="overallLabel" />
    </div>

    <!-- Tabelle: Status je Mandant -->
    <ListCard :title="t('overview.cardTitle')">
      <template #tools>
        <SearchBox v-model="query" :label="t('overview.searchLabel')" :placeholder="t('overview.searchPlaceholder')" />
      </template>
      <p v-if="searching" class="sr-only" role="status">{{ t('patterns.searchResult', { n: rows.length }, rows.length) }}</p>
      <ResponsiveTable v-if="rows.length" data-table :columns="columns" :items="rows">
        <template #cell-name="{ item: p }">
          <router-link class="cell-name" :to="`/admin/incidents?page=${p.id}`">
            <span class="avatar" aria-hidden="true">{{ initials(p.name) }}</span>
            <span class="cell-name-txt">
              <span class="row-title">{{ p.name }}</span>
              <span class="row-sub">/{{ p.slug }}</span>
            </span>
          </router-link>
        </template>
        <template #cell-status="{ item: p }"><StatusBadge :status="p.status" /></template>
        <template #cell-open_incidents="{ item: p }">
          <span :class="{ 'open-incidents': p.open_incidents }">{{ p.open_incidents }}</span>
        </template>
      </ResponsiveTable>
      <EmptyState v-else-if="pages.length" :title="t('overview.noResults')" :text="t('overview.noResultsText')" />
      <ListState v-else :loading="loading" :count="pages.length" :empty-text="t('overview.empty')" icon="grid" />
    </ListCard>
  </div>
</template>

<style scoped>
.kpi-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: var(--sp-4);
  margin-bottom: var(--sp-5);
}

/* Smartphone: 2 Spalten */
@media (min-width: 480px) {
  .kpi-grid { grid-template-columns: repeat(2, 1fr); }
}

/* Desktop: 4 Spalten */
@media (min-width: 1200px) {
  .kpi-grid { grid-template-columns: repeat(4, 1fr); }
}

.cell-name { display: inline-flex; align-items: center; gap: 11px; min-width: 0; color: var(--text); text-decoration: none; }
.cell-name:hover .row-title { text-decoration: underline; }
.cell-name-txt { display: flex; flex-direction: column; min-width: 0; }
.avatar {
  width: 32px; height: 32px; border-radius: var(--r-md); flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: var(--fs-xs); font-weight: 700; color: var(--primary-text); /* dunkel heller */
  background: var(--primary-soft);
}
.row-title { font-weight: 600; }
.row-sub { font-family: var(--font-mono); font-size: var(--fs-xs); color: var(--text-3); }
.open-incidents { font-weight: 700; color: var(--major-text); }
.mb-16 { margin-bottom: var(--sp-4); }
</style>
