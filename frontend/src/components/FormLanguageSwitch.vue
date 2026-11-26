<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { OnyxButton } from 'sit-onyx'
import SegmentControl from './SegmentControl.vue'
import FormField from './FormField.vue'
import type { PropType } from 'vue'
import type { useFormTranslations } from '../composables/useFormTranslations'

// Up to this many entries (default language + others) it stays a side-by-side switcher;
// above that (the page has many languages) it becomes a select list, because the tabs would
// otherwise overflow the form width (31 languages ran out of the panel).
const MAX_SEGMENTS = 3

// Language switcher above the title and text of an edit form.
// `trans` is the result of useFormTranslations; the fields below bind
// to trans.titleModel / trans.contentModel.
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = defineProps({ trans: { type: Object as PropType<ReturnType<typeof useFormTranslations>>, required: true } })
const { t } = useI18n()
const { active } = props.trans // ref owned by the view
</script>

<template>
  <div v-if="trans.showSwitcher.value" class="lang-switch" data-test="lang-switch">
    <SegmentControl v-if="trans.langOptions.value.length <= MAX_SEGMENTS" v-model="active" :options="trans.langOptions.value" :label="t('formLanguages.langSwitchLabel')" />
    <FormField v-else v-slot="f" :label="t('formLanguages.langSwitchLabel')">
      <select :id="f.id" v-model="active" data-test="lang-select">
        <option v-for="opt in trans.langOptions.value" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
      </select>
    </FormField>
    <p class="lang-switch__hint">{{ trans.onDefaultLang.value ? t('formLanguages.langHintDefault') : t('formLanguages.langHintOther') }}</p>
    <!-- Translate all: every still-empty additional language at once (drafts). -->
    <div v-if="trans.emptyLanguages.value.length > 0" class="lang-switch__translate">
      <OnyxButton
        type="button" mode="outline" data-test="lang-translate-all"
        :label="trans.translatingAll.value ? trans.translateAllProgress.value : t('formLanguages.translateAll', { n: trans.emptyLanguages.value.length })"
        :disabled="!trans.canTranslateAll.value"
        @click="trans.translateAll()"
      />
    </div>
    <p v-if="trans.translateAllSummary.value" class="lang-switch__hint" role="status" data-test="lang-translate-all-summary">{{ trans.translateAllSummary.value }}</p>
    <!-- Suggestion from the default language; the result is a draft, never "done". -->
    <div v-if="!trans.onDefaultLang.value" class="lang-switch__translate">
      <OnyxButton
        type="button" mode="outline" data-test="lang-translate"
        :label="trans.translating.value ? t('formLanguages.translating') : t('formLanguages.translate', { from: trans.nameOf(trans.defaultCode.value) })"
        :disabled="!trans.canTranslate.value || trans.translating.value"
        @click="trans.translate()"
      />
      <p v-if="!trans.canTranslate.value" class="lang-switch__hint">{{ t(trans.titleless ? 'formLanguages.translateNeedsMessage' : 'formLanguages.translateNeedsTitle') }}</p>
      <p v-if="trans.translateFallback.value" class="lang-switch__hint" role="status" data-test="lang-translate-fallback">{{ t('formLanguages.translateFallback') }}</p>
      <p v-if="trans.translateError.value" class="lang-switch__error" role="alert" data-test="lang-translate-error">{{ trans.translateError.value }}</p>
    </div>
  </div>
</template>

<style scoped>
.lang-switch { margin-bottom: var(--sp-4); }
.lang-switch__translate { margin-top: var(--sp-2); }
.lang-switch__error { margin: var(--sp-2) 0 0; font-size: var(--fs-sm); color: var(--major-text); }
.lang-switch__hint { margin: var(--sp-2) 0 0; font-size: var(--fs-sm); color: var(--text-2); }
</style>
