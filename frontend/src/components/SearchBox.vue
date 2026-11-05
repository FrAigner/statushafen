<script setup>
/*
  Richtung C, Verwaltungsseiten: Suchfeld im Kartenkopf (Lupe,
  type="search"). Das Beschriftungs-Aria ist Pflicht, weil es kein sichtbares
  Label gibt. Weitere Attribute (z. B. v-help) landen am Eingabefeld.
*/
import AppIcon from './AppIcon.vue'

defineOptions({ inheritAttrs: false })
// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  label: { type: String, required: true },
  placeholder: { type: String, default: null },
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const model = defineModel({ type: String, default: '' })
</script>

<template>
  <div class="comp-search">
    <AppIcon name="search" :size="16" class="comp-search__icon" />
    <input
      v-bind="$attrs"
      v-model="model"
      type="search"
      class="comp-search__input"
      :aria-label="label"
      :placeholder="placeholder ?? label"
    >
  </div>
</template>
