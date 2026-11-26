<script setup lang="ts">
/*
  Redesign stage 3: status display made of icon (shape) + colour + text.
  Replaces .badge-*, .ent-pill/.ent-dot and inline colours.
  - variant "soft": tinted area (--*-soft) with text in --*-text (tables,
    cards); "plain": without an area (running text, status lines).
  - size "sm" (default) or "md".
  - Wording from status.* (i18n), "Beeinträchtigt" stays. A slot or `label`
    replaces the text (e.g. a longer sentence); the shape stays.
  - Unknown statuses show the raw value neutrally, never a status colour.
*/
import { computed } from 'vue'
import type { PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusIcon from './StatusIcon.vue'
import { statusMeta } from '../utils/status'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
// Runtime declaration (not type-based) to keep the validators.
const props = defineProps({
  status: { type: String as PropType<string | null>, default: null },
  variant: { type: String as PropType<'soft' | 'plain'>, default: 'soft', validator: (v: string) => ['soft', 'plain'].includes(v) },
  size: { type: String as PropType<'sm' | 'md'>, default: 'sm', validator: (v: string) => ['sm', 'md'].includes(v) },
  label: { type: String as PropType<string | null>, default: null },
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
