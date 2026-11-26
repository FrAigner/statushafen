<script lang="ts">
// Attributes of the parent (e.g. class="header-control") belong on the trigger
// button, not on the wrapper, the list or the error message.
// Plain script block: defineOptions() is hoisted out of setup() and does not
// cope with the local helper variables of the Stryker instrumentation.
export default { inheritAttrs: false }
</script>
<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useLanguage } from '../composables/useLanguage'
import { useDismissable } from '../composables/useDismissable'
import { isOpenKey, isSelectKey, nextActiveIndex } from '../utils/listbox'

// Language switcher in the public header and the admin top bar. Usable by
// signed-in users (persisted via /api/me) and anonymous visitors (localStorage,
// see useLanguage).
//
// A custom select-only combobox instead of a native <select>: the OS popup of
// a native select opened detached from the header (macOS: above the field) and
// did not look like the other header panels. Focus stays on the button, the
// active option is announced via aria-activedescendant (same pattern as
// QuickStatus, keys from utils/listbox).
const { t } = useI18n()
const { languageOptions, selectedLocale, setLanguage, saveError } = useLanguage()

const uid = useId()
const listId = `language-list-${uid}`
const optionId = (index: number) => `language-option-${uid}-${index}`

const root = ref<HTMLElement | null>(null)
const button = ref<HTMLButtonElement | null>(null)
const open = ref(false)
const active = ref(0)

const selectedIndex = computed(() => languageOptions.value.findIndex(o => o.code === selectedLocale.value))
const currentLabel = computed(() => languageOptions.value[selectedIndex.value]?.label ?? selectedLocale.value)

function openList() {
  active.value = Math.max(selectedIndex.value, 0)
  open.value = true
}

function close() {
  open.value = false
}

// A click/tap or focus outside closes the list (and, because the other header
// popups use the same composable, opening this one closes them). Escape is
// handled in onKeydown, focus stays on the button anyway.
useDismissable(open, [root], close, { escape: false })

function choose(index: number) {
  close()
  button.value!.focus()
  setLanguage(languageOptions.value[index]!.code)
}

function onToggle() {
  if (open.value) close()
  else openList()
}

function onKeydown(event: KeyboardEvent) {
  if (!open.value) {
    if (!isOpenKey(event.key)) return
    event.preventDefault()
    openList()
    return
  }
  if (event.key === 'Escape') {
    event.preventDefault()
    close()
    return
  }
  if (event.key === 'Tab') {
    close()
    return
  }
  if (isSelectKey(event.key)) {
    event.preventDefault()
    choose(active.value)
    return
  }
  const next = nextActiveIndex(active.value, event.key, languageOptions.value.length)
  if (next === null) return
  event.preventDefault()
  active.value = next
}

watch(open, async (isOpen) => {
  if (!isOpen) return
  await nextTick()
  document.getElementById(optionId(active.value))?.scrollIntoView?.({ block: 'nearest' })
})
</script>

<template>
  <div ref="root" class="language-switcher-root">
    <button
      ref="button"
      v-bind="$attrs"
      type="button"
      role="combobox"
      class="language-switcher"
      aria-haspopup="listbox"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-controls="listId"
      :aria-activedescendant="open ? optionId(active) : undefined"
      :aria-label="t('language.label')"
      :title="t('language.label')"
      @click="onToggle"
      @keydown="onKeydown"
    >
      {{ currentLabel }}
    </button>

    <ul
      v-show="open"
      :id="listId"
      role="listbox"
      tabindex="-1"
      class="language-switcher__list"
      :aria-label="t('language.label')"
    >
      <li
        v-for="(opt, index) in languageOptions"
        :id="optionId(index)"
        :key="opt.code"
        :data-code="opt.code"
        role="option"
        class="language-switcher__option"
        :class="{ 'language-switcher__option--active': index === active }"
        :aria-selected="index === selectedIndex ? 'true' : 'false'"
        :lang="opt.code"
        @pointerdown.prevent
        @click="choose(index)"
        @pointermove="active = index"
      >
        <span class="language-switcher__check-slot" aria-hidden="true">
          <svg
            v-if="index === selectedIndex"
            class="language-switcher__check"
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          ><path d="M20 6 9 17l-5-5" /></svg>
        </span>
        <span>{{ opt.label }}</span>
      </li>
    </ul>
    <span v-if="saveError" class="language-switcher-error" role="alert">{{ saveError }}</span>
  </div>
</template>

<style scoped>
.language-switcher-root {
  position: relative;
  display: inline-flex;
  align-items: center;
}

/* Base look outside the headers; both headers style the trigger like their
   neighbouring controls (PublicLayout.vue: .header-control.language-switcher,
   style.css: .admin-topbar .language-switcher). :where() keeps this rule at
   the lowest specificity so those header rules always win. */
:where(.language-switcher) {
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
  font-size: 0.85rem;
  white-space: nowrap;
  cursor: pointer;
}

/* Panel in the style of the user menu: opens directly below the trigger,
   aligned to its end edge (the switcher sits at the right of both headers).
   Colours only via tokens, so light and dark theme need no own rules. */
.language-switcher__list {
  position: absolute;
  inset-block-start: calc(100% + 4px);
  inset-inline-end: 0;
  z-index: 400;
  min-inline-size: 180px;
  max-inline-size: calc(100vw - 24px);
  max-block-size: min(60vh, 360px);
  overflow-y: auto;
  margin: 0;
  padding: 6px;
  list-style: none;
  font-family: var(--font-sans);
  background: var(--surface);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-2);
}

.language-switcher__option {
  display: flex;
  align-items: center;
  gap: 8px;
  min-block-size: 36px;
  padding: 0 10px;
  border-radius: var(--radius-sm);
  color: var(--text);
  font-size: 0.85rem;
  white-space: nowrap;
  cursor: pointer;
}

.language-switcher__option--active {
  background: var(--surface-2);
}

.language-switcher__option[aria-selected='true'] {
  font-weight: 600;
}

/* Fixed slot, so the labels stay aligned whether or not they carry the check. */
.language-switcher__check-slot {
  display: inline-flex;
  inline-size: 14px;
  color: var(--primary);
}

.language-switcher-error {
  margin-left: 8px;
  font-size: 0.85rem;
  color: var(--major-text);
}
</style>
