<script setup>
/*
  Komponentenseite nach Richtung C: Statusauswahl als Kacheln mit
  Symbol statt eines Selects („Anfangsstatus“). Status ist Form + Farbe + Text,
  die Kachel zeigt beides über StatusIcon und den Statustext. Technisch eine
  Radiogruppe: Pfeiltasten wählen, Tab springt hinein und heraus. Wert ist der
  Roh-Status des Backends (wie QuickStatus). Die Gruppe (fieldset mit Legende,
  Pflichtmarker, Hilfe) setzt der Aufrufer.
*/
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusIcon from './StatusIcon.vue'
import { STATUS_LEVELS, statusMeta, toRawStatus } from '../utils/status'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  modelValue: { type: String, default: null },
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['update:modelValue'])

const { t } = useI18n()
const name = `status-tiles-${useId()}`
const options = STATUS_LEVELS.map(level => ({
  level,
  value: toRawStatus(level),
  labelKey: statusMeta(level).labelKey,
}))
</script>

<template>
  <div class="status-tiles">
    <label v-for="option in options" :key="option.value" class="status-tiles__tile">
      <input
        type="radio"
        class="status-tiles__input"
        :name="name"
        :value="option.value"
        :checked="modelValue === option.value"
        @change="emit('update:modelValue', option.value)"
      >
      <span class="status-tiles__face">
        <StatusIcon :status="option.level" :size="16" />
        <span>{{ t(option.labelKey) }}</span>
      </span>
    </label>
  </div>
</template>

<style scoped>
.status-tiles { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--sp-2); }
.status-tiles__tile { position: relative; display: block; min-inline-size: 0; cursor: pointer; }
.status-tiles__input {
  position: absolute;
  inset: 0;
  inline-size: 100%;
  block-size: 100%;
  margin: 0;
  opacity: 0;
  cursor: pointer;
}
.status-tiles__face {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  min-block-size: 44px;
  padding: 0 var(--sp-3);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-md);
  background: var(--surface);
  color: var(--text);
  font-size: var(--fs-base);
}
.status-tiles__tile:hover .status-tiles__face { border-color: var(--text-3); }
.status-tiles__input:checked + .status-tiles__face {
  border-color: var(--primary);
  background: var(--primary-soft);
  box-shadow: inset 0 0 0 1px var(--primary);
  font-weight: 600;
}
.status-tiles__input:focus-visible + .status-tiles__face { outline: 2px solid var(--focus); outline-offset: 2px; }
</style>
