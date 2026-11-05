<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useContentLanguages } from '../../composables/useContentLanguages'
import { useAuth } from '../../composables/useAuth'
import { useApi } from '../../composables/useApi'
import { useDeleteAction } from '../../composables/useAdminCrudActions'
import { useTenant } from '../../composables/useTenant'
import AppIcon from '../../components/AppIcon.vue'
import RelativeTime from '../../components/RelativeTime.vue'
import ListState from '../../components/ListState.vue'
import PageHeader from '../../components/PageHeader.vue'
import FormPanel from '../../components/FormPanel.vue'
import ListCard from '../../components/ListCard.vue'
import SearchBox from '../../components/SearchBox.vue'
import RowMenu from '../../components/RowMenu.vue'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import { useFormPanel } from '../../composables/useFormPanel'
import { formatRelative } from '../../utils/relativeTime'
import FormField from '../../components/FormField.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import EmptyState from '../../components/EmptyState.vue'
import { languageFlag, languageDisplayName } from '../../utils/languageFlags'
import { isValidLanguageCode, serverInputErrorText } from '../../utils/inputRules'

// Sprachverwaltung (Stage 1 von 5 der Mehrsprachigkeit): globaler
// Katalog (global-admin-only) + pro aktueller Statusseite aktivierte
// Sprachen inkl. Default. Ersetzt die vorherige, implizit auf en/de fixierte
// Konvention der Benachrichtigungs-Templates durch einen
// offen erweiterbaren Katalog.

const { catalog, pageLanguages, loadCatalog, loadPageLanguages, savePageLanguages, createLanguage, updateLanguage } = useContentLanguages()
const { isGlobalAdmin, hasPerm } = useAuth()
// Seitensprachen speichern verlangt im Backend languages:update
// (PutPageLanguagesHandler); lesen darf jede Rolle der Seite.
const canEditPageLanguages = computed(() => hasPerm('languages:update'))
const { apiFetch } = useApi()
const { activeTenantId } = useTenant()
const { t, locale } = useI18n()

const alertMsg = ref('')
const alertType = ref('info')
const isEditing = ref(false)
const currentEditItem = ref(null)
const formData = ref({})
const isSaving = ref(false)
const query = ref('')
const editName = ref('')
const errors = ref({ code: '', name: '' })

// Format des Sprachcodes (Backend: 400 language_code_invalid).
const codeError = (isNew) => {
  if (!isNew) return ''
  const code = (formData.value.code || '').trim()
  if (!code) return t('languages.codeRequired')
  return isValidLanguageCode(code) ? '' : t('inputErrors.language_code_invalid')
}
const { formEl, focusForm, focusInvalid } = useFormPanel()
const loading = ref(true)
// Stryker disable next-line BooleanLiteral: ListStateRow gibt loading Vorrang vor error und onMounted setzt loadFailed synchron zurück, der Startwert ist nicht beobachtbar
const loadFailed = ref(false)

const selectedCodes = ref([])
const selectedDefault = ref('')
const isSavingPageLanguages = ref(false)

// vor jeder neuen Aktion die Meldung des vorigen Schritts entfernen.
const clearAlert = () => { alertMsg.value = '' }
const showAlert = (msg, type = 'info') => {
  alertMsg.value = msg
  alertType.value = type
}

const loadAll = async () => {
  loading.value = true
  loadFailed.value = false
  try {
    await loadCatalog()
    if (activeTenantId.value) {
      await loadPageLanguages(activeTenantId.value)
      selectedCodes.value = pageLanguages.value.map(l => l.LanguageCode)
      selectedDefault.value = pageLanguages.value.find(l => l.IsDefault)?.LanguageCode || ''
    }
  } catch (err) {
    loadFailed.value = true
    showAlert(t('common.loadError') + err.message, 'error')
  } finally {
    loading.value = false
  }
}

onMounted(loadAll)
watch(activeTenantId, loadAll)

const activeLanguages = computed(() => catalog.value.filter(l => l.IsActive))

// selectedCodes ist an die native Checkbox-Gruppe unten per v-model gebunden
// (Vue verwaltet die Array-Mitgliedschaft dabei selbst) - hier nur noch
// nachziehen, dass die Default-Auswahl gültig bleibt, wenn ein Code
// abgewählt wird.
watch(selectedCodes, (codes) => {
  if (!codes.includes(selectedDefault.value)) {
    selectedDefault.value = codes[0] || ''
  }
})

