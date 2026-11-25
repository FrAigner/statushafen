<script setup>
/*
  Quick search in the admin top bar: finds admin pages, individual settings
  (registry in utils/adminSearch.js) and built-in docs sections. ARIA pattern
  "combobox with listbox popup": focus stays in the input, the active option
  is announced via aria-activedescendant. Ctrl/Cmd+K and "/" focus the field.
  On small screens a search button opens the field.
*/
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount, useId } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useTenant } from '../composables/useTenant'
import { buildSearchIndex, searchIndex } from '../utils/adminSearch'
import { revealSetting } from '../utils/revealSetting'
import { nextActiveIndex } from '../utils/listbox'
import AppIcon from './AppIcon.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  // Visibility rule of the nav (AdminLayout.canSeeNavItem).
  canSee: { type: Function, required: true },
})
// Stryker restore all

const { t, locale } = useI18n()
const router = useRouter()
const { activeTenantId } = useTenant()

const uid = useId()
const fieldId = `qs-field-${uid}`
const listboxId = `qs-listbox-${uid}`
const hintId = `qs-hint-${uid}`
const groupId = (kind) => `qs-group-${uid}-${kind}`
const optionId = (index) => `qs-option-${uid}-${index}`

const rootRef = ref(null)
const inputRef = ref(null)
const toggleRef = ref(null)
const query = ref('')
const open = ref(false)
const mobileOpen = ref(false)
const activeIndex = ref(0)

// Docs entries are loaded on first use so marked and the docs stay out of the start bundle.
const docsModule = ref(null)
const loadDocs = () => {
  if (docsModule.value) return
  import('../utils/docsSearchIndex')
    .then(mod => { docsModule.value = mod })
    .catch(err => console.error('Failed to load docs search index', err))
}
const docs = computed(() => (docsModule.value ? docsModule.value.buildDocsEntries(locale.value) : []))

const entries = computed(() => buildSearchIndex({
  t,
  canSee: props.canSee,
  activePageId: activeTenantId.value || null,
  docs: docs.value,
}))
// Read locale so the index follows a language switch.
const groups = computed(() => (locale.value, searchIndex(entries.value, query.value)))
const flat = computed(() => groups.value.flatMap(group => group.entries))
const listOpen = computed(() => open.value && flat.value.length > 0)
const activeId = computed(() => (listOpen.value ? optionId(activeIndex.value) : undefined))
const indexOf = (entry) => flat.value.indexOf(entry)

const groupLabel = (kind) => t(`quickSearch.groups.${{ page: 'pages', setting: 'settings', doc: 'docs' }[kind]}`)

const statusText = computed(() => {
  if (!query.value.trim()) return ''
  if (flat.value.length === 0) return t('quickSearch.noResults', { query: query.value.trim() })
  return t('quickSearch.resultCount', { n: flat.value.length }, flat.value.length)
})
const showNoResults = computed(() => open.value && query.value.trim() !== '' && flat.value.length === 0)

watch(query, () => { activeIndex.value = 0 })

const isMac = () => /Mac|iPhone|iPad/.test(navigator.platform)
const shortcutLabel = computed(() => (isMac() ? '⌘ K' : t('quickSearch.shortcut')))

const focusInput = () => {
  mobileOpen.value = true
  inputRef.value?.focus()
  nextTick(() => inputRef.value?.focus())
}

const onFocus = () => {
  open.value = true
  loadDocs()
}

const onInput = () => { open.value = true }

const reset = () => {
  open.value = false
  query.value = ''
  mobileOpen.value = false
}

const select = async (entry) => {
  reset()
  inputRef.value?.blur()
  await router.push(entry.to)
  if (entry.targetId) revealSetting(entry.targetId, { fallbackId: entry.fallbackId })
}

const scrollActiveIntoView = () => nextTick(() => {
  document.getElementById(optionId(activeIndex.value))?.scrollIntoView?.({ block: 'nearest' })
})

