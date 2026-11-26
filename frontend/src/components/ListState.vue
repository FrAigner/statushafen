<script setup lang="ts">
/*
  Redesign stage 4b: loading, error and empty state of an admin
  list without a table row (successor of ListStateRow for views that use
  ResponsiveTable with dataTable). Only appears while the list is empty
  (count = 0): existing rows stay in place while reloading or after a load
  error. Precedence: loading > error > empty.
  The empty state uses EmptyState (title = the view's empty text).
*/
import { useI18n } from 'vue-i18n'
import EmptyState from './EmptyState.vue'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
withDefaults(defineProps<{
  loading?: boolean
  error?: boolean
  count: number
  emptyText: string
  icon?: string | null
}>(), {
  loading: false,
  error: false,
  icon: null,
})
// Stryker restore all

const { t } = useI18n()
</script>

<template>
  <template v-if="count === 0">
    <p v-if="loading" class="list-state" role="status" aria-busy="true">{{ t('common.loading') }}</p>
    <p v-else-if="error" class="list-state" role="alert">{{ t('common.listLoadError') }}</p>
    <EmptyState v-else :title="emptyText" :icon="icon" />
  </template>
</template>

<style scoped>
.list-state { margin: 0; padding: var(--sp-6) var(--sp-4); text-align: center; color: var(--text-2); }
</style>
