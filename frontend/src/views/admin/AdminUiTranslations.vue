<script setup>
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
import PageHeader from '../../components/PageHeader.vue'
import EmptyState from '../../components/EmptyState.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import PaginationBar from '../../components/PaginationBar.vue'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { confirmDialog } from '../../composables/useConfirm'

// UI-Übersetzungs-Editor: Admin-editierbare Overrides
// für App-UI-Texte (Nav, Buttons, Formularlabels), ohne Code-Deploy.
// Bewusst OHNE requiresGlobalAdmin auf Router-Ebene (Muster: AdminLanguages.vue) -
// ein Seiten-Admin ohne Global-Admin-Rechte muss die Overrides seiner
// eigenen Seite pflegen können, der global-admin-only Teil (Scope "global")
// blendet sich hier per isGlobalAdmin aus.

const { t } = useI18n()
const { apiFetch } = useApi()
const { hasPerm, isGlobalAdmin } = useAuth()
const { activeTenantId } = useTenant()

// Standardtexte je Sprache; weitere Oberflächensprachen werden bei Auswahl nachgeladen.
const baseMessages = ref({ de, en })
const { catalog, pageLanguages, loadCatalog, loadPageLanguages } = useContentLanguages()

const scope = ref(isGlobalAdmin.value ? 'global' : 'page')
const language = ref('de')
const namespaceFilter = ref('all')
const searchText = ref('')

const canEditCurrentScope = computed(() => scope.value === 'global' ? isGlobalAdmin.value : hasPerm('ui_translations:update'))

// Teil 1: Angeboten werden die Oberflächensprachen (de, en) und alle
// aktiven Sprachen des Katalogs (Bereich „Global“) bzw. der gewählten Seite, mit
// Eigenbezeichnung. Gibt es für eine Sprache keine eigenen Standardtexte, zeigt die
// Spalte „Standard“ den englischen Text (Rückfall Englisch).
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

const overrides = ref({})
const alertMsg = ref('')
const alertType = ref('warning')
const savingKey = ref(null)
const loading = ref(false)

const breadcrumbs = usePageCrumbs(computed(() => t('uiTranslations.title')), { global: computed(() => scope.value === 'global') })

const basePath = computed(() => {
  if (scope.value === 'global') return '/api/admin/ui-overrides'
  return `/api/pages/${activeTenantId.value}/ui-overrides`
})

const showAlert = (msg, type = 'warning') => {
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
    showAlert(t('uiTranslations.loadError') + err.message, 'error')
  } finally {
    loading.value = false
  }
}

watch([scope, language, activeTenantId], loadOverrides, { immediate: true })
watch(language, async (code) => {
  if (Object.hasOwn(baseMessages.value, code) || !SUPPORTED_LOCALES.includes(code)) return
  if (await loadLocaleMessages(code)) baseMessages.value = { ...baseMessages.value, [code]: getBaseMessages(code) }
}, { immediate: true })

// Sprachlisten laden (Fehler: nur de/en bleiben, die Seite bleibt benutzbar).
const loadLanguageOptions = async () => {
  try {
    await loadCatalog()
    if (activeTenantId.value) await loadPageLanguages(activeTenantId.value)
  } catch {
    // Sprachliste ist nur eine Erweiterung der Auswahl
  }
}
onMounted(loadLanguageOptions)
watch(activeTenantId, loadLanguageOptions)
// Eine Sprache, die im neuen Bereich/auf der neuen Seite nicht mehr angeboten wird, wechselt auf Deutsch.
watch(languageOptions, (options) => {
  if (!options.some(o => o.code === language.value)) language.value = 'de'
})

const rows = computed(() => {
  const ns = namespaceFilter.value
  const search = searchText.value.trim().toLowerCase()
  return allKeys.value
    .filter(([key]) => ns === 'all' || key === ns || key.startsWith(ns + '.'))
    .filter(([key, defaultValue]) => !search || key.toLowerCase().includes(search) || String(defaultValue).toLowerCase().includes(search))
    .map(([key, defaultValue]) => ({
      key,
      defaultValue,
      overrideValue: Object.hasOwn(overrides.value, key) ? overrides.value[key] : '',
      hasOverride: Object.hasOwn(overrides.value, key),
    }))
})

// Ungespeicherte Eingaben liegen im Zustand dieser Ansicht, nicht
// in der DOM-Zeile. Die Liste zeigt nur eine Seite; beim Blättern oder
// Filtern werden Zeilen entfernt und neu aufgebaut, die Entwürfe bleiben
// erhalten. Sie sind je Kontext (Bereich, Seite, Sprache) getrennt, damit ein
// Entwurf der deutschen Fassung nicht im englischen Feld auftaucht.
const draftContext = computed(() => `${scope.value}|${scope.value === 'page' ? activeTenantId.value : ''}|${language.value}`)
const draftValues = ref({})
const contextDrafts = computed(() => draftValues.value[draftContext.value] || {})
const draftFor = (row) => Object.hasOwn(contextDrafts.value, row.key) ? contextDrafts.value[row.key] : row.overrideValue
const setDraft = (key, value) => {
  draftValues.value = { ...draftValues.value, [draftContext.value]: { ...contextDrafts.value, [key]: value } }
}
const dropDraft = (key) => {
  draftValues.value = { ...draftValues.value, [draftContext.value]: omitKey(contextDrafts.value, key) }
}

