<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PropType } from 'vue'
import type { useFormTranslations } from '../composables/useFormTranslations'

// "Translation is done" checkbox; only visible in additional languages.
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = defineProps({ trans: { type: Object as PropType<ReturnType<typeof useFormTranslations>>, required: true } })
const { t } = useI18n()
const { activeDraft } = props.trans // computed, points to the currently selected language
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
