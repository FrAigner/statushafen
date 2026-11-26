<script setup lang="ts" generic="T extends object">
/*
  Generic mobile-first table pattern (phase 6):
  - Tablet+ (>=768px): classic table in a horizontally scrollable
    .table-wrapper (for records that are still too wide at 768px).
  - Mobile (<768px): card list, one card per item with label/value rows per
    column instead of tightly squeezed table cells.
  Cell contents can be customized via named slots `cell-<column.key>` (e.g.
  for badges/avatars); without a slot the raw value `item[column.key]` is used.

  Redesign stage 3: with `dataTable` the component renders the shared table
  style (style.css: .data-table-wrapper > table.data-table, calm header,
  52 px rows, tabular-nums). The classes .data-table and .data-table-wrapper
  are kept on purpose, the e2e specs and all views look for them. Instead of
  two DOM variants (JS switching) there is only the table; below 860 px CSS
  turns it into cards, the cells carry `data-label` for that. Additionally:
  - Column `{ key, label, numeric: true }`: right-aligned, tabular-nums.
  - Item with `isGroup: true`: group row across the full width, text from the
    first column.
  - Slot `actions` (parameter `item`): last column with row actions; its
    header is visible to screen readers only.
*/
import { useI18n } from 'vue-i18n'
import { useIsMobile } from '../composables/useMediaQuery'

interface Column {
  key: string
  label: string
  numeric?: boolean
}

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  columns: Column[]
  items: T[]
  rowKey?: string
  loading?: boolean
  // null instead of a static string default, because a default stored right
  // here would not react to a later language switch (Vue evaluates prop
  // defaults only once, when the prop is first missing) - the reactive
  // fallback via t() happens in the template.
  loadingText?: string | null
  emptyText?: string | null
  dataTable?: boolean
}>(), {
  rowKey: 'id',
  loading: false,
  loadingText: null,
  emptyText: null,
  dataTable: false,
})
// Stryker restore all

// Items are arbitrary records; cells are looked up by the column key.
const field = (item: T, key: string): unknown => (item as Record<string, unknown>)[key]
const rowId = (item: T) => field(item, props.rowKey) as PropertyKey
const isGroupRow = (item: T) => Boolean(field(item, 'isGroup'))

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
            <template v-for="item in items" :key="rowId(item)">
              <tr v-if="isGroupRow(item)" class="data-table__group">
                <th :colspan="columns.length + ($slots.actions ? 1 : 0)" scope="colgroup">{{ field(item, columns[0]!.key) }}</th>
              </tr>
              <tr v-else>
                <td v-for="col in columns" :key="col.key" :data-label="col.label" :class="{ 'is-numeric': col.numeric }">
                  <slot :name="`cell-${col.key}`" :item="item" :column="col">{{ field(item, col.key) }}</slot>
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
        <div v-for="item in items" :key="rowId(item)" class="card">
          <div v-for="col in columns" :key="col.key" class="card-row">
            <span class="card-row-label">{{ col.label }}</span>
            <span class="card-row-value">
              <slot :name="`cell-${col.key}`" :item="item" :column="col">{{ field(item, col.key) }}</slot>
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
            <tr v-for="item in items" :key="rowId(item)">
              <td v-for="col in columns" :key="col.key">
                <slot :name="`cell-${col.key}`" :item="item" :column="col">{{ field(item, col.key) }}</slot>
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

/* Tablet+: scrollable table container instead of forced layout overflow */
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

/* Mobile: card list instead of a table */
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
