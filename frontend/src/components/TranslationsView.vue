<script setup>
/*
  Richtung C: Gemeinsame Oberfläche der Admin-Übersetzungsseiten
  für Komponenten und Ankündigungen (beide waren bis auf Inhaltsfeld und Texte
  identisch). Seitenkopf mit Brotkrumen, Listenkarte mit Zeilenmenü
  (Bestätigen, Löschen) und Formular als Seitenkarte. Die Logik (Laden, Speichern,
  Bestätigen, Löschen) bleibt in useAdminTranslationsPanel; diese Komponente
  zeigt nur an und meldet per Ereignis zurück.

  - contentKey: Feld der Übersetzung in der Liste ('Description' / 'Message').
  - contentFormKey: Feld im Formular ('description' / 'message').
  - form ist das Formularobjekt des Panels (language_code, title, Inhalt).
*/
import { computed, nextTick, ref } from 'vue'
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'
import EmptyState from './EmptyState.vue'
import FormField from './FormField.vue'
import FormPanel from './FormPanel.vue'
import ListCard from './ListCard.vue'
import PageHeader from './PageHeader.vue'
import ResponsiveTable from './ResponsiveTable.vue'
import RichTextEditor from './RichTextEditor.vue'
import RowMenu from './RowMenu.vue'
import StatusBadge from './StatusBadge.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  title: { type: String, required: true },
  subtitle: { type: String, default: null },
  breadcrumbs: { type: Array, default: () => [] },
  alertMsg: { type: String, default: '' },
  alertType: { type: String, default: 'info' },
  canCreate: { type: Boolean, default: false },
  canUpdate: { type: Boolean, default: false },
  canDelete: { type: Boolean, default: false },
  isEditing: { type: Boolean, default: false },
  isSaving: { type: Boolean, default: false },
  translations: { type: Array, default: () => [] },
  languages: { type: Array, default: () => [] },
  confirmingId: { type: [Number, String], default: null },
  deletingId: { type: [Number, String], default: null },
  contentKey: { type: String, required: true },
  contentFormKey: { type: String, required: true },
  contentColumnLabel: { type: String, required: true },
  contentFieldLabel: { type: String, required: true },
  contentPlaceholder: { type: String, default: '' },
  // Inhalt ist formatierter Text (Markdown) und bekommt den Editor.
  richText: { type: Boolean, default: false },
  // Formularobjekt des Panels (language_code, title, Inhalt); die Felder schreiben direkt hinein.
  form: { type: Object, default: () => ({}) },
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['new', 'cancel', 'save', 'confirm', 'delete', 'dismiss'])

const { t } = useI18n()

const columns = computed(() => [
  { key: 'LanguageCode', label: t('translations.tableLanguage') },
  { key: 'Title', label: t('translations.tableTitle') },
  { key: props.contentKey, label: props.contentColumnLabel },
  { key: 'Status', label: t('translations.tableStatus') },
])

const menuItems = (trans) => [
  ...(trans.Status === 'draft' && props.canUpdate
    ? [{ key: 'confirm', label: props.confirmingId === trans.ID ? t('translations.confirming') : t('translations.confirm'), icon: 'check', disabled: props.confirmingId === trans.ID }]
    : []),
  ...(props.canDelete ? [{ key: 'delete', label: props.deletingId === trans.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: props.deletingId === trans.ID }] : []),
]
const onMenuSelect = (trans, key) => emit(key, trans.ID)

// Pflichtfelder: Fehler am Feld, Fokus auf das erste fehlerhafte Feld.
const errors = ref({})
const formEl = ref(null)
const fieldErrors = () => ({
  language: props.form.language_code ? '' : t('translations.errorLanguage'),
  title: (props.form.title || '').trim() ? '' : t('translations.errorTitle'),
  content: (props.form[props.contentFormKey] || '').trim() ? '' : t('translations.errorContent'),
})
const onSubmit = async () => {
  errors.value = fieldErrors()
  if (Object.values(errors.value).some(Boolean)) {
    await nextTick()
    formEl.value?.querySelector('[aria-invalid="true"]')?.focus()
    return
  }
  emit('save')
}
const onCancel = () => {
  errors.value = {}
  emit('cancel')
}
const onNew = async () => {
  errors.value = {}
  emit('new')
  await nextTick()
  formEl.value?.closest('aside')?.scrollIntoView?.({ block: 'nearest' })
  formEl.value?.querySelector('select')?.focus({ preventScroll: true })
}
// Das Formularobjekt gehört der Seite und wird hier absichtlich bearbeitet (v-model).
const formModel = computed(() => props.form)
</script>

<template>
  <div>
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="emit('dismiss')" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="title" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template #actions>
        <OnyxButton v-if="canCreate && !isEditing" :label="t('translations.newButton')" @click="onNew" />
      </template>
    </PageHeader>

    <div class="split" :class="{ 'split--open': isEditing }">
      <ListCard :title="t('translations.cardTitle')">
        <EmptyState v-if="translations.length === 0" :title="t('translations.empty')" icon="globe" />
        <ResponsiveTable v-else data-table :columns="columns" :items="translations" row-key="ID">
          <template #cell-LanguageCode="{ item }"><code>{{ item.LanguageCode }}</code></template>
          <template #cell-Title="{ item }"><strong class="component-name__title">{{ item.Title }}</strong></template>
          <template #[`cell-${contentKey}`]="{ item }"><span class="component-name__desc">{{ item[contentKey] }}</span></template>
          <template #cell-Status="{ item }">
            <StatusBadge :status="item.Status === 'confirmed' ? 'operational' : null" :label="item.Status" />
          </template>
          <template v-if="canUpdate || canDelete" #actions="{ item }">
            <RowMenu :label="t('translations.menuLabel', { name: item.Title })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
      </ListCard>

      <FormPanel v-if="isEditing" :title="t('translations.newFormTitle')" @close="onCancel">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="onSubmit">
          <FormField v-slot="f" field-id="lang-select" :label="t('translations.fieldLanguage')" required :error="errors.language || null">
            <select :id="f.id" v-model="formModel.language_code" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" @change="errors.language = ''">
              <option value="">{{ t('translations.selectLanguagePlaceholder') }}</option>
              <option v-for="l in languages" :key="l.Code" :value="l.Code">{{ l.Name }} ({{ l.Code }})</option>
            </select>
          </FormField>
          <FormField v-slot="f" field-id="title-input" :label="t('translations.fieldTitle')" required :error="errors.title || null">
            <input :id="f.id" v-model="formModel.title" type="text" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('translations.titlePlaceholder')" @input="errors.title = ''" />
          </FormField>
          <FormField v-slot="f" field-id="content-input" :label="contentFieldLabel" required :error="errors.content || null">
            <RichTextEditor v-if="richText" :id="f.id" v-model="formModel[contentFormKey]" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="contentPlaceholder" @input="errors.content = ''" />
            <textarea v-else :id="f.id" v-model="formModel[contentFormKey]" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="contentPlaceholder" @input="errors.content = ''"></textarea>
          </FormField>
          <div class="form-actions">
            <OnyxButton :label="t('common.cancel')" mode="outline" type="button" :disabled="isSaving" @click="onCancel" />
            <OnyxButton :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>
      </FormPanel>
    </div>
  </div>
</template>