const savePageSelection = async () => {
  if (selectedCodes.value.length === 0) {
    showAlert(t('languages.confirmMinOne'), 'warning')
    return
  }
  clearAlert()
  isSavingPageLanguages.value = true
  try {
    await savePageLanguages(activeTenantId.value, selectedCodes.value, selectedDefault.value)
    showAlert(t('languages.selectionSaved'), 'success')
  } catch (err) {
    showAlert(t('common.saveError') + err.message, 'error')
  } finally {
    isSavingPageLanguages.value = false
  }
}

const openEdit = (item) => {
  if (item === 'new') {
    formData.value = { code: '', name: '' }
  } else {
    formData.value = { code: item.Code, name: item.Name, is_active: item.IsActive }
  }
  currentEditItem.value = item === 'new' ? 'new' : item.ID
  editName.value = item === 'new' ? '' : item.Name
  errors.value = { code: '', name: '' }
  isEditing.value = true
  focusForm()
}

const cancelEdit = () => {
  isEditing.value = false
  currentEditItem.value = null
  errors.value = { code: '', name: '' }
  formData.value = {}
}

const saveItem = async () => {
  const isNew = currentEditItem.value === 'new'
  // Pflichtfelder: Fehler am Feld statt eines Serverfehlers, Fokus aufs erste fehlerhafte Feld.
  errors.value = {
    code: codeError(isNew),
    name: (formData.value.name || '').trim() ? '' : t('languages.nameRequired'),
  }
  if (errors.value.code || errors.value.name) {
    await focusInvalid()
    return
  }
  clearAlert()
  isSaving.value = true
  try {
    if (isNew) {
      await createLanguage(formData.value.code, formData.value.name)
    } else {
      await updateLanguage(currentEditItem.value, { name: formData.value.name, is_active: formData.value.is_active })
    }
    showAlert(t(isNew ? 'adminFeedback.languageCreated' : 'adminFeedback.languageUpdated'), 'success')
    cancelEdit()
  } catch (err) {
    if (err.data?.code === 'language_code_invalid') {
      errors.value = { ...errors.value, code: serverInputErrorText(err, t) }
      await focusInvalid()
    } else {
      showAlert(t('common.saveError') + err.message, 'error')
    }
  } finally {
    isSaving.value = false
  }
}

const formTitle = computed(() => currentEditItem.value === 'new'
  ? t('languages.createTitle')
  : t('languages.editTitle', { name: editName.value }))

// Richtung C: Seitenkopf mit Zählung, Suche, Zeilenmenü.
const breadcrumbs = usePageCrumbs(() => t('languages.title'))
const subtitle = computed(() => t('languages.subtitle', {
  languages: t('languages.countLanguages', { n: catalog.value.length }, catalog.value.length),
  active: activeLanguages.value.length,
}))
const searching = computed(() => query.value.trim() !== '')
const rows = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return catalog.value.filter(l => `${l.Code} ${l.Name}`.toLowerCase().includes(needle))
})
// Löschen steht allen Zeilen offen, der Server entscheidet (409 bei Standardsprache einer
// Seite oder vorhandenen Mail-Vorlagen) und liefert die Meldung unter api.errors.*.
const deletingId = ref(null)
const deleteAction = useDeleteAction({ apiFetch, deletingId, reload: loadCatalog, onStart: clearAlert, onDeleted: () => showAlert(t('adminFeedback.deleted'), 'success'), onError: (err) => showAlert(t('common.deleteError') + err.message, 'error') })
const deleteItem = (l) => deleteAction(`/api/languages/${l.ID}`, l.ID, t('languages.confirmDelete', { name: l.Name }))
const menuItems = (l) => [
  { key: 'edit', label: t('common.edit'), icon: 'pencil' },
  { key: 'delete', label: deletingId.value === l.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === l.ID },
]
const onMenuSelect = (l, key) => {
  if (key === 'edit') openEdit(l)
  else deleteItem(l)
}
</script>