// Zählung über ALLE Texte, nicht nur die sichtbare Seite.
const customizedCount = computed(() => Object.keys(overrides.value).length)
const unsavedKeys = computed(() => Object.keys(contextDrafts.value)
  .filter(key => contextDrafts.value[key] !== (Object.hasOwn(overrides.value, key) ? overrides.value[key] : '')))
const saveableKeys = computed(() => unsavedKeys.value.filter(key => String(contextDrafts.value[key]).trim() !== ''))

// Paginierung: 1.700 Zeilen mit je einem Eingabefeld auf einmal
// machten die Seite träge und für Tastatur und Screenreader unbedienbar.
const page = ref(1)
const pageSize = ref(50)
const listRef = ref(null)
watch([scope, language, namespaceFilter, searchText, activeTenantId], () => { page.value = 1 })
const pagedRows = computed(() => {
  const maxPage = Math.max(1, Math.ceil(rows.value.length / pageSize.value))
  const current = Math.min(page.value, maxPage)
  return rows.value.slice((current - 1) * pageSize.value, current * pageSize.value)
})
// Fokus nach Seitenwechsel in die Liste (Überschriftenbereich der Tabelle),
// damit Tastaturnutzer direkt beim ersten Eintrag der neuen Seite weitermachen.
const focusList = async () => {
  await nextTick()
  listRef.value?.focus()
}

// omitKey liefert eine flache Kopie ohne den angegebenen Key - Ersatz für das
// Destrukturieren-mit-Rest-Muster ({ [key]: _, ...rest } = obj), dessen
// absichtlich ungenutzte Extraktionsvariable von SonarCloud als "unused"
// bemängelt wird.
const omitKey = (obj, key) => {
  const copy = { ...obj }
  delete copy[key]
  return copy
}

const saveRow = async (row) => {
  savingKey.value = row.key
  try {
    await apiFetch(basePath.value, {
      method: 'PUT',
      body: JSON.stringify({ language_code: language.value, key: row.key, value: draftFor(row) }),
    })
    overrides.value = { ...overrides.value, [row.key]: draftFor(row) }
    dropDraft(row.key)
  } catch (err) {
    showAlert(t('uiTranslations.saveError') + err.message, 'error')
  } finally {
    savingKey.value = null
  }
}

// Ein leeres Feld darf nie als Override gespeichert werden (das
// hat den Text bisher für alle geleert, das Backend lehnt es jetzt mit 400
// ab). Speichern ist deshalb bei leerem Entwurf gesperrt; Entfernen geht nur
// über den eigenen Zurücksetzen-Knopf.
const canSave = (row) => String(draftFor(row)).trim() !== ''

const scopeLabel = computed(() => scope.value === 'global' ? t('uiTranslations.scopeGlobal') : t('uiTranslations.scopePage'))

const resetRow = async (row) => {
  // Bestätigung mit Umfang (Scope + Sprache), damit klar ist, dass nur genau
  // dieser Eintrag auf den Standardtext zurückfällt.
  if (!(await confirmDialog({ message: t('uiTranslations.confirmReset', { key: row.key, scope: scopeLabel.value, language: language.value }), confirmLabel: t('confirm.reset') }))) return
  savingKey.value = row.key
  try {
    await apiFetch(`${basePath.value}?language_code=${language.value}&key=${encodeURIComponent(row.key)}`, { method: 'DELETE' })
    overrides.value = omitKey(overrides.value, row.key)
    dropDraft(row.key)
  } catch (err) {
    showAlert(t('uiTranslations.resetError') + err.message, 'error')
  } finally {
    savingKey.value = null
  }
}

// Alle ungespeicherten, gültigen Änderungen nacheinander senden (das Backend
// kennt nur einzelne Keys). Fehlgeschlagene bleiben als Entwurf stehen.
const savingAll = ref(false)
const saveAll = async () => {
  savingAll.value = true
  let failed = 0
  let lastError = ''
  for (const key of [...saveableKeys.value]) {
    const value = contextDrafts.value[key]
    try {
      await apiFetch(basePath.value, {
        method: 'PUT',
        body: JSON.stringify({ language_code: language.value, key, value }),
      })
      overrides.value = { ...overrides.value, [key]: value }
      dropDraft(key)
    } catch (err) {
      failed++
      lastError = err.message
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

    <!-- Richtung C: Karte „Alle Texte“, Filter und Suche im Kartenkopf. -->
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

      <!-- Bewusst keine ResponsiveTable: Die Liste hat über 1.700 Zeilen mit je einem
           Eingabefeld; der Slot-Aufwand pro Zelle machte den Mount rund dreimal
           so langsam (jetzt seitenweise; die Umstellung auf ResponsiveTable
           wäre damit möglich, ist aber ein eigener Schritt). Aus demselben Grund gibt es hier kein Zeilenmenü je Zeile
           (Speichern und Zurücksetzen bleiben Knöpfe). Die Tabelle nutzt aber
           denselben Tabellenstil (.data-table, data-label für die Kartenansicht
           unter 860 px). -->
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
                    @input="setDraft(row.key, $event.target.value)"
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
/* Die Tabelle war bei 1440 px 1619 px breit (lange Keys ohne
   Umbruchstelle, min-width am Eingabefeld), Override und Aktionen lagen
   rechts außerhalb. Feste Spaltenaufteilung mit Umbruch an beliebiger
   Stelle hält sie in der Breite; auf schmalen Bildschirmen scrollt nur die
   Tabelle, nicht die ganze Seite. */
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
