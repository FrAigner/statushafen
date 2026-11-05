<script setup>
/*
  Redesign 4a: 90-Tage-Verfügbarkeitsbalken. Zeichnet die Tage aus
  GET /api/public/pages/{slug}/availability (älteste links, heute rechts).
  - Tage ohne Daten (`no_data`, vor Anlage der Seite) sind neutral grau, nie
    grün: keine Daten sind kein Betrieb. Die Tagesfarbe ist der schlimmste Status
    des Tages (eingeschränkt, Teilausfall, Großstörung, Wartung),
    zusätzlich zur Farbe über die Form unterschieden (Form + Farbe + Text):
    eingeschränkt diagonal schraffiert, Teilausfall kreuzschraffiert,
    Großstörung vollflächig, Wartung blau schraffiert.
  - Der Tooltip nennt Verfügbarkeit in % und die Dauer je Schweregrad.
  - Eine Prozentzahl erscheint nur bei vorhandenen Daten (`uptimePercent` und
    je Tag `availability` sind sonst null).
  - Jeder Tag hat einen Namen für Screenreader (Datum, Status, Prozent); Tage
    mit Beeinträchtigung oder Wartung sind per Tab erreichbar und zeigen das
    Label als Tooltip (auch bei Fokus, WCAG 1.4.13: Escape-frei, nur Text).
  Farben ausschließlich über Tokens (--ok, --deg, --maint, --border-strong).
*/
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { toIntlLocale } from '../utils/datetime'
import { availabilityDayMeta, durationParts, formatAvailability, formatDay } from '../utils/availability'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  // Name für den zugänglichen Namen, z. B. die Komponente oder „Gesamtstatus“.
  name: { type: String, required: true },
  days: { type: Array, required: true }, // [{ date, status, availability, downtime_seconds }]
  uptimePercent: { type: Number, default: null },
  // Kompakt (je Komponente): Zahl und Zeitachse klein, ohne Zeitachsenzeile.
  compact: { type: Boolean, default: false },
})
// Stryker restore all

const { t, locale } = useI18n()

const FOCUSABLE = new Set(['deg', 'part', 'major', 'maint'])
const TIP_EDGE = 20

const fmtDay = (iso) => formatDay(iso, toIntlLocale(locale.value))

const percentText = computed(() => formatAvailability(props.uptimePercent, toIntlLocale(locale.value)))

const items = computed(() => props.days.map((d, index, all) => {
  const meta = availabilityDayMeta(d.status)
  const percent = formatAvailability(d.availability, toIntlLocale(locale.value))
  const params = { date: fmtDay(d.date), status: t(meta.labelKey), percent }
  // Der Prozentwert gehört nur dazu, wenn es einen gibt und er etwas aussagt (< 100).
  const base = percent && d.availability < 100 ? t('availability.dayStatusPercent', params) : t('availability.dayStatus', params)
  // Dauer je Schweregrad (ungewichtet, vom Backend schlimmster zuerst).
  const durations = (Array.isArray(d.impairments) ? d.impairments : [])
    .filter(i => i && i.seconds > 0)
    .map((i) => {
      const dur = durationParts(i.seconds)
      return t('availability.impairment', { status: t(availabilityDayMeta(i.status).labelKey), duration: t(dur.key, dur.params) })
    })
  return {
    date: d.date,
    level: meta.level,
    // Nur Tage mit Beeinträchtigung/Wartung brauchen einen Tab-Stopp (90 Stopps pro Zeile wären Lärm).
    focusable: FOCUSABLE.has(meta.level),
    // Der Tooltip ist breit: an den Rändern nach innen statt aus dem Fenster ragen.
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
    <!-- ohne Datenbasis „Keine Daten“ statt einer erfundenen Zahl (nie „0,00 %“). -->
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
/* Schraffur: Beeinträchtigung/Wartung unterscheiden sich auch ohne Farbwahrnehmung. */
.avail__day--deg { background: repeating-linear-gradient(135deg, var(--deg) 0 3px, var(--deg-soft) 3px 5px); }
/* Teilausfall: Kreuzschraffur; Großstörung: vollflächig (Form unterscheidet die Stufen auch ohne Farbwahrnehmung). */
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
