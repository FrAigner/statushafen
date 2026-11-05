<script setup>
/*
  Redesign Stufe 3: Statussymbol. Status ist immer Form + Farbe
  + Text, nie nur Farbe: Kreis-Haken (in Betrieb), Dreieck-! (beeinträchtigt),
  Raute-– (Teilausfall), Achteck-× (Großstörung), Quadrat-Uhr (Wartung).
  Die Zuordnung steht in utils/status.js.

  Ohne `label` ist das Symbol dekorativ (aria-hidden), weil daneben der
  Statustext steht (StatusBadge). Allein verwendet braucht es `label`, dann
  bekommt es role="img" und einen zugänglichen Namen.
  Farben nur über Tokens (--ok, --deg, …, --icon-on für das Zeichen).
*/
import { computed } from 'vue'
import { statusMeta } from '../utils/status'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  status: { type: String, default: null },
  size: { type: [Number, String], default: 16 },
  label: { type: String, default: null },
})
// Stryker restore all

const meta = computed(() => statusMeta(props.status))
</script>

<template>
  <svg
    class="status-icon"
    :class="meta.level ? `status-icon--${meta.level}` : 'status-icon--unknown'"
    :data-shape="meta.shape"
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    :role="label ? 'img' : undefined"
    :aria-label="label || undefined"
    :aria-hidden="label ? undefined : 'true'"
    focusable="false"
  >
    <template v-if="meta.shape === 'circle-check'">
      <circle class="si-shape" cx="12" cy="12" r="10.5" />
      <polyline class="si-glyph" points="7.5,12.5 10.7,15.7 16.5,9" />
    </template>
    <template v-else-if="meta.shape === 'triangle-exclamation'">
      <path class="si-shape" d="M12 2.2 22.6 20.8H1.4Z" />
      <line class="si-glyph" x1="12" y1="9" x2="12" y2="14.2" />
      <line class="si-glyph" x1="12" y1="17.2" x2="12" y2="17.3" />
    </template>
    <template v-else-if="meta.shape === 'diamond-minus'">
      <path class="si-shape" d="M12 1.2 22.8 12 12 22.8 1.2 12Z" />
      <line class="si-glyph" x1="7.5" y1="12" x2="16.5" y2="12" />
    </template>
    <template v-else-if="meta.shape === 'octagon-cross'">
      <path class="si-shape" d="M8 1.8h8L22.2 8v8L16 22.2H8L1.8 16V8Z" />
      <line class="si-glyph" x1="8.7" y1="8.7" x2="15.3" y2="15.3" />
      <line class="si-glyph" x1="15.3" y1="8.7" x2="8.7" y2="15.3" />
    </template>
    <template v-else-if="meta.shape === 'square-clock'">
      <rect class="si-shape" x="1.8" y="1.8" width="20.4" height="20.4" rx="3.5" />
      <circle class="si-glyph si-glyph--open" cx="12" cy="12" r="5.6" />
      <polyline class="si-glyph" points="12,9 12,12 14.2,13.4" />
    </template>
    <template v-else>
      <circle class="si-shape si-shape--open" cx="12" cy="12" r="9.5" />
    </template>
  </svg>
</template>

<style scoped>
.status-icon {
  flex-shrink: 0;
  vertical-align: -0.15em;
  --si-color: var(--text-3);
}
.status-icon--operational { --si-color: var(--ok); }
.status-icon--degraded { --si-color: var(--deg); }
.status-icon--partial { --si-color: var(--part); }
.status-icon--major { --si-color: var(--major); }
.status-icon--maintenance { --si-color: var(--maint); }

.si-shape {
  fill: var(--si-color);
  stroke: var(--si-color);
  stroke-width: 1.4;
  stroke-linejoin: round;
}
.si-shape--open { fill: none; stroke-width: 2; }
.si-glyph {
  fill: none;
  stroke: var(--icon-on);
  stroke-width: 2.2;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.si-glyph--open { stroke-width: 1.8; }
</style>
