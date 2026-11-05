<script setup>
/*
  Redesign Stufe 3: Statusanzeige aus Symbol (Form) + Farbe +
  Text. Ersetzt .badge-*, .ent-pill/.ent-dot und Inline-Farben.
  - variant "soft": getönte Fläche (--*-soft) mit Text in --*-text (Tabellen,
    Karten); "plain": ohne Fläche (Fließtext, Statuszeilen).
  - size "sm" (Standard) oder "md".
  - Wortlaut aus status.* (i18n), „Beeinträchtigt“ bleibt. Ein Slot oder
    `label` ersetzt den Text (z. B. ein längerer Satz); die Form bleibt.
  - Unbekannte Status zeigen neutral den Rohwert, nie eine Statusfarbe.
*/
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusIcon from './StatusIcon.vue'
import { statusMeta } from '../utils/status'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  status: { type: String, default: null },
  variant: { type: String, default: 'soft', validator: v => ['soft', 'plain'].includes(v) },
  size: { type: String, default: 'sm', validator: v => ['sm', 'md'].includes(v) },
  label: { type: String, default: null },
})
// Stryker restore all

const { t } = useI18n()
const meta = computed(() => statusMeta(props.status))
const text = computed(() => props.label ?? (meta.value.labelKey ? t(meta.value.labelKey) : (props.status ?? '')))
</script>

<template>
  <span
    class="st-badge"
    :class="[
      `st-badge--${variant}`,
      `st-badge--${size}`,
      meta.level ? `st-badge--${meta.level}` : 'st-badge--unknown',
    ]"
  >
    <StatusIcon :status="status" :size="size === 'md' ? 18 : 14" />
    <span class="st-badge__text"><slot>{{ text }}</slot></span>
  </span>
</template>

<style scoped>
.st-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-1);
  max-inline-size: 100%;
  border-radius: var(--r-sm);
  font-weight: 600;
  line-height: 1.3;
  white-space: nowrap;
  color: var(--st-text, var(--text-2));
}
.st-badge--sm { padding: 2px 8px; font-size: var(--fs-xs); }
.st-badge--md { padding: 4px 10px; font-size: var(--fs-base); gap: 6px; }
.st-badge--plain { padding-inline: 0; background: none; }
.st-badge--soft { background: var(--st-soft, var(--surface-2)); }

.st-badge--operational { --st-text: var(--ok-text); --st-soft: var(--ok-soft); }
.st-badge--degraded { --st-text: var(--deg-text); --st-soft: var(--deg-soft); }
.st-badge--partial { --st-text: var(--part-text); --st-soft: var(--part-soft); }
.st-badge--major { --st-text: var(--major-text); --st-soft: var(--major-soft); }
.st-badge--maintenance { --st-text: var(--maint-text); --st-soft: var(--maint-soft); }
</style>
