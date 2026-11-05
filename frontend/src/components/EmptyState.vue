<script setup>
/*
  Redesign Stufe 3: Leerzustand mit Symbol, Titel, einem Satz und
  einer Aktion (Slot "action"). Ersetzt .table-empty, .pad-empty und den
  Leerfall von ListStateRow. In Tabellen innerhalb einer <td colspan> setzen.
  Der Titel ist bewusst ein <p>, keine Überschrift: ein Leerzustand darf die
  Überschriftenhierarchie der Seite nicht verändern.
*/
import AppIcon from './AppIcon.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  title: { type: String, required: true },
  text: { type: String, default: null },
  icon: { type: String, default: null },
})
// Stryker restore all
</script>

<template>
  <div class="empty-state">
    <AppIcon v-if="icon" :name="icon" :size="28" class="empty-state__icon" />
    <p class="empty-state__title">{{ title }}</p>
    <p v-if="text" class="empty-state__text">{{ text }}</p>
    <div v-if="$slots.action" class="empty-state__action"><slot name="action" /></div>
  </div>
</template>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--sp-2);
  padding: var(--sp-6) var(--sp-4);
  text-align: center;
}
.empty-state__icon { color: var(--text-3); }
.empty-state__title { margin: 0; font-size: var(--fs-md); font-weight: 600; color: var(--text); }
.empty-state__text { margin: 0; max-inline-size: 42ch; font-size: var(--fs-base); color: var(--text-2); }
.empty-state__action { margin-top: var(--sp-2); }
</style>
