<script setup>
/*
  In-App Monitoring-Dashboard (Golden Signals Schritt 3/4), nur für
  Global Admins. Zeigt dieselben Signale wie der Prometheus-Export aus
  Schritt 2/4 direkt in der App an – als Fallback/Quick-View, falls
  Grafana Cloud nicht verfügbar oder noch nicht eingerichtet ist.
  Route: { path: 'monitoring', component: AdminMonitoring }  in router/index.js
  Backend: GET /api/admin/monitoring  (handlers/monitoring.go, RequireGlobalAdmin)
*/
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import PageHeader from '../../components/PageHeader.vue'
import ListCard from '../../components/ListCard.vue'
import ListState from '../../components/ListState.vue'
import KpiCard from '../../components/KpiCard.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import SegmentControl from '../../components/SegmentControl.vue'
import { ERROR_RATE_MAJOR, errorLevel } from './monitoringThresholds.js'

const { apiFetch } = useApi()
const { t } = useI18n()
const breadcrumbs = usePageCrumbs(computed(() => t('monitoring.title')), { global: true })

const routes = ref([])
const totals = ref({
  request_count: 0, error_count: 0, error_rate: 0, requests_per_minute: 0,
  avg_latency_ms: 0, p50_latency_ms: 0, p95_latency_ms: 0, max_latency_ms: 0,
})
const uptimeSeconds = ref(0)
// Dashboard-Link kommt aus der Backend-Konfiguration (GRAFANA_DASHBOARD_URL); leer = kein Link.
const grafanaUrl = ref('')
const saturation = ref({
  goroutine_count: 0, heap_alloc_bytes: 0, heap_sys_bytes: 0, sys_bytes: 0, num_gc: 0,
  db_stats_available: false, db_max_open_connections: 0, db_open_connections: 0, db_in_use: 0, db_idle: 0,
})
// Schlüssel je Zeile für ResponsiveTable (Methode + Route ist eindeutig).
// Das Backend liefert alle registrierten Routen, auch ungenutzte (0 Requests).
// Filter „Alle“ / „Nur genutzte“; ungenutzte Zeilen zeigen „–“ statt 0,0 % / 0ms.
const usageFilter = ref('all')
const usageOptions = computed(() => [
  { value: 'all', label: t('monitoring.filterAll') },
  { value: 'used', label: t('monitoring.filterUsed') },
])
const unusedCount = computed(() => routes.value.filter(r => !r.request_count).length)
const routeRows = computed(() => routes.value
  .filter(r => usageFilter.value === 'all' || r.request_count > 0)
  .map(r => ({ ...r, rowKey: r.method + ' ' + r.route, unused: !r.request_count })))
const loading = ref(true)
const errorMsg = ref('')

const loadData = async () => {
  loading.value = true
  errorMsg.value = ''
  try {
    const data = await apiFetch('/api/admin/monitoring')
    routes.value = data.routes ?? []
    totals.value = data.totals ?? totals.value
    uptimeSeconds.value = data.uptime_seconds ?? 0
    grafanaUrl.value = data.grafana_dashboard_url ?? ''
    saturation.value = data.saturation ?? saturation.value
  } catch (err) {
    errorMsg.value = t('monitoring.loadError') + err.message
  } finally {
    loading.value = false
  }
}
onMounted(loadData)

const fmtMs = (ms) => (ms == null ? '–' : ms >= 1000 ? (ms / 1000).toFixed(2) + 's' : Math.round(ms) + 'ms')
const fmtPct = (ratio) => (ratio == null ? '–' : (ratio * 100).toFixed(1) + '%')
const fmtRate = (perMin) => (perMin == null ? '–' : perMin < 10 ? perMin.toFixed(2) : Math.round(perMin))
const fmtUptime = (s) => {
  if (!s) return '–'
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60)
  return h > 0 ? `${h}h ${m}min` : `${m}min`
}
// Schwellen (gelb ab 5 %, rot ab 10 %) und Stufenlogik: monitoringThresholds.js.
// Grober Wert, kein SLO; dient nur der visuellen Orientierung im Dashboard.
// Stufe 4d: Die Stufe (major/degraded) geht an StatusBadge (Form +
// Farbe + Text), KpiCard färbt nur ab "major" (alert); keine Regenbogen-Kacheln.

