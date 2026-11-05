<script setup>
/*
  Teil D: gespiegelte Vorfälle und Wartungen externer Quellen
  (Dashboard-Feld external_incidents), eigener Abschnitt der Statusseite.
  - Jede Meldung ist mit „extern“, der Quelle (source_name) und einem Link auf
    die Originalmeldung gekennzeichnet (nur http/https, neuer Tab mit Hinweis).
  - Statuswort wie bei eigenen Vorfällen über StatusBadge (Form + Farbe + Text).
  - Texte stammen aus fremden Quellen und werden ausschließlich als Text
    ausgegeben (Mustache-Interpolation, nie v-html); Zeilenumbrüche bleiben per
    CSS (white-space: pre-wrap) erhalten.
  - Updates in zeitlicher Folge, neueste zuerst (wie die Zeitleiste eigener Vorfälle).
*/
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'
import StatusBadge from './StatusBadge.vue'
import { toIntlLocale } from '../utils/datetime'
import { incidentStatusMeta } from '../utils/incidentStatus'
import { safeHttpUrl } from '../utils/externalStatus'

// Wie bei eigenen Vorfällen stehen in der Karte nur die neuesten
// Updates, der Rest im Verlauf-Dialog der Statusseite (Ereignis open-history).
const INLINE_UPDATES = 3

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  // external_incidents der Dashboard-Antwort.
  items: { type: Array, required: true },
})
// Stryker restore all

// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['open-history'])

const { t, locale } = useI18n()

const fmt = computed(() => new Intl.DateTimeFormat(toIntlLocale(locale.value), { dateStyle: 'medium', timeStyle: 'short' }))
const fmtDate = (d) => (d ? fmt.value.format(new Date(d)) : '—')

const badge = (status) => {
  const meta = incidentStatusMeta(status)
  return { level: meta.level, label: meta.labelKey ? t(meta.labelKey) : status }
}

const byTimeDesc = (a, b) => new Date(b.display_at) - new Date(a.display_at)

const rows = computed(() => props.items.map((i) => {
  const maintenance = i.kind === 'maintenance'
  const times = []
  if (maintenance && i.scheduled_for) {
    times.push(t('external.scheduledWindow', { from: fmtDate(i.scheduled_for), until: fmtDate(i.scheduled_until) }))
  } else if (i.started_at) {
    times.push(t('external.startedAt', { time: fmtDate(i.started_at) }))
  }
  if (i.resolved_at) times.push(t('external.resolvedAt', { time: fmtDate(i.resolved_at) }))
  else if (i.updated_at) times.push(t('external.updatedAt', { time: fmtDate(i.updated_at) }))
  return {
    ...i,
    kindLabel: maintenance ? t('external.kindMaintenance') : t('external.kindIncident'),
    status: badge(i.status),
    href: safeHttpUrl(i.url) || safeHttpUrl(i.source_url),
    times,
    affected: (i.affected_components || []).map((c) => c.name).join(', '),
    updates: [...(i.updates || [])].sort(byTimeDesc).slice(0, INLINE_UPDATES).map((u) => ({ ...u, status: badge(u.status) })),
    hasUpdates: (i.updates || []).length > 0,
  }
}))
</script>

