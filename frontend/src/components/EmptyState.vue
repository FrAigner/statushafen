<script setup lang="ts">
/*
  Redesign stage 3: empty state with icon, title, one sentence and
  an action (slot "action"). Replaces .table-empty, .pad-empty and the
  empty case of ListStateRow. In tables, place it inside a <td colspan>.
  The title is deliberately a <p>, not a heading: an empty state must not
  change the page's heading hierarchy.
*/
import AppIcon from './AppIcon.vue'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
withDefaults(defineProps<{
  title: string
  text?: string | null
  icon?: string | null
}>(), {
  text: null,
  icon: null,
})
// Stryker restore all
</script>

<template>
  <div class="empty-state">
    <AppIcon v-if="icon" :name="icon" :size="28" class="empty-state__icon" />
    <p class="empty-state__title">{{ title }}</p>
    <p v-if="text" class="empty-state__text">{{ text }}</p>
    <div v-if="$slots.action" class="empty-state__action"><slot name="action" /></div>
  </div>
</template>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--sp-2);
  padding: var(--sp-6) var(--sp-4);
  text-align: center;
}
.empty-state__icon { color: var(--text-3); }
.empty-state__title { margin: 0; font-size: var(--fs-md); font-weight: 600; color: var(--text); }
.empty-state__text { margin: 0; max-inline-size: 42ch; font-size: var(--fs-base); color: var(--text-2); }
.empty-state__action { margin-top: var(--sp-2); }
</style>