const kpis = computed(() => [
  { label: t('monitoring.kpiRequests'),            value: String(totals.value.request_count ?? 0) },
  { label: t('monitoring.kpiErrorRate'),           value: fmtPct(totals.value.error_rate), alert: (totals.value.error_rate ?? 0) >= ERROR_RATE_MAJOR },
  { label: t('monitoring.kpiAvgLatency'),          value: fmtMs(totals.value.avg_latency_ms) },
  { label: t('monitoring.kpiP95Latency'),          value: fmtMs(totals.value.p95_latency_ms) },
  { label: t('monitoring.kpiRequestsPerMinute'),   value: String(fmtRate(totals.value.requests_per_minute)) },
  { label: t('monitoring.kpiUptime'),              value: fmtUptime(uptimeSeconds.value) },
])

// Saturation (Golden Signals Schritt 4/4): Ressourcen-Auslastung
// des Backend-Prozesses – ergänzt die Requests-lastigen KPIs oben um
// Goroutines/Speicher/DB-Pool.
const fmtMb = (bytes) => (bytes == null ? '–' : (bytes / (1024 * 1024)).toFixed(1) + ' MB')
// Ab welcher Pool-Auslastung die DB-Kachel Handlungsbedarf zeigt –
// ebenfalls ein grober visueller Schwellwert, keine harte SLO-Grenze.
const dbPoolAlert = (inUse, maxOpen) => maxOpen > 0 && inUse / maxOpen >= 0.8

const saturationKpis = computed(() => {
  const s = saturation.value
  const dbValue = s.db_stats_available ? `${s.db_in_use} / ${s.db_max_open_connections}` : 'n/a'
  return [
    { label: t('monitoring.kpiGoroutines'), value: String(s.goroutine_count ?? 0) },
    { label: t('monitoring.kpiHeapUsage'),  value: fmtMb(s.heap_alloc_bytes) },
    { label: t('monitoring.kpiGcCycles'),   value: String(s.num_gc ?? 0) },
    { label: t('monitoring.kpiDbPool'),     value: dbValue, alert: s.db_stats_available && dbPoolAlert(s.db_in_use, s.db_max_open_connections) },
  ]
})

const columns = computed(() => [
  { key: 'method', label: t('monitoring.columnMethod') },
  { key: 'route', label: t('monitoring.columnRoute') },
  { key: 'request_count', label: t('monitoring.columnRequests'), numeric: true },
  { key: 'requests_per_minute', label: t('monitoring.columnReqPerMin'), numeric: true },
  { key: 'error_rate', label: t('monitoring.columnErrorRate'), numeric: true },
  { key: 'avg_latency_ms', label: t('monitoring.columnAvgLatency'), numeric: true },
  { key: 'p50_latency_ms', label: t('monitoring.columnP50'), numeric: true },
  { key: 'p95_latency_ms', label: t('monitoring.columnP95'), numeric: true },
  { key: 'max_latency_ms', label: t('monitoring.columnMax'), numeric: true },
])
</script>

