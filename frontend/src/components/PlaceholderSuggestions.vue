<script setup lang="ts" generic="T extends { key: string, desc: string }">
import { ref, watch, nextTick } from 'vue'

// Suggestion list of the placeholder autocompletion (ARIA listbox). The state
// (open, active entry, filter) is held by usePlaceholderInsert; the component
// only renders and reports clicks. mousedown.prevent keeps the focus in the
// text field, otherwise its blur would close the list before the click arrives.
// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = defineProps<{
  id: string
  label: string
  items: T[]
  open: boolean
  activeIndex: number
  optionId: (index: number) => string
}>()
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ select: [item: T] }>()

const listbox = ref<HTMLUListElement | null>(null)

// Keep the active entry visible when using the arrow keys in a long list.
watch(() => [props.open, props.activeIndex], async () => {
  if (!props.open) return
  await nextTick()
  listbox.value!.children[props.activeIndex]!.scrollIntoView({ block: 'nearest' })
})
</script>

<template>
  <ul v-show="open" :id="id" ref="listbox" role="listbox" :aria-label="label" class="placeholder-suggestions">
    <li
      v-for="(item, index) in items"
      :id="optionId(index)"
      :key="item.key"
      role="option"
      :aria-selected="index === activeIndex ? 'true' : 'false'"
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
