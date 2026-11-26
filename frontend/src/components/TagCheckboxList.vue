<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { Tag } from '../types/api'

// Shared tag multi-select (replacement for the former country free-text
// field) - used in AdminComponents/AdminIncidents/AdminMaintenance/AdminUsers,
// to maintain the checkbox list that was originally duplicated four times
// there (SonarCloud new-code duplication gate) in one place.
// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  tags: Tag[] // available tags (from GET /api/tags)
  selectedIds: number[] // currently selected tag IDs
  // null instead of a static string default, because a default stored right
  // here would not react to a later language switch (Vue evaluates prop
  // defaults only once, when the prop is first missing) - the reactive
  // fallback via t() happens in the template instead.
  emptyHint?: string | null
}>(), {
  emptyHint: null,
})
// Stryker restore all

const { t } = useI18n()
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ toggle: [tagId: number] }>()
</script>

<template>
  <div>
    <div v-if="tags.length === 0" class="empty-state">{{ emptyHint ?? t('components.tagCheckboxList.emptyHint') }}</div>
    <div v-else class="tag-checkbox-list">
      <label v-for="t in tags" :key="t.ID" class="tag-checkbox">
        <input
          type="checkbox"
          :checked="selectedIds.includes(t.ID)"
          @change="emit('toggle', t.ID)"
        />
        {{ t.Name }}<span v-if="t.StatusPageID" class="tag-scope-hint"> ({{ $t('components.tagCheckboxList.pageScoped') }})</span>
      </label>
    </div>
  </div>
</template>

<style scoped>
.tag-checkbox-list {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  padding: 1rem;
  background-color: var(--surface-2);
  border-radius: var(--radius-lg);
  border: 1px solid var(--border);
}
.tag-checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  margin: 0;
}
.tag-scope-hint {
  color: var(--text-secondary);
  font-size: 0.85em;
}
</style>
