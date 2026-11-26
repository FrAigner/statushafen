<script setup lang="ts">
/*
  Row menu ("…") for table rows, replaces the row of
  translations/edit/delete. Follows the ARIA "Menu Button" pattern.

  Entry: { key, label, icon?, to?, danger?, disabled? }. With `to` the entry
  is a link (router-link), otherwise a button; `select` reports its key.

  Keyboard
  - Button: Enter/Space/Arrow down opens and focuses the first entry,
    Arrow up the last one.
  - Menu: arrows move (wrapping around), Home/End, Enter/Space select,
    Escape closes and returns focus to the button, Tab closes.
  - Clicking outside, scrolling and resizing close it.

  The menu is `position: fixed` (computed from the button), because the
  table uses overflow and the menu of the last row would be clipped
  otherwise. Target size 44 px (button and entries).
*/
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import AppIcon from './AppIcon.vue'
import { useDismissable } from '../composables/useDismissable'

interface MenuItem {
  key: string
  label: string
  icon?: string
  to?: RouteLocationRaw
  danger?: boolean
  disabled?: boolean
}

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  label: string
  items: MenuItem[]
  disabled?: boolean
}>(), {
  disabled: false,
})
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ select: [key: string] }>()

const uid = useId()
const menuId = `row-menu-${uid}`
const root = ref<HTMLElement | null>(null)
const button = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const open = ref(false)
const position = ref<Record<string, string>>({})

const entries = () => [...menu.value!.querySelectorAll<HTMLElement>('[role="menuitem"]:not([aria-disabled="true"])')]

function place() {
  const rect = button.value!.getBoundingClientRect()
  const height = menu.value!.offsetHeight
  // Downwards, unless it does not fit there and there is room above.
  const flip = height > 0 && rect.bottom + height + 8 > window.innerHeight && rect.top > height + 8
  position.value = {
    insetInlineEnd: `${Math.max(window.innerWidth - rect.right, 8)}px`,
    insetBlockStart: flip ? `${rect.top - height - 4}px` : `${rect.bottom + 4}px`,
  }
}

// A disabled button is neither clickable nor focusable; a separate guard
// here would be dead code.
async function openMenu(which: 'first' | 'last' = 'first') {
  open.value = true
  await nextTick()
  place()
  const list = entries()
  list[which === 'last' ? list.length - 1 : 0]?.focus()
}

function closeMenu(restoreFocus = false) {
  if (!open.value) return
  open.value = false
  if (restoreFocus) button.value?.focus()
}

function onButtonKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    openMenu(event.key === 'ArrowUp' ? 'last' : 'first')
  }
}

function onToggle() {
  if (open.value) closeMenu(true)
  else openMenu()
}

function onMenuKeydown(event: KeyboardEvent) {
  const list = entries()
  const index = list.indexOf(document.activeElement as HTMLElement)
  const last = list.length - 1
  const target = ({
    ArrowDown: index >= last ? 0 : index + 1,
    ArrowUp: index <= 0 ? last : index - 1,
    Home: 0,
    End: last,
  } as Record<string, number | undefined>)[event.key]
  if (target !== undefined) {
    event.preventDefault()
    list[target]?.focus()
    return
  }
  if (event.key === 'Escape') {
    event.preventDefault()
    closeMenu(true)
    return
  }
  if (event.key === 'Tab') {
    closeMenu()
    return
  }
  // Space does nothing on links; treat it like Enter.
  if (event.key === ' ') {
    event.preventDefault()
    const active = document.activeElement as HTMLElement | null
    active?.click()
  }
}

function onSelect(item: MenuItem) {
  closeMenu(true)
  emit('select', item.key)
}

// A link navigates by itself; the menu only closes, focus is left to the navigation.
const onLink = () => closeMenu()

// Click/tap outside closes; Escape is handled in onMenuKeydown (focus returns to the button).
useDismissable(open, [root], () => closeMenu(), { escape: false })
const onViewportChange = () => closeMenu()

const listeners: [event: 'scroll' | 'resize', handler: () => void, capture: boolean][] = [
  ['scroll', onViewportChange, true],
  ['resize', onViewportChange, false],
]
const eventTarget = (event: 'scroll' | 'resize'): EventTarget => (event === 'resize' ? window : document)

watch(open, (isOpen) => {
  for (const [event, handler, capture] of listeners) {
    if (isOpen) eventTarget(event).addEventListener(event, handler, capture)
    else eventTarget(event).removeEventListener(event, handler, capture)
  }
})

onBeforeUnmount(() => {
  for (const [event, handler, capture] of listeners) eventTarget(event).removeEventListener(event, handler, capture)
})
</script>

<template>
  <div ref="root" class="row-menu">
    <button
      ref="button"
      type="button"
      class="row-menu__button"
      aria-haspopup="menu"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-controls="menuId"
      :aria-label="label"
      :disabled="disabled"
      @click="onToggle"
      @keydown="onButtonKeydown"
    >
      <AppIcon name="more" :size="18" />
    </button>

    <div
      v-show="open"
      :id="menuId"
      ref="menu"
      role="menu"
      class="row-menu__list"
      :aria-label="label"
      :style="position"
      @keydown="onMenuKeydown"
    >
      <template v-for="item in items" :key="item.key">
        <router-link
          v-if="item.to"
          :to="item.to"
          role="menuitem"
          tabindex="-1"
          class="row-menu__item"
          @click="onLink"
        >
          <AppIcon v-if="item.icon" :name="item.icon" :size="16" />{{ item.label }}
        </router-link>
        <button
          v-else
          type="button"
          role="menuitem"
          tabindex="-1"
          class="row-menu__item"
          :class="{ 'row-menu__item--danger': item.danger }"
          :aria-disabled="item.disabled ? 'true' : undefined"
          data-help-exempt="Menüeintrag, die Hilfe hängt am Menüknopf"
          @click="!item.disabled && onSelect(item)"
        >
          <AppIcon v-if="item.icon" :name="item.icon" :size="16" />{{ item.label }}
        </button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.row-menu { position: relative; display: inline-block; }
.row-menu__button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: 44px;
  block-size: 44px;
  padding: 0;
  border: 1px solid transparent;
  border-radius: var(--r-md);
  background: transparent;
  color: var(--text-2);
  cursor: pointer;
}
.row-menu__button:hover:not(:disabled),
.row-menu__button[aria-expanded='true'] { background: var(--surface-2); color: var(--text); }
.row-menu__button:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
.row-menu__button:disabled { opacity: .55; cursor: not-allowed; }

.row-menu__list {
  position: fixed;
  z-index: 60;
  min-inline-size: 200px;
  padding: var(--sp-1);
  border-radius: var(--r-md);
  background: var(--surface);
  box-shadow: var(--shadow-pop);
  text-align: start;
}
.row-menu__item {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  inline-size: 100%;
  min-block-size: 44px;
  padding: 0 var(--sp-3);
  border: 0;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text);
  font: inherit;
  font-size: var(--fs-base);
  text-align: start;
  text-decoration: none;
  white-space: nowrap;
  cursor: pointer;
}
.row-menu__item:hover,
.row-menu__item:focus-visible { background: var(--surface-2); }
.row-menu__item:focus-visible { outline: 2px solid var(--focus); outline-offset: -2px; }
.row-menu__item[aria-disabled='true'] { opacity: .55; cursor: not-allowed; }
.row-menu__item--danger { color: var(--major-text); }
</style>
