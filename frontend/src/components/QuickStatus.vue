<script setup lang="ts">
/*
  Redesign stage 3: quick status as a listbox button with an icon, replaces
  the native select.inline-select in AdminComponents. Follows the ARIA
  "Select-Only Combobox" pattern: focus stays on the button, the active
  option is in aria-activedescendant.

  Keyboard (logic in utils/listbox.ts):
  - closed: arrows, Enter, Space open (the current value is active)
  - open: arrows/Home/End move, Enter/Space pick, Escape closes without a
    change, Tab closes; a click outside closes.
  - After picking and after Escape, focus stays on the button.

  The value (v-model) is the backend's raw status (operational,
  degraded_performance, partial_outage, major_outage), so views need not
  convert anything. There are four levels; maintenance is not a component
  status (umsetzung.md). An unknown value is shown neutrally and can only be
  replaced by picking an option.
*/
import { computed, nextTick, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'
import StatusIcon from './StatusIcon.vue'
import { STATUS_LEVELS, statusMeta, toRawStatus } from '../utils/status'
import { isOpenKey, isSelectKey, nextActiveIndex } from '../utils/listbox'
import { useDismissable } from '../composables/useDismissable'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  modelValue?: string | null
  // Accessible name, e.g. "Status of API"; default: "Change status".
  label?: string | null
  disabled?: boolean
}>(), {
  modelValue: null,
  label: null,
  disabled: false,
})
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ 'update:modelValue': [status: string] }>()

const { t } = useI18n()
const uid = useId()
const listId = `qs-list-${uid}`
const optionId = (index: number) => `qs-opt-${uid}-${index}`

const options = STATUS_LEVELS.map(level => {
  // Each of the four levels has a raw value and a label.
  const value = toRawStatus(level) as string
  return { value, level, labelKey: statusMeta(level).labelKey as string }
})

const root = ref<HTMLElement | null>(null)
const button = ref<HTMLButtonElement | null>(null)
const open = ref(false)
const active = ref(0)

const selectedIndex = computed(() => options.findIndex(o => o.value === toRawStatus(props.modelValue)))
const current = computed(() => statusMeta(props.modelValue))
const currentText = computed(() => (current.value.labelKey ? t(current.value.labelKey) : (props.modelValue ?? '')))

function openList() {
  active.value = Math.max(selectedIndex.value, 0)
  open.value = true
}

function close() {
  open.value = false
}

function choose(index: number) {
  const { value } = options[index]!
  close()
  if (value !== toRawStatus(props.modelValue)) emit('update:modelValue', value)
  button.value!.focus()
}

function onKeydown(event: KeyboardEvent) {
  if (props.disabled) return
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
  const next = nextActiveIndex(active.value, event.key, options.length)
  if (next === null) return
  event.preventDefault()
  active.value = next
}

function onToggle() {
  if (props.disabled) return
  if (open.value) close()
  else openList()
}

// Click/tap outside closes; Escape is handled in onKeydown.
useDismissable(open, [root], close, { escape: false })

watch(open, async (isOpen) => {
  if (isOpen) {
    await nextTick()
    document.getElementById(optionId(active.value))?.scrollIntoView?.({ block: 'nearest' })
  }
})
</script>

<template>
  <div ref="root" class="quick-status">
    <button
      ref="button"
      type="button"
      role="combobox"
      class="quick-status__button"
      aria-haspopup="listbox"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-controls="listId"
      :aria-activedescendant="open ? optionId(active) : undefined"
      :aria-label="label ?? t('patterns.quickStatus')"
      :disabled="disabled"
      @click="onToggle"
      @keydown="onKeydown"
    >
      <StatusIcon :status="modelValue" :size="16" />
      <span class="quick-status__text">{{ currentText }}</span>
      <AppIcon name="chevron-down" :size="14" class="quick-status__chevron" />
    </button>

    <ul v-show="open" :id="listId" role="listbox" tabindex="-1" class="quick-status__list" :aria-label="label ?? t('patterns.quickStatus')">
      <li
        v-for="(option, index) in options"
        :id="optionId(index)"
        :key="option.value"
        role="option"
        class="quick-status__option"
        :class="{ 'quick-status__option--active': index === active }"
        :aria-selected="index === selectedIndex ? 'true' : 'false'"
        @pointerdown.prevent
        @click="choose(index)"
        @pointermove="active = index"
      >
        <StatusIcon :status="option.level" :size="16" />
        <span>{{ t(option.labelKey) }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.quick-status { position: relative; display: inline-block; }
.quick-status__button {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  min-block-size: var(--ctl-h);
  padding: 0 var(--sp-3);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-md);
  background: var(--surface);
  color: var(--text);
  font: inherit;
  font-size: var(--fs-base);
  cursor: pointer;
}
.quick-status__button:hover:not(:disabled) { border-color: var(--text-3); }
.quick-status__button:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
.quick-status__button:disabled { opacity: .55; cursor: not-allowed; }
.quick-status__chevron { color: var(--text-3); }

.quick-status__list {
  position: absolute;
  inset-block-start: calc(100% + 4px);
  inset-inline-start: 0;
  z-index: 50;
  min-inline-size: 100%;
  margin: 0;
  padding: var(--sp-1);
  list-style: none;
  border-radius: var(--r-md);
  background: var(--surface);
  box-shadow: var(--shadow-pop);
}
.quick-status__option {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  min-block-size: var(--ctl-h);
  padding: 0 var(--sp-3);
  border-radius: var(--r-sm);
  color: var(--text);
  font-size: var(--fs-base);
  white-space: nowrap;
  cursor: pointer;
}
.quick-status__option--active { background: var(--surface-2); }
.quick-status__option[aria-selected='true'] { font-weight: 600; }
</style>
