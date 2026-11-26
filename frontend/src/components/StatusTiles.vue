<script setup lang="ts">
/*
  Component page following direction C: status choice as tiles with an icon
  instead of a select ("initial status"). Status is shape + colour + text,
  the tile shows both via StatusIcon and the status text. Technically a radio
  group: arrow keys select, Tab jumps in and out. The value is the backend's
  raw status (like QuickStatus). The group (fieldset with legend, required
  marker, help) is provided by the caller.
*/
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusIcon from './StatusIcon.vue'
import { STATUS_LEVELS, statusMeta, toRawStatus } from '../utils/status'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
withDefaults(defineProps<{
  modelValue?: string | null
}>(), {
  modelValue: null,
})
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ 'update:modelValue': [status: string] }>()

const { t } = useI18n()
const name = `status-tiles-${useId()}`
// Each of the four levels has a raw value and a label.
const options = STATUS_LEVELS.map(level => ({
  level,
  value: toRawStatus(level) as string,
  labelKey: statusMeta(level).labelKey as string,
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
