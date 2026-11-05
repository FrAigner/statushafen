<script setup>
// Darstellung des Seiten-Layouts "liste" – Komponentengruppen als
// Karten mit 90-Tage-Balkenreihe und eine Vorfall-Zeitleiste. Reine
// Darstellung: Daten, Filter und der Verlauf-Dialog gehören weiterhin
// StatusPage.vue.
// 4a: Der Gesamtstatus steht jetzt für beide Layouts oben in
// StatusPage.vue (nicht mehr hier). Die Balken zeigen die Tagesaggregate des
// Backends (availability, GET …/availability?include=components) statt einer
// Schätzung aus Vorfällen; Status überall als StatusBadge, Einrückung per Klasse.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'
import AvailabilityBars from './AvailabilityBars.vue'
import EmptyState from './EmptyState.vue'
import RichText from './RichText.vue'
import StatusBadge from './StatusBadge.vue'
import ExternalSourceNote from './ExternalSourceNote.vue'
import { visibleExternalSources } from '../utils/externalStatus'
import { toIntlLocale } from '../utils/datetime'
import { groupIncidentsByDay } from '../utils/pageLayout'
import { incidentStatusMeta } from '../utils/incidentStatus'

// Stryker disable all: defineProps wird aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  // Gefilterte Komponenten für die Karten.
  components: { type: Array, required: true },
  // Gefilterte Vorfälle für die Zeitleiste.
  timelineIncidents: { type: Array, required: true },
  // Antwort von GET …/availability?include=components oder null (noch nicht
  // geladen/fehlgeschlagen): dann entfallen die Balken, der Rest bleibt.
  availability: { type: Object, default: null },
})
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['open-history'])
// Stryker restore all

const { t, locale } = useI18n()

// Vorfallstatus -> Stufe/Text; unbekannte Werte zeigen neutral den Rohwert.
const incidentBadge = (status) => {
  const meta = incidentStatusMeta(status)
  return { level: meta.level, label: meta.labelKey ? t(meta.labelKey) : status }
}

const days = computed(() => groupIncidentsByDay(props.timelineIncidents))

// Ein Formatter je Sprache; toLocaleDateString baute je Aufruf einen neuen
// Intl-Formatter auf (Review-Befund 4: Sekunden Renderzeit bei 40 Zeilen).
const dayFmt = computed(() => new Intl.DateTimeFormat(toIntlLocale(locale.value), { dateStyle: 'medium', timeZone: 'UTC' }))
const fmtDay = (iso) => dayFmt.value.format(new Date(`${iso}T12:00:00Z`))

// Tageswerte je Komponente. Gruppen haben keine eigene Zeile im Endpunkt
// (ein Vorfall an einer Gruppe zählt für ihre Kinder) und bekommen daher
// keinen Balken, ebenso Komponenten, die der Endpunkt nicht kennt.
const availabilityById = computed(() => new Map((props.availability?.components ?? []).map(c => [c.id, c])))

// Einrückung als Klasse (pl-d0 … pl-d5) statt Inline-Style; tiefer verschachtelte
// Ebenen teilen sich die letzte Stufe.
const MAX_INDENT_LEVEL = 5
const flatten = (node, depth = 0) => [
  { node, depth: Math.min(depth, MAX_INDENT_LEVEL), avail: availabilityById.value.get(node.ID) ?? null },
  ...(node.Children || []).flatMap(c => flatten(c, depth + 1)),
]
const groups = computed(() => props.components.map(n => flatten(n)))
</script>

