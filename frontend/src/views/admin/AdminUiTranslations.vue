<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { useContentLanguages } from '../../composables/useContentLanguages'
import { flatten } from '../../i18n/overrides'
import de from '../../i18n/locales/de.json'
import en from '../../i18n/locales/en.json'
import { SUPPORTED_LOCALES, getBaseMessages, loadLocaleMessages } from '../../i18n'
import type { LocaleMessages } from '../../i18n'
import PageHeader from '../../components/PageHeader.vue'
import EmptyState from '../../components/EmptyState.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import PaginationBar from '../../components/PaginationBar.vue'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { confirmDialog } from '../../composables/useConfirm'

/** One row of the editor: a flattened message key with its default and override text. */
interface UiTranslationRow {
  key: string
  defaultValue: unknown
  overrideValue: string
  hasOverride: boolean
}

// UI translation editor: admin-editable overrides for app UI texts (nav,
// buttons, form labels) without a code deploy.
// Deliberately WITHOUT requiresGlobalAdmin at router level (pattern: AdminLanguages.vue) -
// a page admin without global admin rights must be able to maintain the
// overrides of their own page; the global-admin-only part (scope "global")
// hides itself here via isGlobalAdmin.

const { t } = useI18n()
const { apiFetch } = useApi()
const { hasPerm, isGlobalAdmin } = useAuth()
const { activeTenantId } = useTenant()

// Default texts per language; further UI languages are loaded on selection.
const baseMessages = ref<{ en: LocaleMessages, [code: string]: LocaleMessages | undefined }>({ de, en })
const { catalog, pageLanguages, loadCatalog, loadPageLanguages } = useContentLanguages()

const scope = ref(isGlobalAdmin.value ? 'global' : 'page')
const language = ref('de')
const namespaceFilter = ref('all')
const searchText = ref('')

const canEditCurrentScope = computed(() => scope.value === 'global' ? isGlobalAdmin.value : hasPerm('ui_translations:update'))

// Part 1: offered are the UI languages (de, en) and all active languages of
// the catalog (scope "Global") or of the selected page, with their own name.
// If a language has no default texts of its own, the "Default" column shows
// the English text (English fallback).
const languageOptions = computed(() => {
  const names = new Map((Array.isArray(catalog.value) ? catalog.value : []).map(l => [l.Code, l.Name]))
  const codes = scope.value === 'global'
    ? [...names.keys()].filter(code => catalog.value.find(l => l.Code === code)?.IsActive)
    : (Array.isArray(pageLanguages.value) ? pageLanguages.value : []).map(l => l.LanguageCode)
  const extra = [...new Set(codes)].filter(code => !SUPPORTED_LOCALES.includes(code)).sort()
  return [...SUPPORTED_LOCALES, ...extra].map(code => ({ code, label: names.get(code) || (SUPPORTED_LOCALES.includes(code) ? t(`language.${code}`) : code) }))
})
const hasOwnDefaults = computed(() => Object.hasOwn(baseMessages.value, language.value))
const defaultMessages = computed(() => baseMessages.value[language.value] || baseMessages.value.en)
const languageLabels = computed(() => Object.fromEntries(languageOptions.value.map(o => [o.code, o.label])))

const namespaces = computed(() => Object.keys(defaultMessages.value).sort())

const allKeys = computed(() => Object.entries(flatten(defaultMessages.value)))

const overrides = ref<Record<string, string>>({})
const alertMsg = ref('')
const alertType = ref('warning')
const savingKey = ref<string | null>(null)
const loading = ref(false)

const breadcrumbs = usePageCrumbs(computed(() => t('uiTranslations.title')), { global: computed(() => scope.value === 'global') })

const basePath = computed(() => {
  if (scope.value === 'global') return '/api/admin/ui-overrides'
  return `/api/pages/${activeTenantId.value}/ui-overrides`
})

const showAlert = (msg: string, type = 'warning') => {
  alertMsg.value = msg
  alertType.value = type
}

const loadOverrides = async () => {
  if (scope.value === 'page' && !activeTenantId.value) {
    overrides.value = {}
    return
  }
  loading.value = true
  try {
    overrides.value = (await apiFetch(`${basePath.value}?language_code=${language.value}`)) || {}
  } catch (err) {
    showAlert(t('uiTranslations.loadError') + (err as Error).message, 'error')
  } finally {
    loading.value = false
  }
}

