<script setup>
import { useI18n } from 'vue-i18n'

// Eine Tabellenzeile für Lade-, Fehler- und Leerzustand, damit
// Admin-Listen vor der ersten Antwort keinen falschen Leerzustand zeigen.
// Die Zeile erscheint nur, solange die Liste leer ist (count=0): vorhandene
// Zeilen bleiben beim erneuten Laden oder einem Ladefehler stehen.
// Vorrang: Laden > Fehler > Leer.
// Stryker disable all: defineProps wird aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  loading: { type: Boolean, default: false },
  error: { type: Boolean, default: false },
  count: { type: Number, required: true },
  colspan: { type: Number, required: true },
  emptyText: { type: String, required: true },
  cellClass: { type: String, default: 'table-empty' },
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
