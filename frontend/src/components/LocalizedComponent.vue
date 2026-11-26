<script setup lang="ts">
import { computed } from 'vue'
import { useComponentLocalization } from '../composables/useComponentLocalization'
import type { Component } from '../types/api'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = defineProps<{
  component: Component
}>()
// Stryker restore all

const { currentTranslation } = useComponentLocalization(
  props.component,
  (id: number) => `/api/components/${id}/translations`
)

const displayTitle = computed(() => {
  return currentTranslation.value?.Title || props.component.Name
})

const displayDescription = computed(() => {
  return currentTranslation.value?.Description || props.component.Description || ''
})
</script>

<template>
  <div class="component">
    <div class="component-content">
      <h3>{{ displayTitle }}</h3>
      <p v-if="displayDescription" class="component-description">{{ displayDescription }}</p>
    </div>
  </div>
</template>

<style scoped>
.component {
  padding: 12px;
}

.component-content h3 {
  margin: 0 0 8px 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-1);
}

.component-description {
  margin: 0;
  font-size: 14px;
  color: var(--text-2);
  line-height: 1.4;
}
</style>
