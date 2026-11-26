<script setup lang="ts">
/*
  Direction C, admin pages: search field in the card header (magnifier,
  type="search"). The aria label is required because there is no visible
  label. Further attributes (e.g. v-help) land on the input field.
*/
import AppIcon from './AppIcon.vue'

defineOptions({ inheritAttrs: false })
// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
withDefaults(defineProps<{
  label: string
  placeholder?: string | null
}>(), {
  placeholder: null,
})
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const model = defineModel<string>({ default: '' })
</script>

<template>
  <div class="comp-search">
    <AppIcon name="search" :size="16" class="comp-search__icon" />
    <input
      v-bind="$attrs"
      v-model="model"
      type="search"
      class="comp-search__input"
      :aria-label="label"
      :placeholder="placeholder ?? label"
    >
  </div>
</template>