<template>
  <div>
    <PageHeader :level="2" :title="t('monitoring.title')" :subtitle="t('monitoring.subtitle')" :breadcrumbs="breadcrumbs">
      <template #actions>
        <a
          v-if="grafanaUrl"
          class="grafana-link"
          :href="grafanaUrl"
          target="_blank"
          rel="noopener noreferrer"
        >{{ t('monitoring.grafanaLink') }} ↗</a>
      </template>
    </PageHeader>

    <div v-if="errorMsg" class="alert alert-error mb-16">{{ errorMsg }}</div>

    <!-- KPI-Zeile -->
    <div class="kpi-grid">
      <KpiCard v-for="k in kpis" :key="k.label" :label="k.label" :value="k.value" :alert="k.alert" />
    </div>

    <!-- KPI-Zeile: Saturation (Ressourcen-Auslastung) -->
    <div class="section-subheader">
      <h3>{{ t('monitoring.saturationHeading') }}</h3>
      <span class="text-muted">{{ t('monitoring.saturationSubtitle') }}</span>
    </div>
    <div class="kpi-grid kpi-grid-saturation">
      <KpiCard v-for="k in saturationKpis" :key="k.label" :label="k.label" :value="k.value" :alert="k.alert" />
    </div>

    <!-- Tabelle: Golden Signals je Endpunkt -->
    <ListCard :title="t('monitoring.cardTitle')">
      <template #tools>
        <SegmentControl v-model="usageFilter" :options="usageOptions" :label="t('monitoring.filterLabel')" />
      </template>
      <ListState v-if="routes.length === 0" :loading="loading" :error="!!errorMsg" :count="0" :empty-text="t('monitoring.empty')" icon="activity" />
      <template v-else>
        <p class="muted error-rate-hint">{{ t('monitoring.errorRateHint') }}</p>
        <p v-if="unusedCount > 0" class="muted unused-hint">{{ t('monitoring.unusedHint', { n: unusedCount }) }}</p>
      </template>
      <p v-if="routes.length > 0 && routeRows.length === 0" class="muted">{{ t('monitoring.noUsedRoutes') }}</p>
      <ResponsiveTable v-if="routeRows.length > 0" data-table :columns="columns" :items="routeRows" row-key="rowKey">
        <template #cell-method="{ item }"><span class="ent-pill method-pill">{{ item.method }}</span></template>
        <template #cell-route="{ item }"><span class="ent-mono">{{ item.route }}</span></template>
        <template #cell-request_count="{ item }"><span class="ent-mono">{{ item.request_count }}</span></template>
        <template #cell-requests_per_minute="{ item }"><span class="ent-mono muted">{{ item.unused ? '–' : fmtRate(item.requests_per_minute) }}</span></template>
        <template #cell-error_rate="{ item }">
          <span v-if="item.unused" class="ent-mono muted">–</span>
          <span v-else :title="t('monitoring.errorRateHint')">
            <StatusBadge v-if="errorLevel(item.error_rate)" :status="errorLevel(item.error_rate)" :label="fmtPct(item.error_rate)" />
            <span v-else class="ent-mono muted">{{ fmtPct(item.error_rate) }}</span>
          </span>
        </template>
        <template #cell-avg_latency_ms="{ item }"><span class="ent-mono muted">{{ item.unused ? '–' : fmtMs(item.avg_latency_ms) }}</span></template>
        <template #cell-p50_latency_ms="{ item }"><span class="ent-mono muted">{{ item.unused ? '–' : fmtMs(item.p50_latency_ms) }}</span></template>
        <template #cell-p95_latency_ms="{ item }"><span class="ent-mono muted">{{ item.unused ? '–' : fmtMs(item.p95_latency_ms) }}</span></template>
        <template #cell-max_latency_ms="{ item }"><span class="ent-mono muted">{{ item.unused ? '–' : fmtMs(item.max_latency_ms) }}</span></template>
      </ResponsiveTable>
    </ListCard>
  </div>
</template>

<style scoped>
.grafana-link { font-size: var(--fs-sm); color: var(--primary-text); text-decoration: none; white-space: nowrap; }
.grafana-link:hover { text-decoration: underline; }
.grafana-link:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; border-radius: var(--r-sm); }

.kpi-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: var(--sp-3);
  margin-bottom: var(--sp-5);
}

.section-subheader { display: flex; align-items: baseline; gap: var(--sp-3); margin-bottom: var(--sp-3); flex-wrap: wrap; }
.section-subheader h3 { margin: 0; font-size: var(--fs-md); }

.method-pill {
  font-family: var(--font-mono); font-size: var(--fs-xs); font-weight: 700;
  color: var(--primary-text); background: var(--primary-soft);
}
.muted { color: var(--text-2); }
.mb-16 { margin-bottom: var(--sp-4); }

/* Smartphone (480px+): 2 spalten */
@media (min-width: 480px) {
  .kpi-grid { grid-template-columns: repeat(2, 1fr); }
}

/* Tablet (768px+): 3 spalten */
@media (min-width: 768px) {
  .kpi-grid { grid-template-columns: repeat(3, 1fr); }
  .kpi-grid-saturation { grid-template-columns: repeat(2, 1fr); }
}

/* Desktop (1200px+): 6 spalten */
@media (min-width: 1200px) {
  .kpi-grid { grid-template-columns: repeat(6, 1fr); }
  .kpi-grid-saturation { grid-template-columns: repeat(4, 1fr); }
}
</style>