<template>
  <div class="pl-root">
    <h2 class="pl-heading">{{ t('pageLayout.groupsHeading') }}</h2>
    <EmptyState
      v-if="groups.length === 0"
      icon="search"
      :title="t('statusPage.noComponentsFound')"
      :text="t('statusPage.noComponentsHint')"
    />
    <section v-for="rows in groups" :key="rows[0].node.ID" class="pl-card">
      <div v-for="row in rows" :key="row.node.ID" class="pl-row" :class="`pl-d${row.depth}`">
        <div class="pl-row-head">
          <span class="pl-name">{{ row.node.Name }}</span>
          <StatusBadge :status="row.node.Status" />
        </div>
        <ExternalSourceNote
          v-if="visibleExternalSources(row.node).length > 0"
          :sources="visibleExternalSources(row.node)"
          :name="row.node.Name"
        />
        <AvailabilityBars
          v-if="row.avail"
          compact
          :name="row.node.Name"
          :days="row.avail.days"
          :uptime-percent="row.avail.uptime_percent"
        />
      </div>
    </section>

    <h2 class="pl-heading">{{ t('pageLayout.timelineHeading') }}</h2>
    <ol v-if="days.length > 0" class="pl-timeline">
      <li v-for="d in days" :key="d.date" class="pl-timeline-day">
        <h3 class="pl-day-mark">{{ fmtDay(d.date) }}</h3>
        <article v-for="i in d.items" :key="i.ID" class="pl-incident">
          <div class="pl-incident-head">
            <span class="pl-incident-title">{{ i.Title }}</span>
            <span v-if="i.external" class="pl-ext-chip">
              <AppIcon name="globe" :size="12" />
              <span class="pl-ext-tag">{{ t('external.badge') }}</span>
              <span>{{ t('external.sourceLine', { source: i.external.source_name }) }}</span>
            </span>
            <StatusBadge :status="incidentBadge(i.Status).level" :label="incidentBadge(i.Status).label" />
          </div>
          <RichText class="pl-incident-msg" :text="i.Message" :plain="!!i.external" />
          <button type="button" class="pl-link" @click="emit('open-history', i)">
            <AppIcon name="clock" :size="13" /> {{ t('statusPage.viewHistoryButton') }}
          </button>
        </article>
      </li>
    </ol>
    <p v-else class="pl-empty">{{ t('pageLayout.timelineEmpty') }}</p>
  </div>
</template>

<style scoped>
.pl-root { display: flex; flex-direction: column; gap: var(--sp-3); max-inline-size: 100%; }
.pl-heading { margin: var(--sp-2) 0 0; font-size: var(--fs-lg); font-weight: 600; color: var(--text); }

.pl-card { border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface); overflow: hidden; }
.pl-row { --indent: 0; padding: var(--sp-3) var(--sp-4); padding-inline-start: calc(var(--sp-4) + var(--indent) * 20px); display: flex; flex-direction: column; gap: var(--sp-2); }
.pl-d1 { --indent: 1; }
.pl-d2 { --indent: 2; }
.pl-d3 { --indent: 3; }
.pl-d4 { --indent: 4; }
.pl-d5 { --indent: 5; }
.pl-row + .pl-row { border-top: 1px solid var(--border); }
.pl-row-head { display: flex; justify-content: space-between; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.pl-name { font-weight: 600; color: var(--text); overflow-wrap: anywhere; }

.pl-link:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }

.pl-timeline { list-style: none; margin: 0; padding: 0 0 0 var(--sp-3); border-left: 2px solid var(--border); display: flex; flex-direction: column; gap: var(--sp-4); }
.pl-day-mark { font-size: var(--fs-base); color: var(--text-2); margin: 0 0 var(--sp-2); }
.pl-incident { border: 1px solid var(--border); border-radius: var(--r-lg); padding: var(--sp-3) var(--sp-4); background: var(--surface); }
.pl-incident + .pl-incident { margin-top: var(--sp-2); }
.pl-incident-head { display: flex; justify-content: space-between; gap: var(--sp-3); align-items: center; flex-wrap: wrap; }
.pl-incident-title { font-weight: 600; color: var(--text); overflow-wrap: anywhere; }
.pl-incident-msg { margin: var(--sp-2) 0; color: var(--text-secondary); font-size: var(--fs-base); overflow-wrap: anywhere; }
.pl-ext-chip { display: inline-flex; align-items: center; gap: var(--sp-1); padding: 1px 8px; border: 1px solid var(--border-strong); border-radius: var(--r-sm); background: var(--surface-2); font-size: var(--fs-xs); font-weight: 600; color: var(--text-2); }
.pl-ext-tag { text-transform: uppercase; letter-spacing: 0.04em; }
.pl-link { background: none; border: 0; padding: var(--sp-1) 0; min-block-size: 24px; cursor: pointer; color: var(--primary); display: inline-flex; gap: var(--sp-1); align-items: center; font-size: var(--fs-sm); }
.pl-empty { color: var(--text-secondary); }
</style>
