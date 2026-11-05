<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RESOURCES, ACTIONS } from '../utils/permissions'
import { NAV_GROUPS } from '../utils/adminNav'
import {
  ACTION_ABBREVIATION,
  isOn,
  setCell,
  setColumn,
  setRow,
  rowState,
  columnState,
  matrixState,
  setStandalone,
} from '../utils/permissionMatrix'

// Berechtigungs-Matrix als Tabelle. Zeilen sind die Tabs der
// Navigation, gruppiert nach Bereich (Betrieb, Inhalte, Verwaltung, System),
// Spalten die Aktionen C R U D (Anlegen, Lesen, Ändern, Löschen). Zeilen- und
// Spaltenschalter setzen ganze Reihen. Anlegen, Ändern und Löschen setzen Lesen
// mit (siehe utils/permissionMatrix.js). Das Modell bleibt ein Array derselben
// Rechte-Strings (v-model), das Backend sieht keinen Unterschied.
//
// Aufbau gegen Abschneiden: feste Spaltenbreiten, die Bereichsspalte bricht
// Text um, und reicht der Platz trotzdem nicht, scrollt nur .perm-matrix-scroll
// (fokussierbar, damit Tastaturnutzer mitscrollen können), nie die Seite.
// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  modelValue: { type: Array, required: true }, // gewählte Rechte
  standalone: { type: Array, default: () => [] }, // Sonderrechte ohne Aktionen (admin_dashboard)
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['update:modelValue'])
const { t, te } = useI18n()

const scopeOf = Object.fromEntries(RESOURCES.map(r => [r.key, r.scope]))

// Gruppen mit ihren Zeilen: Beschriftung = Tab-Beschriftung der Navigation.
const groups = computed(() => NAV_GROUPS
  .map(group => ({
    key: group.key,
    label: t(`adminNav.groups.${group.key}`),
    rows: group.items
      .filter(item => item.resource)
      .map(item => ({
        resource: item.resource,
        label: t(`adminNav.items.${item.labelKey}`),
        locked: scopeOf[item.resource] === 'global',
      })),
  }))
  .filter(group => group.rows.length))

// Nur Seitenressourcen sind wählbar; globale Zeilen sind gesperrt (nur Global Admins).
const editable = computed(() => groups.value.flatMap(g => g.rows).filter(r => !r.locked).map(r => r.resource))

const actionText = (action) => t(`roles.matrix.${action}`)
const standaloneLabel = (key) => {
  const path = `roles.matrix.labels.${key}`
  return te(path) ? t(path) : key
}

const model = computed(() => props.modelValue)
const update = (next) => emit('update:modelValue', next)

const toggleCell = (resource, action, enabled) => update(setCell(model.value, resource, action, enabled))
const toggleRow = (resource, enabled) => update(setRow(model.value, resource, enabled))
const toggleColumn = (action, enabled) => update(setColumn(model.value, editable.value, action, enabled))
const toggleAll = (enabled) => update(editable.value.reduce((acc, resource) => setRow(acc, resource, enabled), model.value))
const toggleStandalone = (key, enabled) => update(setStandalone(model.value, key, enabled))
</script>

<template>
  <fieldset class="perm-matrix">
    <legend>{{ t('roles.fieldPermissions') }}</legend>
    <p class="perm-hint">{{ t('roles.matrix.hint') }}</p>

    <div
      class="perm-matrix-scroll"
      role="region"
      tabindex="0"
      :aria-label="t('roles.matrix.regionLabel')"
    >
      <table class="perm-table">
        <thead>
          <tr>
            <th scope="col" class="perm-area-col">
              <label class="perm-head perm-head--area">
                <input
                  type="checkbox"
                  data-column="all"
                  :aria-label="t('roles.matrix.matrixAll')"
                  :checked="matrixState(model, editable) === 'all'"
                  :indeterminate="matrixState(model, editable) === 'some'"
                  @change="toggleAll($event.target.checked)"
                />
                <span>{{ t('roles.matrix.area') }}</span>
              </label>
            </th>
            <th v-for="action in ACTIONS" :key="action" scope="col" class="perm-action-col">
              <label class="perm-head">
                <input
                  type="checkbox"
                  :data-column="action"
                  :aria-label="t('roles.matrix.columnAll', { action: actionText(action) })"
                  :checked="columnState(model, editable, action) === 'all'"
                  :indeterminate="columnState(model, editable, action) === 'some'"
                  @change="toggleColumn(action, $event.target.checked)"
                />
                <span class="perm-abbr" aria-hidden="true">{{ ACTION_ABBREVIATION[action] }}</span>
                <span class="perm-action-text" aria-hidden="true">{{ actionText(action) }}</span>
              </label>
            </th>
          </tr>
        </thead>
        <tbody v-for="group in groups" :key="group.key" :data-group="group.key">
          <tr class="perm-group-row">
            <th scope="rowgroup" colspan="5">{{ group.label }}</th>
          </tr>
          <tr v-for="row in group.rows" :key="row.resource" :data-area="row.resource" :class="{ 'perm-row--locked': row.locked }">
            <th scope="row" class="perm-area-col">
              <label class="perm-row-label" :title="row.resource">
                <input
                  type="checkbox"
                  data-row-all
                  :disabled="row.locked"
                  :aria-label="t('roles.matrix.rowAll', { area: row.label })"
                  :checked="rowState(model, row.resource) === 'all'"
                  :indeterminate="rowState(model, row.resource) === 'some'"
                  @change="toggleRow(row.resource, $event.target.checked)"
                />
                <span class="perm-label">{{ row.label }}</span>
              </label>
              <span v-if="row.locked" class="perm-badge">{{ t('roles.matrix.globalOnly') }}</span>
            </th>
            <td v-for="action in ACTIONS" :key="action" class="perm-action-col">
              <input
                type="checkbox"
                :value="`${row.resource}:${action}`"
                :disabled="row.locked"
                :title="`${row.resource}:${action}`"
                :aria-label="`${row.label} – ${actionText(action)}`"
                :checked="isOn(model, row.resource, action)"
                @change="toggleCell(row.resource, action, $event.target.checked)"
              />
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="standalone.length" class="perm-access">
      <h4 class="perm-access-title">{{ t('roles.matrix.access') }}</h4>
      <label v-for="key in standalone" :key="key" class="perm-access-item">
        <input
          type="checkbox"
          :value="key"
          :title="key"
          :checked="model.includes(key)"
          @change="toggleStandalone(key, $event.target.checked)"
        />
        <span class="perm-label">{{ standaloneLabel(key) }}</span>
        <code class="perm-key">{{ key }}</code>
      </label>
    </div>
  </fieldset>
