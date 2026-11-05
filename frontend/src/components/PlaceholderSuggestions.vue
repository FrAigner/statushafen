<script setup>
import { ref, watch, nextTick } from 'vue'

// Vorschlagsliste der Platzhalter-Autovervollständigung
// (ARIA-Listbox). Den Zustand (offen, aktiver Eintrag, Filter) hält
// usePlaceholderInsert; die Komponente zeigt nur an und meldet Klicks.
// mousedown.prevent hält den Fokus im Textfeld, sonst würde dessen Blur die
// Liste schließen, bevor der Klick ankommt.
// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  id: { type: String, required: true },
  label: { type: String, required: true },
  items: { type: Array, required: true },
  open: { type: Boolean, required: true },
  activeIndex: { type: Number, required: true },
  optionId: { type: Function, required: true },
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['select'])

const listbox = ref(null)

// Bei Pfeiltasten in einer langen Liste den aktiven Eintrag sichtbar halten.
watch(() => [props.open, props.activeIndex], async () => {
  if (!props.open) return
  await nextTick()
  listbox.value.children[props.activeIndex].scrollIntoView({ block: 'nearest' })
})
</script>

<template>
  <ul v-show="open" :id="id" ref="listbox" role="listbox" :aria-label="label" class="placeholder-suggestions">
    <li
      v-for="(item, index) in items"
      :id="optionId(index)"
      :key="item.key"
      role="option"
      :aria-selected="String(index === activeIndex)"
      class="placeholder-suggestions__option"
      :class="{ 'is-active': index === activeIndex }"
      @mousedown.prevent
      @click="emit('select', item)"
    >
      <code>{{ item.key }}</code>
      <span class="placeholder-suggestions__desc">{{ item.desc }}</span>
    </li>
  </ul>
</template>

<style scoped>
.placeholder-suggestions {
  position: absolute;
  top: 100%;
  left: 0;
  z-index: 20;
  min-width: min(100%, 320px);
  max-width: 100%;
  max-height: 240px;
  overflow-y: auto;
  margin: 2px 0 0;
  padding: 4px;
  list-style: none;
  background: var(--surface);
  color: var(--text);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  box-shadow: var(--shadow-2);
}

.placeholder-suggestions__option {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px 8px;
  border-radius: var(--radius-sm);
  cursor: pointer;
}

.placeholder-suggestions__option code {
  font-family: var(--font-mono);
}

.placeholder-suggestions__option:hover,
.placeholder-suggestions__option.is-active {
  background: var(--primary-soft);
}

.placeholder-suggestions__desc {
  font-size: 0.85em;
  color: var(--text-2);
}
</style>
