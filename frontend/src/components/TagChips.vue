<script setup>
/*
  Komponentenseite nach Richtung C: Tag-Auswahl als Chips mit ×
  und „+ Tag“ (Vorlage) statt der Checkbox-Liste. Gleiche Schnittstelle wie
  TagCheckboxList (`toggle` mit der Tag-ID), damit useTagToggle weiter passt.
  „+ Tag“ ist ein natives Select mit den noch nicht gewählten Tags: Es ist
  tastatur- und screenreaderfähig und braucht kein eigenes Flyout.
  Die Hilfe (v-help) und die Beschriftung der Gruppe setzt der Aufrufer.
*/
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  tags: { type: Array, required: true },        // verfügbare Tags (GET /api/tags)
  selectedIds: { type: Array, required: true }, // gewählte Tag-IDs
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['toggle'])

const { t } = useI18n()

// Gewählte Tags in der Reihenfolge der Auswahl; eine ID ohne Tag in der
// Liste (z. B. nicht ladbar) bleibt unsichtbar, aber im Formular erhalten.
const selected = computed(() =>
  props.selectedIds.map(id => props.tags.find(tag => tag.ID === id)).filter(Boolean))
const addable = computed(() => props.tags.filter(tag => !props.selectedIds.includes(tag.ID)))

function onAdd(event) {
  const id = Number(event.target.value)
  event.target.value = ''
  if (id) emit('toggle', id)
}
</script>

<template>
  <div class="tag-chips">
    <p v-if="tags.length === 0" class="tag-chips__empty">{{ t('components.tagCheckboxList.emptyHint') }}</p>
    <template v-else>
      <span v-for="tag in selected" :key="tag.ID" class="tag-chip">
        {{ tag.Name }}
        <button
          type="button"
          class="tag-chip__remove"
          :aria-label="t('components.tagRemove', { name: tag.Name })"
          data-help-exempt="Tag entfernen, die Hilfe hängt an der Tag-Gruppe"
          @click="emit('toggle', tag.ID)"
        ><AppIcon name="x" :size="12" /></button>
      </span>
      <select
        v-if="addable.length"
        class="tag-chips__add"
        :aria-label="t('components.tagAddAria')"
        @change="onAdd"
      >
        <option value="">{{ t('components.tagAdd') }}</option>
        <option v-for="tag in addable" :key="tag.ID" :value="tag.ID">{{ tag.Name }}</option>
      </select>
    </template>
  </div>
</template>

<style scoped>
.tag-chips {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--sp-1);
  min-block-size: var(--ctl-h);
  padding: var(--sp-1) var(--sp-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-md);
  background: var(--surface);
}
.tag-chips__empty { margin: 0; font-size: var(--fs-sm); color: var(--text-2); }
.tag-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-1);
  padding: 2px var(--sp-2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--surface-2);
  color: var(--text);
  font-size: var(--fs-xs);
}
.tag-chip__remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* Zielgröße 24 px (WCAG 2.2 AA); die Kachel selbst bleibt klein. */
  inline-size: 24px;
  block-size: 24px;
  margin-inline-end: -6px;
  padding: 0;
  border: 0;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-2);
  cursor: pointer;
}
.tag-chip__remove:hover { color: var(--text); background: var(--border); }
.tag-chip__remove:focus-visible { outline: 2px solid var(--focus); outline-offset: 1px; }
.tag-chips__add {
  appearance: none;
  -webkit-appearance: none;
  min-block-size: 28px;
  padding: 0 var(--sp-1);
  border: 0;
  background: transparent;
  color: var(--primary-text);
  font: inherit;
  font-size: var(--fs-xs);
  cursor: pointer;
}
.tag-chips__add:focus-visible { outline: 2px solid var(--focus); outline-offset: 1px; border-radius: var(--r-sm); }
</style>