</template>

<style scoped>
.perm-matrix {
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 12px 12px 14px;
  margin: 0 0 1rem;
  /* Ein fieldset wächst sonst mit seinem Inhalt über die Formularbreite hinaus. */
  min-width: 0;
  max-width: 100%;
  background: var(--surface);
  color: var(--text);
}
.perm-matrix legend {
  padding: 0 6px;
  font-weight: 600;
}
.perm-hint {
  margin: 0 0 8px;
  font-size: 0.8rem;
  color: var(--text-2);
}
/* Reicht der Platz nicht, scrollt nur die Matrix, nie die Seite. */
.perm-matrix-scroll {
  max-width: 100%;
  overflow-x: auto;
}
.perm-matrix-scroll:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: 2px;
}
.perm-table {
  width: 100%;
  min-width: 20rem;
  border-collapse: collapse;
  table-layout: fixed;
}
.perm-table th,
.perm-table td {
  padding: 4px 2px;
  border-bottom: 1px solid var(--border);
  text-align: center;
  vertical-align: middle;
}
.perm-table .perm-action-col {
  width: 2.9rem;
}
.perm-table thead th {
  background: var(--surface-2);
  color: var(--text-2);
  font-size: 0.75rem;
  font-weight: 600;
  vertical-align: bottom;
}
.perm-table .perm-area-col {
  text-align: left;
  font-weight: 500;
  padding-inline-start: 4px;
  overflow-wrap: break-word;
}
.perm-group-row th {
  text-align: left;
  padding: 6px 4px;
  background: var(--surface-2);
  color: var(--text-2);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.perm-table tbody tr:not(.perm-group-row):hover {
  background: var(--surface-2);
}
.perm-row--locked {
  color: var(--text-3);
}
.perm-head {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  margin: 0;
  cursor: pointer;
}
.perm-head--area {
  align-items: flex-start;
  flex-direction: row;
  gap: 6px;
}
.perm-abbr {
  font-size: 0.85rem;
  font-weight: 700;
  color: var(--text);
}
.perm-action-text {
  font-size: 0.65rem;
  font-weight: 500;
  line-height: 1.1;
  overflow-wrap: break-word;
}
.perm-row-label {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  cursor: pointer;
}
.perm-row--locked .perm-row-label {
  cursor: not-allowed;
}
.perm-label {
  font-size: 0.85rem;
  line-height: 1.2;
  min-width: 0;
  /* Auch ein langes Wort bricht um, statt in die nächste Spalte zu laufen. */
  overflow-wrap: anywhere;
}
.perm-badge {
  display: block;
  margin-inline-start: 24px;
  font-size: 0.65rem;
  font-weight: 500;
  color: var(--text-3);
}
.perm-key {
  font-family: var(--font-mono);
  font-size: 0.75rem;
  font-weight: 400;
  color: var(--text-3);
}
.perm-matrix input[type="checkbox"] {
  width: 18px;
  height: 18px;
  margin: 0;
  flex: none;
  cursor: pointer;
  accent-color: var(--primary);
}
.perm-matrix input[type="checkbox"]:disabled {
  cursor: not-allowed;
}
.perm-matrix input[type="checkbox"]:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: 2px;
}
.perm-access {
  margin-top: 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.perm-access-title {
  margin: 0;
  font-size: 0.85rem;
  font-weight: 600;
  color: var(--text-2);
}
.perm-access-item {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin: 0;
  cursor: pointer;
}
</style>