watch([scope, language, activeTenantId], loadOverrides, { immediate: true })
watch(language, async (code) => {
  if (Object.hasOwn(baseMessages.value, code) || !SUPPORTED_LOCALES.includes(code)) return
  if (await loadLocaleMessages(code)) baseMessages.value = { ...baseMessages.value, [code]: getBaseMessages(code) }
}, { immediate: true })

// Load the language lists (on error only de/en remain; the page stays usable).
const loadLanguageOptions = async () => {
  try {
    await loadCatalog()
    if (activeTenantId.value) await loadPageLanguages(activeTenantId.value)
  } catch {
    // the language list only extends the selection
  }
}
onMounted(loadLanguageOptions)
watch(activeTenantId, loadLanguageOptions)
// A language no longer offered in the new scope/on the new page switches to German.
watch(languageOptions, (options) => {
  if (!options.some(o => o.code === language.value)) language.value = 'de'
})

const rows = computed((): UiTranslationRow[] => {
  const ns = namespaceFilter.value
  const search = searchText.value.trim().toLowerCase()
  return allKeys.value
    .filter(([key]) => ns === 'all' || key === ns || key.startsWith(ns + '.'))
    .filter(([key, defaultValue]) => !search || key.toLowerCase().includes(search) || String(defaultValue).toLowerCase().includes(search))
    .map(([key, defaultValue]) => ({
      key,
      defaultValue,
      overrideValue: Object.hasOwn(overrides.value, key) ? overrides.value[key]! : '',
      hasOverride: Object.hasOwn(overrides.value, key),
    }))
})

// Unsaved input lives in this view's state, not in the DOM row. The list
// shows only one page; paging or filtering removes and rebuilds rows, the
// drafts are kept. They are separated per context (scope, page, language) so
// that a draft of the German version does not show up in the English field.
const draftContext = computed(() => `${scope.value}|${scope.value === 'page' ? activeTenantId.value : ''}|${language.value}`)
const draftValues = ref<Record<string, Record<string, string>>>({})
const contextDrafts = computed(() => draftValues.value[draftContext.value] || {})
const draftFor = (row: UiTranslationRow) => Object.hasOwn(contextDrafts.value, row.key) ? contextDrafts.value[row.key]! : row.overrideValue
const setDraft = (key: string, value: string) => {
  draftValues.value = { ...draftValues.value, [draftContext.value]: { ...contextDrafts.value, [key]: value } }
}
const dropDraft = (key: string) => {
  draftValues.value = { ...draftValues.value, [draftContext.value]: omitKey(contextDrafts.value, key) }
}

// Count over ALL texts, not just the visible page.
const customizedCount = computed(() => Object.keys(overrides.value).length)
const unsavedKeys = computed(() => Object.keys(contextDrafts.value)
  .filter(key => contextDrafts.value[key] !== (Object.hasOwn(overrides.value, key) ? overrides.value[key] : '')))
const saveableKeys = computed(() => unsavedKeys.value.filter(key => String(contextDrafts.value[key]).trim() !== ''))

// Pagination: 1,700 rows with one input field each at once made the page
// sluggish and unusable with keyboard and screen reader.
const page = ref(1)
const pageSize = ref(50)
const listRef = ref<HTMLElement | null>(null)
watch([scope, language, namespaceFilter, searchText, activeTenantId], () => { page.value = 1 })
const pagedRows = computed(() => {
  const maxPage = Math.max(1, Math.ceil(rows.value.length / pageSize.value))
  const current = Math.min(page.value, maxPage)
  return rows.value.slice((current - 1) * pageSize.value, current * pageSize.value)
})
// After a page change, focus moves into the list (header area of the table),
// so keyboard users continue right at the first entry of the new page.
const focusList = async () => {
  await nextTick()
  listRef.value?.focus()
}

// omitKey returns a shallow copy without the given key - a replacement for
// the destructure-with-rest pattern ({ [key]: _, ...rest } = obj), whose
// deliberately unused extraction variable SonarCloud flags as "unused".
const omitKey = (obj: Record<string, string>, key: string) => {
  const copy = { ...obj }
  delete copy[key]
  return copy
}

const saveRow = async (row: UiTranslationRow) => {
  savingKey.value = row.key
  try {
    await apiFetch(basePath.value, {
      method: 'PUT',
      body: JSON.stringify({ language_code: language.value, key: row.key, value: draftFor(row) }),
    })
    overrides.value = { ...overrides.value, [row.key]: draftFor(row) }
    dropDraft(row.key)
  } catch (err) {
    showAlert(t('uiTranslations.saveError') + (err as Error).message, 'error')
  } finally {
    savingKey.value = null
  }
}

