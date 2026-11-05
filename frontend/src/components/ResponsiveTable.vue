<script setup>
/*
  Generisches Tabellen-Pattern für Mobile-First (Phase 6):
  - Tablet+ (>=768px): klassische Tabelle in einem horizontal scrollbaren
    .table-wrapper (für Datensätze, die auch bei 768px noch zu breit sind).
  - Mobile (<768px): Card-Liste, eine Card pro Item mit Label/Value-Zeilen
    je Spalte statt eng gequetschter Tabellenzellen.
  Zellinhalte sind über benannte Slots `cell-<column.key>` anpassbar (z.B.
  für Badges/Avatare); ohne Slot wird der Rohwert `item[column.key]` genutzt.

  Redesign Stufe 3: Mit `dataTable` rendert die Komponente den
  gemeinsamen Tabellenstil (style.css: .data-table-wrapper > table.data-table,
  ruhiger Kopf, 52-px-Zeilen, tabular-nums). Die Klassen .data-table und
  .data-table-wrapper bleiben absichtlich erhalten, die e2e-Specs und alle
  Views suchen danach. Statt zweier DOM-Varianten (JS-Umschaltung) gibt es nur
  die Tabelle; unter 860 px macht CSS daraus Karten, die Zellen tragen dafür
  `data-label`. Zusätzlich:
  - Spalte `{ key, label, numeric: true }`: rechtsbündig, tabular-nums.
  - Item mit `isGroup: true`: Gruppenzeile über die ganze Breite, Text aus der
    ersten Spalte.
  - Slot `actions` (Parameter `item`): letzte Spalte mit Zeilenaktionen; ihr
    Kopf ist nur für Screenreader sichtbar.
*/
import { useI18n } from 'vue-i18n'
import { useIsMobile } from '../composables/useMediaQuery'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  columns: { type: Array, required: true },   // [{ key, label }]
  items: { type: Array, required: true },
  rowKey: { type: String, default: 'id' },
  loading: { type: Boolean, default: false },
  // null statt eines statischen String-Defaults, da ein direkt hier
  // hinterlegter Default nicht auf einen späteren Sprachwechsel reagieren
  // würde (Vue-Prop-Defaults werden nur einmal beim ersten Fehlen des Props
  // ausgewertet) - der reaktive Fallback per t() passiert im Template.
  loadingText: { type: String, default: null },
  emptyText: { type: String, default: null },
  dataTable: { type: Boolean, default: false },
})
// Stryker restore all

const { t } = useI18n()
const isMobile = useIsMobile()
</script>

<template>
  <div class="responsive-table">
    <div v-if="loading" class="pad-empty">{{ loadingText ?? t('common.loading') }}</div>

    <template v-else-if="dataTable">
      <div v-if="items.length === 0" class="pad-empty">{{ emptyText ?? t('common.noEntries') }}</div>
      <div v-else class="data-table-wrapper">
        <table class="data-table data-table--cards">
          <thead>
            <tr>
              <th v-for="col in columns" :key="col.key" scope="col" :class="{ 'is-numeric': col.numeric }">{{ col.label }}</th>
              <th v-if="$slots.actions" scope="col" class="actions-col"><span class="sr-only">{{ t('common.actions') }}</span></th>
            </tr>
          </thead>
          <tbody>
            <template v-for="item in items" :key="item[rowKey]">
              <tr v-if="item.isGroup" class="data-table__group">
                <th :colspan="columns.length + ($slots.actions ? 1 : 0)" scope="colgroup">{{ item[columns[0].key] }}</th>
              </tr>
              <tr v-else>
                <td v-for="col in columns" :key="col.key" :data-label="col.label" :class="{ 'is-numeric': col.numeric }">
                  <slot :name="`cell-${col.key}`" :item="item" :column="col">{{ item[col.key] }}</slot>
                </td>
                <td v-if="$slots.actions" class="actions-col"><slot name="actions" :item="item" /></td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </template>

    <template v-else-if="isMobile">
      <div v-if="items.length === 0" class="pad-empty">{{ emptyText ?? t('common.noEntries') }}</div>
      <div v-else class="card-grid">
        <div v-for="item in items" :key="item[rowKey]" class="card">
          <div v-for="col in columns" :key="col.key" class="card-row">
            <span class="card-row-label">{{ col.label }}</span>
            <span class="card-row-value">
              <slot :name="`cell-${col.key}`" :item="item" :column="col">{{ item[col.key] }}</slot>
            </span>
          </div>
        </div>
      </div>
    </template>

    <template v-else>
      <div v-if="items.length === 0" class="pad-empty">{{ emptyText ?? t('common.noEntries') }}</div>
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th v-for="col in columns" :key="col.key">{{ col.label }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in items" :key="item[rowKey]">
              <td v-for="col in columns" :key="col.key">
                <slot :name="`cell-${col.key}`" :item="item" :column="col">{{ item[col.key] }}</slot>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>

<style scoped>
.pad-empty { padding: 28px; text-align: center; color: var(--text-3, var(--text-3)); }

/* Tablet+: scrollbarer Tabellen-Container statt erzwungenem Layout-Overflow */
.table-wrapper {
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
  border-radius: var(--radius-md);
}
.table-wrapper table {
  width: 100%;
  min-width: 100%;
  border-collapse: collapse;
}
.table-wrapper th,
.table-wrapper td {
  padding: var(--row-pad, 10px 12px);
  text-align: left;
  border-bottom: 1px solid var(--border);
}
.table-wrapper th {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: .05em;
  color: var(--text-3, var(--text-3));
}

/* Mobile: Card-Liste statt Tabelle */
.card-grid {
  display: flex;
  flex-direction: column;
  gap: var(--gap-sm);
}
.card {
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-sm) var(--space-md);
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.card-row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 12px;
}
.card-row-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: .05em;
  color: var(--text-3, var(--text-3));
  flex-shrink: 0;
}
.card-row-value {
  font-size: 13px;
  text-align: right;
  min-width: 0;
}
</style>
