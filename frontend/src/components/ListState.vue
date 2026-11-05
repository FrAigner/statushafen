<script setup>
/*
  Redesign Stufe 4b: Lade-, Fehler- und Leerzustand einer
  Admin-Liste ohne Tabellenzeile (Nachfolger von ListStateRow für Views, die
  ResponsiveTable mit dataTable nutzen). Erscheint nur, solange die Liste leer
  ist (count = 0): vorhandene Zeilen bleiben beim erneuten Laden oder einem
  Ladefehler stehen. Vorrang: Laden > Fehler > Leer.
  Der Leerzustand nutzt EmptyState (Titel = Leertext der View).
*/
import { useI18n } from 'vue-i18n'
import EmptyState from './EmptyState.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  loading: { type: Boolean, default: false },
  error: { type: Boolean, default: false },
  count: { type: Number, required: true },
  emptyText: { type: String, required: true },
  icon: { type: String, default: null },
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
