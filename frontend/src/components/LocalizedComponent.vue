<script setup>
import { computed } from 'vue'
import { useComponentLocalization } from '../composables/useComponentLocalization'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  component: {
    type: Object,
    required: true
  }
})
// Stryker restore all

const { currentTranslation } = useComponentLocalization(
  props.component,
  (id) => `/api/components/${id}/translations`
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
