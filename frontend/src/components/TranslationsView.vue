<script setup lang="ts">
/*
  Direction C: shared UI of the admin translation pages for components and
  notices (both were identical apart from the content field and texts). Page
  header with breadcrumbs, list card with row menu (confirm, delete) and the
  form as a side card. The logic (loading, saving, confirming, deleting) stays
  in useAdminTranslationsPanel; this component only renders and reports back
  via events.

  - contentKey: field of the translation in the list ('Description' / 'Message').
  - contentFormKey: field in the form ('description' / 'message').
  - form is the panel's form object (language_code, title, content).
*/
import { computed, nextTick, ref } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
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
import type { EntityTranslation, Language } from '../types/api'

type TranslationId = number | string
type FieldErrors = Partial<Record<'language' | 'title' | 'content', string>>

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  title: string
  subtitle?: string | null
  breadcrumbs?: { label: string, to?: RouteLocationRaw }[]
  alertMsg?: string
  alertType?: string
  canCreate?: boolean
  canUpdate?: boolean
  canDelete?: boolean
  isEditing?: boolean
  isSaving?: boolean
  translations?: EntityTranslation[]
  languages?: Language[]
  confirmingId?: TranslationId | null
  deletingId?: TranslationId | null
  contentKey: string
  contentFormKey: string
  contentColumnLabel: string
  contentFieldLabel: string
  contentPlaceholder?: string
  // The content is formatted text (Markdown) and gets the editor.
  richText?: boolean
  // The panel's form object (language_code, title, content); the fields write straight into it.
  form?: Record<string, string>
}>(), {
  subtitle: null,
  breadcrumbs: () => [],
  alertMsg: '',
  alertType: 'info',
  canCreate: false,
  canUpdate: false,
  canDelete: false,
  isEditing: false,
  isSaving: false,
  translations: () => [],
  languages: () => [],
  confirmingId: null,
  deletingId: null,
  contentPlaceholder: '',
  richText: false,
  form: () => ({}),
})
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{
  (e: 'new' | 'cancel' | 'save' | 'dismiss'): void
  (e: 'confirm' | 'delete', id: TranslationId): void
}>()

const { t } = useI18n()

const columns = computed(() => [
  { key: 'LanguageCode', label: t('translations.tableLanguage') },
  { key: 'Title', label: t('translations.tableTitle') },
  { key: props.contentKey, label: props.contentColumnLabel },
  { key: 'Status', label: t('translations.tableStatus') },
])

const menuItems = (trans: EntityTranslation) => [
  ...(trans.Status === 'draft' && props.canUpdate
    ? [{ key: 'confirm', label: props.confirmingId === trans.ID ? t('translations.confirming') : t('translations.confirm'), icon: 'check', disabled: props.confirmingId === trans.ID }]
    : []),
  ...(props.canDelete ? [{ key: 'delete', label: props.deletingId === trans.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: props.deletingId === trans.ID }] : []),
]
// The menu keys are exactly the two events (see menuItems).
const onMenuSelect = (trans: EntityTranslation, key: string) => emit(key as 'confirm' | 'delete', trans.ID)

// Required fields: error at the field, focus on the first invalid field.
const errors = ref<FieldErrors>({})
const formEl = ref<HTMLFormElement | null>(null)
const fieldErrors = (): FieldErrors => ({
  language: props.form.language_code ? '' : t('translations.errorLanguage'),
  title: (props.form.title || '').trim() ? '' : t('translations.errorTitle'),
  content: (props.form[props.contentFormKey] || '').trim() ? '' : t('translations.errorContent'),
})
const onSubmit = async () => {
  errors.value = fieldErrors()
  if (Object.values(errors.value).some(Boolean)) {
    await nextTick()
    formEl.value?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
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
// The form object belongs to the page and is edited here on purpose (v-model).
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
