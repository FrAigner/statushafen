<script setup lang="ts">
/*
  Direction C, operations pages: relative time ("3 days ago"); the exact
  point in time is in the title and the datetime attribute. Without a value a
  dash appears. Wording and units: utils/relativeTime.ts.
*/
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatRelative } from '../utils/relativeTime'
import { formatDateTimeShort } from '../utils/datetime'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  value?: string | null
}>(), {
  value: null,
})
// Stryker restore all
const { locale } = useI18n()
const relative = computed(() => formatRelative(props.value, locale.value))
const absolute = computed(() => formatDateTimeShort(props.value, locale.value))
</script>

<template>
  <time v-if="value" class="cell-changed" :datetime="value" :title="absolute">{{ relative }}</time>
  <span v-else class="cell-changed text-muted">—</span>
</template>