<template>
  <section class="ei" aria-labelledby="ei-heading">
    <h2 id="ei-heading" class="ei__heading"><AppIcon name="globe" :size="18" /> {{ t('external.heading') }}</h2>
    <p class="ei__intro">{{ t('external.intro') }}</p>

    <article v-for="i in rows" :key="i.id" class="ei__item" :class="{ 'ei__item--closed': i.closed }">
      <header class="ei__head">
        <h3 class="ei__title">{{ i.title }}</h3>
        <StatusBadge :status="i.status.level" :label="i.status.label" />
      </header>

      <p class="ei__source">
        <span class="ei__chip">
          <AppIcon name="globe" :size="12" />
          <span class="ei__chip-tag">{{ t('external.badge') }}</span>
          <span>{{ i.kindLabel }}</span>
        </span>
        <span>{{ t('external.sourceLine', { source: i.source_name }) }}</span>
        <a
          v-if="i.href"
          class="ei__link"
          :href="i.href"
          target="_blank"
          rel="noopener noreferrer nofollow"
        >
          {{ t('external.viewOriginal', { source: i.source_name }) }}
          <AppIcon name="external-link" :size="12" />
          <span class="sr-only">{{ t('external.newTab') }}</span>
        </a>
      </p>

      <p v-if="i.affected" class="ei__meta">{{ t('external.affects') }}: <strong>{{ i.affected }}</strong></p>
      <p v-if="i.times.length" class="ei__meta ei__times">
        <span v-for="(tm, idx) in i.times" :key="idx">{{ tm }}</span>
      </p>

      <ol v-if="i.updates.length" class="ei__timeline" :aria-label="t('external.updatesLabel', { title: i.title })">
        <li v-for="u in i.updates" :key="u.id" class="ei__update">
          <div class="ei__update-meta">
            <StatusBadge variant="plain" :status="u.status.level" :label="u.status.label" />
            <time class="ei__time" :datetime="u.display_at">{{ fmtDate(u.display_at) }}</time>
          </div>
          <p class="ei__body">{{ u.body }}</p>
        </li>
      </ol>
      <p v-else class="ei__meta">{{ t('external.noUpdates') }}</p>
      <button v-if="i.hasUpdates" type="button" class="ei__history" @click="emit('open-history', items.find((x) => x.id === i.id))">
        <AppIcon name="clock" :size="13" /> {{ t('statusPage.viewFullHistoryButton') }}
      </button>
    </article>
  </section>
</template>

<style scoped>
.ei { margin-top: var(--sp-6); }
.ei__heading {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  margin: 0 0 var(--sp-2);
  font-size: var(--fs-lg);
  font-weight: 600;
  color: var(--text);
}
.ei__intro { margin: 0 0 var(--sp-3); font-size: var(--fs-sm); color: var(--text-secondary); }

.ei__item {
  padding: var(--sp-4) var(--sp-5);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: var(--surface);
}
.ei__item + .ei__item { margin-top: var(--sp-3); }
.ei__head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--sp-3); }
.ei__title { margin: 0; font-size: var(--fs-md); font-weight: 600; color: var(--text); overflow-wrap: anywhere; }

.ei__source {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--sp-1) var(--sp-3);
  margin: var(--sp-2) 0 0;
  font-size: var(--fs-sm);
  color: var(--text-2);
}
.ei__chip {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-1);
  padding: 1px 8px;
  border: 1px solid var(--border-strong);
  border-radius: var(--r-sm);
  background: var(--surface-2);
  font-size: var(--fs-xs);
  font-weight: 600;
  color: var(--text-2);
}
.ei__chip-tag { text-transform: uppercase; letter-spacing: 0.04em; }
.ei__link { display: inline-flex; align-items: center; gap: var(--sp-1); color: var(--primary); overflow-wrap: anywhere; }
.ei__link:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }

.ei__meta { margin: var(--sp-2) 0 0; font-size: var(--fs-base); color: var(--text-secondary); overflow-wrap: anywhere; }
.ei__times { display: flex; flex-wrap: wrap; gap: var(--sp-1) var(--sp-4); font-size: var(--fs-sm); }

.ei__timeline {
  list-style: none;
  margin: var(--sp-4) 0 0;
  padding: 0 0 0 var(--sp-4);
  border-inline-start: 2px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
}
.ei__update-meta { display: flex; flex-wrap: wrap; align-items: center; gap: var(--sp-2) var(--sp-3); }
.ei__history { background: none; border: 0; padding: var(--sp-1) 0; min-block-size: 24px; margin-top: var(--sp-2); cursor: pointer; color: var(--primary); display: inline-flex; gap: var(--sp-1); align-items: center; font-size: var(--fs-sm); }
.ei__history:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
.ei__time { font-size: var(--fs-xs); color: var(--text-3); font-variant-numeric: tabular-nums; }
.ei__body { margin: var(--sp-1) 0 0; font-size: var(--fs-base); color: var(--text-secondary); overflow-wrap: anywhere; white-space: pre-wrap; }

@media (max-width: 640px) {
  .ei__item { padding: var(--sp-3) var(--sp-4); }
  .ei__timeline { padding-inline-start: var(--sp-3); }
}
</style>
