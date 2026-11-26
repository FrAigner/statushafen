<script setup lang="ts">
import { useI18n } from 'vue-i18n'

// One table row for the loading, error and empty state, so that admin
// lists do not show a false empty state before the first response.
// The row only appears while the list is empty (count=0): existing rows
// stay in place while reloading or after a load error.
// Precedence: loading > error > empty.
// Stryker disable all: Vue hoists defineProps out of setup, instrumented code must not reference local variables there
withDefaults(defineProps<{
  loading?: boolean
  error?: boolean
  count: number
  colspan: number
  emptyText: string
  cellClass?: string
}>(), {
  loading: false,
  error: false,
  cellClass: 'table-empty',
})
// Stryker restore all

const { t } = useI18n()
</script>

<template>
  <tr v-if="count === 0">
    <td v-if="loading" :colspan="colspan" :class="cellClass" role="status" aria-busy="true">{{ t('common.loading') }}</td>
    <td v-else-if="error" :colspan="colspan" :class="cellClass" role="alert">{{ t('common.listLoadError') }}</td>
    <td v-else :colspan="colspan" :class="cellClass">{{ emptyText }}</td>
  </tr>
</template>
