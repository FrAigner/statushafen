<script setup>
/*
  Richtung C: Formular als Seitenkarte rechts neben der Listenkarte
  (Kind von .split.split--open, oben bündig, klebt beim Scrollen; unter 860 px
  liegt sie unter der Liste, siehe style.css). Kopf: Titel mit Namen statt ID,
  Pflichtfeld-Hinweis, Schließen-Knopf ×. Der Inhalt (meist ein
  <form class="edit-form">) kommt per Slot; `close` meldet das Schließen
  (wie „Abbrechen“). `hint` ersetzt den Standard-Hinweis, `showRequiredHint`
  false blendet ihn für Formulare ganz ohne Pflichtfelder aus.
  Dieser Baustein ersetzt drei parallel entstandene Fassungen (Betrieb,
  Verwaltung, System).
*/
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  title: { type: String, required: true },
  hint: { type: String, default: null },
  showRequiredHint: { type: Boolean, default: true },
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['close'])

const { t } = useI18n()
</script>

<template>
  <aside class="edit-card form-panel fade-in" :aria-label="title">
    <div class="form-panel__head">
      <div>
        <h3>{{ title }}</h3>
        <p v-if="showRequiredHint || hint" class="form-panel__hint">{{ hint ?? t('patterns.requiredHint') }}</p>
      </div>
      <button type="button" class="form-panel__close" :aria-label="t('patterns.closeForm')" data-help-exempt="Formular schließen, selbsterklärend" @click="emit('close')">
        <AppIcon name="x" :size="16" />
      </button>
    </div>
    <slot />
  </aside>
</template>

<style scoped>
/* Der Titel im Kopf trägt keine Trennlinie: ".edit-card h3" (admin.css, später
   geladen) setzte sonst eine Linie und Abstände unter den Titel. Scoped schlägt
   das per Spezifität, unabhängig von der Ladereihenfolge. */
.form-panel__head h3 {
  margin: 0;
  padding: 0;
  border: 0;
}
</style>