<template>
  <div>
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('languages.title')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template #actions>
        <OnyxButton v-if="isGlobalAdmin" :label="t('languages.newButton')" @click="openEdit('new')" />
      </template>
    </PageHeader>
    <p class="panel-lead">{{ t('languages.catalogDescription') }}</p>

    <!-- Richtung C: Katalog links, Formular als Seitenkarte rechts. -->
    <div class="split" :class="{ 'split--open': isEditing }">
      <div class="split__main">
        <ListCard :title="t('languages.cardTitle')">
          <template #tools>
            <SearchBox v-model="query" :label="t('languages.searchLabel')" />
          </template>
          <p v-if="searching" class="sr-only" role="status">{{ t('languages.searchResult', { n: rows.length }, rows.length) }}</p>

          <table v-if="rows.length" class="data-table data-table--cards">
            <thead>
              <tr>
                <th scope="col">{{ t('languages.tableCode') }}</th>
                <th scope="col">{{ t('languages.tableLanguage') }}</th>
                <th scope="col">{{ t('languages.tableName') }}</th>
                <th scope="col">{{ t('languages.tableStatus') }}</th>
                <th scope="col">{{ t('languages.tableChanged') }}</th>
                <th v-if="isGlobalAdmin" scope="col" class="actions-col"><span class="sr-only">{{ t('common.actions') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="l in rows" :key="l.ID">
                <td :data-label="t('languages.tableCode')"><span v-if="languageFlag(l.Code)" class="lang-flag" aria-hidden="true">{{ languageFlag(l.Code) }}</span> <code>{{ l.Code }}</code></td>
                <td :data-label="t('languages.tableLanguage')">{{ languageDisplayName(l.Code, locale) }}</td>
                <td :data-label="t('languages.tableName')"><strong>{{ l.Name }}</strong></td>
                <td :data-label="t('languages.tableStatus')">
                  <StatusBadge :status="l.IsActive ? 'operational' : null" :label="l.IsActive ? t('languages.statusActive') : t('languages.statusInactive')" />
                </td>
                <td :data-label="t('languages.tableChanged')" class="cell-changed">
                  <RelativeTime :value="l.UpdatedAt" />
                </td>
                <td v-if="isGlobalAdmin" class="actions-col">
                  <RowMenu :label="t('languages.menuLabel', { name: l.Name })" :items="menuItems(l)" @select="key => onMenuSelect(l, key)" />
                </td>
              </tr>
            </tbody>
          </table>
          <EmptyState v-else-if="searching && catalog.length" :title="t('languages.noResults')" :text="t('languages.noResultsText')" icon="globe" />
          <ListState v-else :loading="loading" :error="loadFailed" :count="catalog.length" :empty-text="t('languages.emptyCatalog')" icon="globe" />
        </ListCard>

        <section v-if="activeTenantId" class="edit-card">
          <h3>{{ t('languages.pageLanguagesHeading') }}</h3>
          <p class="panel-lead">{{ t('languages.pageLanguagesDescription') }}</p>
          <div class="stack-list">
            <div v-for="l in activeLanguages" :key="l.Code" class="stack-list__row">
              <label class="checkbox-row">
                <input type="checkbox" :value="l.Code" v-model="selectedCodes" :disabled="!canEditPageLanguages" /> <span v-if="languageFlag(l.Code)" class="lang-flag" aria-hidden="true">{{ languageFlag(l.Code) }}</span> {{ l.Name }} ({{ l.Code }})
              </label>
              <label v-if="selectedCodes.includes(l.Code)" class="checkbox-row checkbox-row--sub">
                <input type="radio" name="default-lang" :value="l.Code" v-model="selectedDefault" :disabled="!canEditPageLanguages" /> {{ t('languages.defaultLabel') }}
              </label>
            </div>
          </div>
          <div v-if="canEditPageLanguages" class="form-actions">
            <OnyxButton :label="isSavingPageLanguages ? t('common.saving') : t('common.save')" @click="savePageSelection" :disabled="isSavingPageLanguages" />
          </div>
        </section>
      </div>

      <FormPanel v-if="isEditing" :title="formTitle" @close="cancelEdit">
        <form ref="formEl" novalidate @submit.prevent="saveItem" class="edit-form">
          <FormField v-if="currentEditItem === 'new'" v-slot="f" field-id="lang-code" :label="t('languages.fieldCode')" required :error="errors.code || null">
            <input :id="f.id" v-model="formData.code" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" maxlength="8" placeholder="fr" @input="errors.code = ''" />
          </FormField>
          <FormField v-slot="f" field-id="lang-name" :label="t('languages.fieldName')" required :error="errors.name || null">
            <input :id="f.id" v-model="formData.name" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('languages.namePlaceholder')" @input="errors.name = ''" />
          </FormField>
          <div class="form-group" v-if="currentEditItem !== 'new'">
            <label class="checkbox-row"><input type="checkbox" v-model="formData.is_active" /> {{ t('languages.fieldActive') }}</label>
          </div>
          <div class="form-actions">
            <OnyxButton :label="t('common.cancel')" mode="outline" @click="cancelEdit" type="button" :disabled="isSaving" />
            <OnyxButton :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>
      </FormPanel>
    </div>
  </div>
</template>
