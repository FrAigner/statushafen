<script setup>
import { useI18n } from 'vue-i18n'

// Gemeinsame Tag-Mehrfachauswahl (Ersatz für das frühere
// Country-Freitextfeld) - genutzt in AdminComponents/AdminIncidents/
// AdminMaintenance/AdminUsers, um die dort ursprünglich vierfach duplizierte
// Checkbox-Liste (SonarCloud New-Code-Duplication-Gate) an einer Stelle zu
// pflegen.
// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  tags: { type: Array, required: true },       // verfügbare Tags (aus GET /api/tags)
  selectedIds: { type: Array, required: true }, // aktuell ausgewählte Tag-IDs
  // null statt eines statischen String-Defaults, da ein direkt hier
  // hinterlegter Default nicht auf einen späteren Sprachwechsel reagieren
  // würde (Vue-Prop-Defaults werden nur einmal beim ersten Fehlen des Props
  // ausgewertet) - der reaktive Fallback per t() passiert stattdessen im Template.
  emptyHint: { type: String, default: null },
})
// Stryker restore all

const { t } = useI18n()
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['toggle'])
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