const onKeydown = (e) => {
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    if (!listOpen.value) {
      open.value = true
      return
    }
    // The list is open, so it has options and the arrow key always moves.
    activeIndex.value = nextActiveIndex(activeIndex.value, e.key, flat.value.length)
    scrollActiveIntoView()
  } else if (e.key === 'Enter') {
    const entry = listOpen.value ? flat.value[activeIndex.value] : null
    if (entry) {
      e.preventDefault()
      select(entry)
    }
  } else if (e.key === 'Escape') {
    if (listOpen.value || showNoResults.value) {
      open.value = false
    } else if (query.value) {
      query.value = ''
    } else if (mobileOpen.value) {
      mobileOpen.value = false
      toggleRef.value?.focus()
    }
  }
}

const onFocusout = (e) => {
  if (e.relatedTarget && rootRef.value?.contains(e.relatedTarget)) return
  open.value = false
  mobileOpen.value = false
}

const toggleMobile = () => {
  if (mobileOpen.value) {
    mobileOpen.value = false
    return
  }
  focusInput()
}

const isEditable = (el) => !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))

const onGlobalKeydown = (e) => {
  const isK = e.key === 'k' || e.key === 'K'
  if (isK && (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey) {
    e.preventDefault()
    focusInput()
  } else if (e.key === '/' && !e.ctrlKey && !e.metaKey && !e.altKey && !isEditable(e.target) && !isEditable(document.activeElement)) {
    e.preventDefault()
    focusInput()
  }
}

onMounted(() => window.addEventListener('keydown', onGlobalKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onGlobalKeydown))
</script>

<template>
  <div ref="rootRef" class="quick-search" :class="{ 'is-open': mobileOpen }" @focusout="onFocusout">
    <button
      ref="toggleRef"
      type="button"
      class="icon-btn quick-search__toggle"
      :aria-label="mobileOpen ? t('quickSearch.close') : t('quickSearch.open')"
      :aria-expanded="mobileOpen"
      :aria-controls="fieldId"
      @click="toggleMobile"
    >
      <AppIcon :name="mobileOpen ? 'x' : 'search'" :size="16" />
    </button>

    <div :id="fieldId" class="quick-search__field">
      <AppIcon name="search" :size="14" class="quick-search__icon" />
      <input
        ref="inputRef"
        v-model="query"
        type="text"
        role="combobox"
        class="quick-search__input"
        autocomplete="off"
        spellcheck="false"
        aria-autocomplete="list"
        :aria-label="t('quickSearch.label')"
        :aria-expanded="listOpen"
        :aria-controls="listOpen ? listboxId : undefined"
        :aria-activedescendant="activeId"
        :aria-describedby="hintId"
        :placeholder="t('quickSearch.placeholder')"
        @focus="onFocus"
        @input="onInput"
        @keydown="onKeydown"
      >
      <kbd class="quick-search__kbd" aria-hidden="true">{{ shortcutLabel }}</kbd>
    </div>
    <span :id="hintId" class="sr-only">{{ t('quickSearch.hint') }}</span>

    <div
      v-if="listOpen"
      :id="listboxId"
      role="listbox"
      class="quick-search__results"
      :aria-label="t('quickSearch.resultsLabel')"
    >
      <div v-for="group in groups" :key="group.kind" role="group" class="quick-search__group" :aria-labelledby="groupId(group.kind)">
        <div :id="groupId(group.kind)" role="presentation" class="quick-search__group-label">{{ groupLabel(group.kind) }}</div>
        <div
          v-for="entry in group.entries"
          :id="optionId(indexOf(entry))"
          :key="entry.key"
          role="option"
          class="quick-search__option"
          :class="{ 'is-active': indexOf(entry) === activeIndex }"
          :aria-selected="indexOf(entry) === activeIndex"
          :data-kind="entry.kind"
          @mousedown.prevent
          @mousemove="activeIndex = indexOf(entry)"
          @click="select(entry)"
        >
          <AppIcon :name="entry.kind === 'page' ? entry.icon : entry.kind === 'doc' ? 'book' : 'settings'" :size="14" class="quick-search__option-icon" />
          <span class="quick-search__option-text">
            <span class="quick-search__option-label">{{ entry.label }}</span>
            <span class="quick-search__option-context">{{ entry.context }}</span>
          </span>
        </div>
      </div>
    </div>

    <p aria-live="polite" aria-atomic="true" class="quick-search__status" :class="{ 'sr-only': !showNoResults }">{{ statusText }}</p>
  </div>
</template>

<style scoped>
.quick-search {
  position: relative;
  flex: 0 1 340px;
  min-width: 0;
}
.quick-search__toggle { display: none; }

.quick-search__field {
  position: relative;
  display: flex;
  align-items: center;
}
.quick-search__icon {
  position: absolute;
  left: 10px;
  color: var(--nav-text);
  pointer-events: none;
}
.quick-search__input {
  width: 100%;
  height: var(--ctl-h);
  padding: 0 52px 0 30px;
  background: color-mix(in srgb, var(--top-text) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--top-text) 24%, transparent);
  border-radius: var(--r-md);
  color: var(--top-text);
  font-family: var(--font-sans);
  font-size: 0.82rem;
}
.quick-search__input::placeholder { color: var(--nav-text); opacity: 1; }
.quick-search__input:hover { background: color-mix(in srgb, var(--top-text) 16%, transparent); }
.quick-search__kbd {
  position: absolute;
  right: 8px;
  padding: 1px 6px;
  border: 1px solid color-mix(in srgb, var(--top-text) 24%, transparent);
  border-radius: var(--r-sm, 4px);
  color: var(--nav-text);
  font-family: var(--font-sans);
  font-size: 0.7rem;
  pointer-events: none;
}

