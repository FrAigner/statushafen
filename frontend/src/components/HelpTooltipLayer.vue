<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import i18n from '../i18n'
import { HELP_PARTS, helpDomId, helpTextFor } from '../composables/useHelpText'
import type { HelpText } from '../composables/useHelpText'
import { activeHelp, cancelHide, helpRegistry, scheduleHide, tooltipPosition } from '../directives/helpTooltipState'

// Tooltip layer for v-help. The directive itself mounts it into <body>
// via render() as soon as the first element with help appears, and removes
// it again when none is left (see vHelp.ts). Hence there is no app
// context: texts come via i18n.global.
//
// Every key in use has a tooltip (role="tooltip") in the DOM, even when
// closed (v-show). So the fields' aria-describedby always points to an
// existing element and screen readers read the help on focus without
// waiting for the visible delay.
//
// position: fixed, constrained to the viewport: so neither overflowing
// tables nor edges clip it.

const { t } = i18n.global

const entries = computed(() => [...helpRegistry.keys()]
  .map(key => ({ key, id: helpDomId(key), help: helpTextFor(key) }))
  .filter((entry): entry is { key: string, id: string, help: HelpText } => !!entry.help))

const position = ref({ top: '0px', left: '0px' })

function updatePosition() {
  const active = activeHelp.value
  const bubble = active && document.getElementById(helpDomId(active.key))
  if (!bubble) return
  position.value = tooltipPosition(
    active.anchor.getBoundingClientRect(),
    { width: bubble.offsetWidth, height: bubble.offsetHeight },
    // clientWidth instead of innerWidth: without the width of a scrollbar.
    { width: document.documentElement.clientWidth, height: window.innerHeight },
  )
}

function listen(on: boolean) {
  const method = on ? 'addEventListener' : 'removeEventListener'
  window[method]('scroll', updatePosition, true)
  window[method]('resize', updatePosition)
}

// Pointer moves into the tooltip: it stays open, leaving closes it with a delay.
function onPointerEnter(event: PointerEvent) {
  if (event.pointerType !== 'touch') cancelHide()
}

function onPointerLeave(event: PointerEvent) {
  if (event.pointerType !== 'touch') scheduleHide()
}

watch(activeHelp, async (active) => {
  listen(!!active)
  if (!active) return
  await nextTick()
  updatePosition()
})

onBeforeUnmount(() => listen(false))
</script>

<template>
  <div
    v-for="entry in entries"
    v-show="activeHelp?.key === entry.key"
    :id="entry.id"
    :key="entry.key"
    role="tooltip"
    class="help-tooltip"
    :style="activeHelp?.key === entry.key ? position : undefined"
    @pointerenter="onPointerEnter"
    @pointerleave="onPointerLeave"
  >
    <p v-for="part in HELP_PARTS" :key="part" class="help-tooltip__part">
      <span class="help-tooltip__heading">{{ t(`help.parts.${part}`) }}<span class="help-tooltip__sr">: </span></span>
      {{ entry.help[part] }}
    </p>
  </div>
</template>

<style>
/* Not scoped: the layer lives outside the app in <body>. Colours only via
   theme variables (tokens.css), so light and dark work without extra rules. */
.help-tooltip {
  position: fixed;
  z-index: 1000;
  display: flex;
  flex-direction: column;
  gap: 5px;
  box-sizing: border-box;
  inline-size: max-content;
  max-inline-size: min(300px, calc(100vw - 16px));
  margin: 0;
  padding: var(--sp-2) var(--sp-3);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-md);
  /* Stage 3: inverted bubble (text colour as background, surface colour as text),
     same contrast ratio as --text on --surface, in both themes. */
  background: var(--text);
  color: var(--surface);
  box-shadow: var(--shadow-pop);
  font-size: var(--fs-sm);
  line-height: 1.4;
  text-align: start;
  white-space: normal;
  overflow-wrap: anywhere;
  animation: help-tooltip-in 120ms ease-out;
}

@keyframes help-tooltip-in {
  from { opacity: 0; transform: translateY(-2px); }
  to { opacity: 1; transform: none; }
}

@media (prefers-reduced-motion: reduce) {
  .help-tooltip { animation: none; }
}

.help-tooltip__part {
  margin: 0;
}

.help-tooltip__heading {
  margin-inline-end: 4px;
  font-size: var(--fs-xs);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: .04em;
  color: var(--surface-2);
}

/* Colon only for screen readers (the description reads "What: …"). */
.help-tooltip__sr {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
</style>