// An empty field must never be stored as an override (that used to blank
// the text for everyone; the backend now rejects it with 400). Saving is
// therefore locked for an empty draft; removing only works via the separate
// reset button.
const canSave = (row: UiTranslationRow) => String(draftFor(row)).trim() !== ''

const scopeLabel = computed(() => scope.value === 'global' ? t('uiTranslations.scopeGlobal') : t('uiTranslations.scopePage'))

const resetRow = async (row: UiTranslationRow) => {
  // Confirmation with its extent (scope + language), so it is clear that only
  // exactly this entry falls back to the default text.
  if (!(await confirmDialog({ message: t('uiTranslations.confirmReset', { key: row.key, scope: scopeLabel.value, language: language.value }), confirmLabel: t('confirm.reset') }))) return
  savingKey.value = row.key
  try {
    await apiFetch(`${basePath.value}?language_code=${language.value}&key=${encodeURIComponent(row.key)}`, { method: 'DELETE' })
    overrides.value = omitKey(overrides.value, row.key)
    dropDraft(row.key)
  } catch (err) {
    showAlert(t('uiTranslations.resetError') + (err as Error).message, 'error')
  } finally {
    savingKey.value = null
  }
}

// Send all unsaved, valid changes one after another (the backend only knows
// single keys). Failed ones remain as drafts.
const savingAll = ref(false)
const saveAll = async () => {
  savingAll.value = true
  let failed = 0
  let lastError = ''
  for (const key of [...saveableKeys.value]) {
    const value = contextDrafts.value[key]!
    try {
      await apiFetch(basePath.value, {
        method: 'PUT',
        body: JSON.stringify({ language_code: language.value, key, value }),
      })
      overrides.value = { ...overrides.value, [key]: value }
      dropDraft(key)
    } catch (err) {
      failed++
      lastError = (err as Error).message
    }
  }
  savingAll.value = false
  if (failed > 0) showAlert(t('uiTranslations.saveAllError', { n: failed }) + lastError, 'error')
}
</script>

