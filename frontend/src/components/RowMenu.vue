<script setup>
/*
  Zeilenmenü („…“) für Tabellenzeilen, ersetzt die Reihe
  aus Übersetzungen/Bearbeiten/Löschen. Nach dem ARIA-Muster „Menu Button“.

  Eintrag: { key, label, icon?, to?, danger?, disabled? }. Mit `to` ist der
  Eintrag ein Link (router-link), sonst ein Button; `select` meldet dessen key.

  Tastatur
  - Knopf: Enter/Leertaste/Pfeil unten öffnet und fokussiert den ersten
    Eintrag, Pfeil oben den letzten.
  - Menü: Pfeile bewegen (mit Umbruch), Home/End, Enter/Leertaste wählen,
    Escape schließt und setzt den Fokus auf den Knopf zurück, Tab schließt.
  - Klick außerhalb, Scrollen und Größenänderung schließen.

  Das Menü ist `position: fixed` (aus dem Knopf berechnet), weil die Tabelle
  mit overflow arbeitet und das Menü der letzten Zeile sonst abgeschnitten
  würde. Zielgröße 44 px (Knopf und Einträge).
*/
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import AppIcon from './AppIcon.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  label: { type: String, required: true },
  items: { type: Array, required: true },
  disabled: { type: Boolean, default: false },
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['select'])

const uid = useId()
const menuId = `row-menu-${uid}`
const root = ref(null)
const button = ref(null)
const menu = ref(null)
const open = ref(false)
const position = ref({})

const entries = () => [...menu.value.querySelectorAll('[role="menuitem"]:not([aria-disabled="true"])')]

function place() {
  const rect = button.value.getBoundingClientRect()
  const height = menu.value.offsetHeight
  // Nach unten, außer es passt dort nicht hin und oben ist Platz.
  const flip = height > 0 && rect.bottom + height + 8 > window.innerHeight && rect.top > height + 8
  position.value = {
    insetInlineEnd: `${Math.max(window.innerWidth - rect.right, 8)}px`,
    insetBlockStart: flip ? `${rect.top - height - 4}px` : `${rect.bottom + 4}px`,
  }
}

// Ein deaktivierter Knopf ist weder anklickbar noch fokussierbar; eine eigene
// Sperre hier wäre toter Code.
async function openMenu(which = 'first') {
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

function onButtonKeydown(event) {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    openMenu(event.key === 'ArrowUp' ? 'last' : 'first')
  }
}

function onToggle() {
  if (open.value) closeMenu(true)
  else openMenu()
}

function onMenuKeydown(event) {
  const list = entries()
  const index = list.indexOf(document.activeElement)
  const last = list.length - 1
  const target = {
    ArrowDown: index >= last ? 0 : index + 1,
    ArrowUp: index <= 0 ? last : index - 1,
    Home: 0,
    End: last,
  }[event.key]
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
  // Leertaste löst bei Links nichts aus; wie Enter behandeln.
  if (event.key === ' ') {
    event.preventDefault()
    document.activeElement?.click()
  }
}

function onSelect(item) {
  closeMenu(true)
  emit('select', item.key)
}

// Ein Link navigiert selbst; das Menü schließt nur, der Fokus bleibt der Navigation überlassen.
const onLink = () => closeMenu()

function onDocumentPointerdown(event) {
  if (!root.value?.contains(event.target)) closeMenu()
}
const onViewportChange = () => closeMenu()

const listeners = [
  ['pointerdown', onDocumentPointerdown, true],
  ['scroll', onViewportChange, true],
  ['resize', onViewportChange, false],
]
const eventTarget = event => (event === 'resize' ? window : document)

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
