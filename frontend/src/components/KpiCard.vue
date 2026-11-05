<script setup>
/*
  Redesign Stufe 3: Kennzahl-Karte. Wert in Inter 28/600, keine
  Regenbogenfarben: nur `alert` färbt den Wert (--major-text) und fügt für
  Screenreader „Handlungsbedarf“ an, damit der Alarm nicht nur über Farbe
  läuft. Typischer Fall: „Offene Vorfälle“ > 0.
*/
import { useI18n } from 'vue-i18n'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  label: { type: String, required: true },
  value: { type: [Number, String], required: true },
  alert: { type: Boolean, default: false },
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
