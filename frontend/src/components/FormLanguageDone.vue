<script setup>
import { useI18n } from 'vue-i18n'

// „Übersetzung ist fertig“-Haken; nur in weiteren Sprachen sichtbar.
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({ trans: { type: Object, required: true } })
const { t } = useI18n()
const { activeDraft } = props.trans // berechnet, zeigt auf die gerade gewählte Sprache
</script>

<template>
  <label v-if="trans.showSwitcher.value && !trans.onDefaultLang.value" class="lang-done">
    <input v-model="activeDraft.done" type="checkbox" data-test="lang-done">
    <span>{{ t('formLanguages.langDone') }}</span>
  </label>
</template>

<style scoped>
.lang-done { display: flex; align-items: center; gap: var(--sp-2); margin-bottom: var(--sp-4); font-size: var(--fs-sm); }
</style>