.quick-search__results,
.quick-search__status:not(.sr-only) {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  z-index: 400;
  width: max(100%, min(480px, calc(100vw - 24px)));
  background: var(--surface);
  color: var(--text);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-pop);
}
.quick-search__results {
  max-height: min(70vh, 520px);
  overflow-y: auto;
  padding: 6px;
}
.quick-search__status:not(.sr-only) {
  margin: 0;
  padding: 12px;
  font-size: 0.85rem;
  color: var(--text-2);
}
.quick-search__group + .quick-search__group { margin-top: 4px; border-top: 1px solid var(--border); padding-top: 4px; }
.quick-search__group-label {
  padding: 6px 8px 4px;
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: .04em;
  text-transform: uppercase;
  color: var(--text-3);
}
.quick-search__option {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 6px 8px;
  border-radius: var(--r-md);
  cursor: pointer;
}
.quick-search__option.is-active { background: var(--primary-soft); color: var(--primary-text); }
.quick-search__option-icon { flex-shrink: 0; margin-top: 3px; }
.quick-search__option-text { display: flex; flex-direction: column; min-width: 0; }
.quick-search__option-label { font-size: 0.85rem; font-weight: 500; overflow-wrap: anywhere; }
.quick-search__option-context { font-size: 0.75rem; color: var(--text-3); }
.quick-search__option.is-active .quick-search__option-context { color: inherit; }

/* Small screens: only a search button; it opens the field below the top row. */
@media (max-width: 768px) {
  .quick-search { position: static; flex: 0 0 auto; margin-left: auto; }
  .quick-search__toggle { display: flex; }
  .quick-search__field { display: none; }
  .quick-search__kbd { display: none; }
  .quick-search.is-open .quick-search__field {
    display: flex;
    position: absolute;
    top: var(--admin-mobile-head-h);
    left: var(--space-md);
    right: var(--space-md);
    z-index: 401;
  }
  .quick-search.is-open .quick-search__input { background: var(--top-bg); }
  .quick-search__results,
  .quick-search__status:not(.sr-only) {
    top: calc(var(--admin-mobile-head-h) + var(--ctl-h) + 6px);
    left: var(--space-md);
    right: var(--space-md);
    width: auto;
  }
}
</style>
