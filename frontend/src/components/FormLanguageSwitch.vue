<script setup>
import { useI18n } from 'vue-i18n'
import { OnyxButton } from 'sit-onyx'
import SegmentControl from './SegmentControl.vue'
import FormField from './FormField.vue'

// Bis zu so vielen Einträgen (Standardsprache + weitere) bleibt es ein Umschalter nebeneinander;
// darüber (die Seite hat viele Sprachen) wird es eine Auswahlliste, weil die Reiter sonst über
// die Formularbreite hinauslaufen (31 Sprachen liefen aus dem Panel).
const MAX_SEGMENTS = 3

// Sprachumschalter über Titel und Text eines Bearbeiten-Formulars.
// `trans` ist das Ergebnis von useFormTranslations; die Felder darunter binden
// an trans.titleModel / trans.contentModel.
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({ trans: { type: Object, required: true } })
const { t } = useI18n()
const { active } = props.trans // Ref, das die Ansicht besitzt
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
    <!-- Alles übersetzen: alle noch leeren weiteren Sprachen auf einmal (Entwürfe). -->
    <div v-if="trans.emptyLanguages.value.length > 0" class="lang-switch__translate">
      <OnyxButton
        type="button" mode="outline" data-test="lang-translate-all"
        :label="trans.translatingAll.value ? trans.translateAllProgress.value : t('formLanguages.translateAll', { n: trans.emptyLanguages.value.length })"
        :disabled="!trans.canTranslateAll.value"
        @click="trans.translateAll()"
      />
    </div>
    <p v-if="trans.translateAllSummary.value" class="lang-switch__hint" role="status" data-test="lang-translate-all-summary">{{ trans.translateAllSummary.value }}</p>
    <!-- Vorschlag aus der Standardsprache; das Ergebnis ist ein Entwurf, nie „fertig“. -->
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
