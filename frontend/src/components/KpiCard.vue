<script setup lang="ts">
/*
  Redesign stage 3: KPI card. Value in Inter 28/600, no rainbow
  colours: only `alert` colours the value (--major-text) and appends
  "action needed" for screen readers, so the alert is not conveyed by colour
  alone. Typical case: "Open incidents" > 0.
*/
import { useI18n } from 'vue-i18n'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
withDefaults(defineProps<{
  label: string
  value: number | string
  alert?: boolean
}>(), {
  alert: false,
})
// Stryker restore all

const { t } = useI18n()
</script>

<template>
  <div class="kpi" :class="{ 'kpi--alert': alert }">
    <div class="kpi__label">{{ label }}</div>
    <div class="kpi__value">
      {{ value }}
      <span v-if="alert" class="kpi__sr">{{ t('patterns.kpiAlert') }}</span>
    </div>
  </div>
</template>

<style scoped>
.kpi {
  position: relative;
  padding: var(--sp-4) var(--sp-5);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: var(--surface);
  box-shadow: var(--shadow-1);
}
.kpi__label { font-size: var(--fs-sm); color: var(--text-2); }
.kpi__value {
  margin-top: var(--sp-1);
  font-family: 'Inter', var(--font-sans);
  font-size: 28px;
  font-weight: 600;
  line-height: 1.2;
  font-variant-numeric: tabular-nums;
  color: var(--text);
}
.kpi--alert .kpi__value { color: var(--major-text); }
.kpi__sr {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
</style>
