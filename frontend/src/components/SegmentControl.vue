<script setup lang="ts">
/*
  Direction C, operations pages: segment switch for list filters
  (e.g. All / Open / Resolved). A group of buttons with aria-pressed, so it
  can be operated via Tab and Enter/Space without special arrow-key logic.
  Options: [{ value, label }]; the selected value is recognizable without
  colour too, by border and fill (aria-pressed conveys it).
*/
// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
defineProps<{
  modelValue: string
  options: { value: string, label: string }[]
  label: string
}>()
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
</script>

<template>
  <div class="segment" role="group" :aria-label="label">
    <button
      v-for="opt in options"
      :key="opt.value"
      type="button"
      class="segment__item"
      :class="{ 'segment__item--on': opt.value === modelValue }"
      :aria-pressed="opt.value === modelValue ? 'true' : 'false'"
      data-help-exempt="Filter-Umschalter, selbsterklärend"
      @click="emit('update:modelValue', opt.value)"
    >{{ opt.label }}</button>
  </div>
</template>