<template>
  <div>
    <PageHeader :level="2" :title="t('uiTranslations.title')" :subtitle="t('uiTranslations.subtitle')" :breadcrumbs="breadcrumbs" />

    <div v-if="alertMsg" :class="`alert alert-${alertType} mb-16`">{{ alertMsg }}</div>

    <p v-if="!hasOwnDefaults" class="mb-16 text-muted" role="status">{{ t('uiTranslations.noOwnDefaults', { language: languageLabels[language] }) }}</p>

    <!-- Direction C: "All texts" card, filter and search in the card header. -->
    <ListCard :title="t('uiTranslations.cardTitle')">
      <template #tools>
        <select v-if="isGlobalAdmin" v-model="scope" class="comp-filter" :aria-label="t('uiTranslations.ariaScope')">
          <option value="global">{{ t('uiTranslations.scopeGlobal') }}</option>
          <option value="page">{{ t('uiTranslations.scopePage') }}</option>
        </select>

        <select v-model="language" class="comp-filter" :aria-label="t('uiTranslations.ariaLanguage')">
          <option v-for="opt in languageOptions" :key="opt.code" :value="opt.code">{{ opt.label }}</option>
        </select>

        <select v-model="namespaceFilter" class="comp-filter" :aria-label="t('uiTranslations.ariaNamespace')">
          <option value="all">{{ t('uiTranslations.allNamespaces') }}</option>
          <option v-for="ns in namespaces" :key="ns" :value="ns">{{ ns }}</option>
        </select>

        <span v-if="!loading && !(scope === 'page' && !activeTenantId)" class="count-note" role="status">
          {{ t('uiTranslations.customizedCount', { n: customizedCount }) }}<template v-if="unsavedKeys.length"> · {{ t('uiTranslations.unsavedCount', { n: unsavedKeys.length }) }}</template>
        </span>
        <button v-if="canEditCurrentScope && saveableKeys.length" type="button" class="action-btn" :disabled="savingAll" @click="saveAll">{{ t('uiTranslations.saveAll', { n: saveableKeys.length }) }}</button>

        <SearchBox v-model="searchText" :label="t('uiTranslations.searchPlaceholder')" :placeholder="t('uiTranslations.searchPlaceholder')" />
      </template>

      <EmptyState v-if="scope === 'page' && !activeTenantId" :title="t('uiTranslations.noActivePage')" icon="globe" />

      <div v-else-if="loading" class="pad-empty" role="status">{{ t('common.loading') }}</div>

      <EmptyState v-else-if="rows.length === 0" :title="t('uiTranslations.empty')" icon="search" />

      <!-- Deliberately no ResponsiveTable: the list has over 1,700 rows with one
           input field each; the slot overhead per cell made mounting about three
           times slower (now paged, so switching to ResponsiveTable would be
           possible, but is a separate step). For the same reason there is no row
           menu per row (save and reset stay buttons). The table does use the same
           table style (.data-table, data-label for the card view below 860 px). -->
      <div v-else ref="listRef" tabindex="-1" role="region" class="data-table-wrapper table-scroll" :aria-label="t('uiTranslations.cardTitle')">
        <table class="data-table data-table--cards ui-translations-table">
          <colgroup>
            <col class="col-key" />
            <col class="col-default" />
            <col class="col-override" />
            <col v-if="canEditCurrentScope" class="col-actions" />
          </colgroup>
          <thead>
            <tr>
              <th scope="col">{{ t('uiTranslations.columnKey') }}</th>
              <th scope="col">{{ t('uiTranslations.columnDefault') }}</th>
              <th scope="col">{{ t('uiTranslations.columnOverride') }}</th>
              <th v-if="canEditCurrentScope" scope="col" class="actions-col"><span class="sr-only">{{ t('common.actions') }}</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in pagedRows" :key="row.key" class="override-row">
              <td :data-label="t('uiTranslations.columnKey')">
                <span class="ent-mono">{{ row.key }}</span>
                <StatusBadge v-if="row.hasOverride" class="override-badge" status="maintenance" :label="t('uiTranslations.customized')" />
              </td>
              <td :data-label="t('uiTranslations.columnDefault')" class="text-muted">{{ row.defaultValue }}</td>
              <td :data-label="t('uiTranslations.columnOverride')">
                <template v-if="canEditCurrentScope">
                  <label class="sr-only" :for="`override-input-${row.key}`">{{ t('uiTranslations.columnOverride') }}: {{ row.key }}</label>
                  <input
                    :id="`override-input-${row.key}`"
                    type="text"
                    :value="draftFor(row)"
                    @input="setDraft(row.key, ($event.target as HTMLInputElement).value)"
                    :placeholder="t('uiTranslations.overridePlaceholder')"
                    class="form-input override-input"
                  />
                </template>
                <span v-else class="text-muted">{{ row.overrideValue }}</span>
              </td>
              <td v-if="canEditCurrentScope" class="actions-col">
                <button type="button" class="action-btn" :disabled="savingKey === row.key || !canSave(row)" :title="canSave(row) ? undefined : t('uiTranslations.saveEmptyHint')" @click="saveRow(row)">{{ t('common.save') }}</button>
                <button v-if="row.hasOverride" type="button" class="action-btn" :disabled="savingKey === row.key" @click="resetRow(row)">{{ t('uiTranslations.resetButton') }}</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <PaginationBar v-if="rows.length > 0 && !loading && !(scope === 'page' && !activeTenantId)" v-model:page="page" v-model:page-size="pageSize" :total="rows.length" @navigate="focusList" />
    </ListCard>
  </div>
</template>

<style scoped>
.mb-16 { margin-bottom: var(--sp-4); }
.pad-empty { padding: var(--sp-6); text-align: center; color: var(--text-3); }
.count-note { font-size: var(--fs-sm); color: var(--text-3); white-space: nowrap; }
.override-badge { margin-inline-start: var(--sp-2); }
.ent-mono { font-family: var(--font-mono, monospace); font-size: 0.85rem; }
/* At 1440 px the table was 1619 px wide (long keys without a break point,
   min-width on the input field); override and actions lay off to the right.
   A fixed column split with breaks anywhere keeps it within the width; on
   narrow screens only the table scrolls, not the whole page. */
.table-scroll { overflow-x: auto; max-width: 100%; }
.ui-translations-table { table-layout: fixed; min-width: 640px; }
.ui-translations-table td { overflow-wrap: anywhere; }
.ui-translations-table td.actions-col { white-space: normal; }
.col-key { width: 26%; }
.col-default { width: 30%; }
.col-override { width: 28%; }
.col-actions { width: 16%; }
.override-input { inline-size: 100%; box-sizing: border-box; }
.action-btn { margin: 2px 6px 2px 0; }
@media (max-width: 859px) {
  .ui-translations-table { min-width: 0; table-layout: auto; }
  .ui-translations-table colgroup { display: none; }
}
</style>
