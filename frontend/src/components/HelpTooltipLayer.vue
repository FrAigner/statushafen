<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import i18n from '../i18n'
import { helpDomId, helpTextFor } from '../composables/useHelpText'
import { activeHelp, cancelHide, helpRegistry, scheduleHide, tooltipPosition } from '../directives/helpTooltipState'

// Tooltip-Ebene für v-help. Wird von der Direktive selbst per
// render() in <body> gehängt, sobald das erste Element mit Hilfe erscheint,
// und wieder entfernt, wenn keines mehr da ist (siehe vHelp.js). Deshalb
// gibt es keinen App-Kontext: Texte kommen über i18n.global.
//
// Für jeden benutzten Schlüssel liegt ein Tooltip (role="tooltip") im DOM,
// auch geschlossen (v-show). So zeigt aria-describedby der Felder immer auf
// ein vorhandenes Element und Screenreader lesen die Hilfe beim Fokus vor,
// ohne auf die sichtbare Verzögerung zu warten.
//
// position: fixed, auf den Viewport begrenzt: so schneiden ihn weder
// überlaufende Tabellen noch Ränder ab.

const { t } = i18n.global

const entries = computed(() => [...helpRegistry.keys()]
  .map(key => ({ key, id: helpDomId(key), help: helpTextFor(key) }))
  .filter(entry => entry.help))

const position = ref({ top: '0px', left: '0px' })

function updatePosition() {
  const active = activeHelp.value
  const bubble = active && document.getElementById(helpDomId(active.key))
  if (!bubble) return
  position.value = tooltipPosition(
    active.anchor.getBoundingClientRect(),
    { width: bubble.offsetWidth, height: bubble.offsetHeight },
    // clientWidth statt innerWidth: ohne die Breite einer Scrollleiste.
    { width: document.documentElement.clientWidth, height: window.innerHeight },
  )
}

function listen(on) {
  const method = on ? 'addEventListener' : 'removeEventListener'
  window[method]('scroll', updatePosition, true)
  window[method]('resize', updatePosition)
}

// Mit dem Zeiger in den Tooltip: bleibt offen, Verlassen schließt verzögert.
function onPointerEnter(event) {
  if (event.pointerType !== 'touch') cancelHide()
}

function onPointerLeave(event) {
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
    <p v-for="part in ['what', 'effect', 'example']" :key="part" class="help-tooltip__part">
      <span class="help-tooltip__heading">{{ t(`help.parts.${part}`) }}<span class="help-tooltip__sr">: </span></span>
      {{ entry.help[part] }}
    </p>
  </div>
</template>

<style>
/* Nicht scoped: die Ebene hängt außerhalb der App in <body>. Farben nur über
   Theme-Variablen (tokens.css), damit Hell und Dunkel ohne eigene Regeln
   funktionieren. */
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
  /* Stufe 3: invertierte Blase (Text- auf Flächenfarbe), gleiches
     Kontrastverhältnis wie --text auf --surface, in beiden Themes. */
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

/* Doppelpunkt nur für Screenreader (Beschreibung liest sich „Was: …“). */
.help-tooltip__sr {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
</style>
