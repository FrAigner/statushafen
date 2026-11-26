<script setup lang="ts">
/*
  Redesign 4a: 90-day availability bars. Draws the days from
  GET /api/public/pages/{slug}/availability (oldest on the left, today on the right).
  - Days without data (`no_data`, before the page was created) are neutral grey,
    never green: no data is not operation. The day colour is the worst status
    of the day (degraded, partial outage, major outage, maintenance),
    distinguished by shape in addition to colour (shape + colour + text):
    degraded hatched diagonally, partial outage cross-hatched,
    major outage solid, maintenance hatched blue.
  - The tooltip gives the availability in % and the duration per severity.
  - A percentage only appears when there is data (`uptimePercent` and
    each day's `availability` are null otherwise).
  - Every day has a name for screen readers (date, status, percent); days
    with an impairment or maintenance are reachable via Tab and show the
    label as a tooltip (also on focus, WCAG 1.4.13: no Escape needed, text only).
  Colours exclusively via tokens (--ok, --deg, --maint, --border-strong).
*/
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { toIntlLocale } from '../utils/datetime'
import { availabilityDayMeta, durationParts, formatAvailability, formatDay } from '../utils/availability'
import type { AvailabilityDay, AvailabilityImpairment } from '../types/api'

// A day of the response including the duration per severity (worst first).
interface AvailabilityBarDay extends AvailabilityDay {
  impairments?: AvailabilityImpairment[] | null
}

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  // Name for the accessible name, e.g. the component or "Overall status".
  name: string
  days: AvailabilityBarDay[]
  uptimePercent?: number | null
  // Compact (per component): number and time axis small, without the time axis row.
  compact?: boolean
}>(), {
  uptimePercent: null,
  compact: false,
})
// Stryker restore all

const { t, locale } = useI18n()

const FOCUSABLE = new Set(['deg', 'part', 'major', 'maint'])
const TIP_EDGE = 20

const fmtDay = (iso: string) => formatDay(iso, toIntlLocale(locale.value))

const percentText = computed(() => formatAvailability(props.uptimePercent, toIntlLocale(locale.value)))

const items = computed(() => props.days.map((d, index, all) => {
  const meta = availabilityDayMeta(d.status)
  const percent = formatAvailability(d.availability, toIntlLocale(locale.value))
  const params = { date: fmtDay(d.date), status: t(meta.labelKey), percent }
  // The percentage is only included if there is one and it says something (< 100).
  const base = percent && d.availability! < 100 ? t('availability.dayStatusPercent', params) : t('availability.dayStatus', params)
  // Duration per severity (unweighted, worst first from the backend).
  const durations = (Array.isArray(d.impairments) ? d.impairments : [])
    .filter(i => i && i.seconds > 0)
    .map((i) => {
      const dur = durationParts(i.seconds)
      return t('availability.impairment', { status: t(availabilityDayMeta(i.status).labelKey), duration: t(dur.key, dur.params) })
    })
  return {
    date: d.date,
    level: meta.level,
    // Only days with an impairment/maintenance need a tab stop (90 stops per row would be noise).
    focusable: FOCUSABLE.has(meta.level),
    // The tooltip is wide: at the edges it opens inwards instead of sticking out of the window.
    tipEdge: index < TIP_EDGE ? 'start' : (index >= all.length - TIP_EDGE ? 'end' : ''),
    label: durations.length ? t('availability.dayWithDurations', { base, durations: durations.join(', ') }) : base,
  }
}))

const summary = computed(() => (percentText.value
  ? t('availability.summary', { name: props.name, percent: percentText.value })
  : t('availability.summaryNoData', { name: props.name })))
</script>

<template>
  <div class="avail" :class="{ 'avail--compact': compact }">
    <p v-if="percentText" class="avail__percent">{{ t('availability.uptime', { percent: percentText }) }}</p>
    <!-- without data "No data" instead of an invented number (never "0.00 %"). -->
    <p v-else class="avail__nodata">{{ t('availability.noData') }}</p>
    <div class="avail__bars" role="group" tabindex="0" :aria-label="summary">
      <span
        v-for="d in items"
        :key="d.date"
        class="avail__day"
        :class="`avail__day--${d.level}`"
        role="img"
        :aria-label="d.label"
        :tabindex="d.focusable ? 0 : -1"
      ><span class="avail__tip" :class="d.tipEdge && `avail__tip--${d.tipEdge}`" aria-hidden="true">{{ d.label }}</span></span>
    </div>
    <div v-if="!compact" class="avail__axis" aria-hidden="true">
      <span>{{ t('availability.rangeStart') }}</span>
      <span>{{ t('availability.rangeEnd') }}</span>
    </div>
  </div>
</template>

<style scoped>
.avail { display: flex; flex-direction: column; gap: var(--sp-1); min-inline-size: 0; }
.avail__percent { margin: 0; font-size: var(--fs-sm); font-weight: 600; color: var(--text); font-variant-numeric: tabular-nums; }

.avail__nodata { margin: 0; font-size: var(--fs-sm); color: var(--text-2); }

.avail__bars { display: flex; gap: 1px; block-size: 32px; border-radius: var(--r-sm); }
.avail--compact .avail__bars { block-size: 22px; }
.avail__bars:focus-visible,
.avail__day:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }

.avail__day { position: relative; flex: 1 1 0; min-inline-size: 0; border-radius: 2px; background: var(--ok); }
.avail__day--none { background: var(--border-strong); }
/* Hatching: degradation/maintenance stay distinguishable without colour perception. */
.avail__day--deg { background: repeating-linear-gradient(135deg, var(--deg) 0 3px, var(--deg-soft) 3px 5px); }
/* Partial outage: cross-hatching; major outage: solid (the shape distinguishes the levels without colour perception). */
.avail__day--part {
  background:
    repeating-linear-gradient(45deg, var(--part) 0 2px, transparent 2px 5px),
    repeating-linear-gradient(135deg, var(--part) 0 2px, var(--part-soft) 2px 5px);
}
.avail__day--major { background: var(--major); }
.avail__day--maint { background: repeating-linear-gradient(135deg, var(--maint) 0 3px, var(--maint-soft) 3px 5px); }

.avail__tip {
  display: none;
  position: absolute;
  inset-block-end: calc(100% + 6px);
  inset-inline-start: 50%;
  transform: translateX(-50%);
  z-index: 5;
  inline-size: max-content;
  max-inline-size: min(20rem, 80vw);
  white-space: normal;
  text-align: start;
  font-size: var(--fs-xs);
  padding: var(--sp-1) var(--sp-2);
  border-radius: var(--r-md);
  background: var(--text);
  color: var(--surface);
  pointer-events: none;
}
.avail__tip--start { inset-inline-start: 0; transform: none; }
.avail__tip--end { inset-inline-start: auto; inset-inline-end: 0; transform: none; }
.avail__day:hover .avail__tip,
.avail__day:focus .avail__tip { display: block; }

.avail__axis { display: flex; justify-content: space-between; font-size: var(--fs-xs); color: var(--text-2); }
</style>
