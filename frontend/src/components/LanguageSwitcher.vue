<script>
// Zwei Root-Elemente (select + Fehlermeldung): Attribute des
// Elternteils (z.B. class="header-control") gehören an das select.
// Eigener normaler Script-Block: defineOptions() wird aus setup() gehoben und
// verträgt die lokalen Hilfsvariablen der Stryker-Instrumentierung nicht.
// Stryker disable next-line ObjectLiteral,BooleanLiteral: äquivalenter Mutant -
// bei mehreren Root-Knoten gibt es kein automatisches Attrs-Fallthrough, egal
// welchen Wert inheritAttrs hat; $attrs wird hier ausschließlich manuell per
// v-bind="$attrs" auf das select gereicht (siehe Template), beobachtbares
// Verhalten ändert sich daher für kein Test (lokal mit Vue 3.5 verifiziert).
export default { inheritAttrs: false }
</script>
<script setup>
import { useI18n } from 'vue-i18n'
import { useLanguage } from '../composables/useLanguage'

// Minimaler Sprachumschalter (i18n-Infrastruktur) - Dropdown im
// Header, nutzbar sowohl von eingeloggten Usern (Persistenz via /api/me) als
// auch anonymen Besuchern (Persistenz via localStorage, siehe useLanguage).
const { t } = useI18n()
const { languageOptions, selectedLocale, setLanguage, saveError } = useLanguage()

const onChange = (event) => {
  setLanguage(event.target.value)
}
</script>

<template>
  <select
    v-bind="$attrs"
    class="language-switcher"
    :value="selectedLocale"
    :aria-label="t('language.label')"
    :title="t('language.label')"
    @change="onChange"
  >
    <option v-for="opt in languageOptions" :key="opt.code" :value="opt.code">
      {{ opt.label }}
    </option>
  </select>
  <span v-if="saveError" class="language-switcher-error" role="alert">{{ saveError }}</span>
</template>

<style scoped>
.language-switcher {
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
  font-size: 0.85rem;
  cursor: pointer;
}

.language-switcher option {
  background: var(--surface);
  color: var(--text);
}
.language-switcher-error {
  margin-left: 8px;
  font-size: 0.85rem;
  color: var(--major-text);
}
</style>
