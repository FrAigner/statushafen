<script setup>
/*
  Gemeinsamer Bestätigungsdialog (ersetzt den nativen Browser-Dialog). Einmal in App.vue
  eingebunden, angesteuert über composables/useConfirm.js.
  - role="alertdialog" mit aria-labelledby/-describedby, Esc und Klick auf den Hintergrund brechen ab.
  - Fokus startet auf „Abbrechen“ (Enter bestätigt nie versehentlich eine gefährliche Aktion),
    Tab bleibt im Dialog, beim Schließen kehrt der Fokus zum auslösenden Element zurück.
  - Tastatur (Esc, Tab-Falle) hängt am Dokument, solange der Dialog offen ist; auch ein Klick auf den
    Text (Fokus auf <body>) hebt Esc nicht auf. Die Seite dahinter ist inert, der Body scrollt nicht.
  - Navigation (Router), Abmeldung und 401 schließen den Dialog mit „abgebrochen“: Die Rückfrage gehörte
    zur alten Seite, ein späteres Bestätigen darf nichts mehr auslösen.
*/
import { ref, watch, nextTick, computed, inject, onMounted, onBeforeUnmount } from 'vue'
import { routerKey } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useConfirmState, answerConfirm } from '../composables/useConfirm'
import { useAuth } from '../composables/useAuth'

const { t } = useI18n()
const state = useConfirmState()
const { token } = useAuth()
const router = inject(routerKey, null)
const dialogEl = ref(null)
const cancelBtn = ref(null)

const title = computed(() => state.title || t('confirm.title'))
const confirmText = computed(() => state.confirmLabel || (state.danger ? t('common.delete') : t('confirm.confirm')))

function answer(value) {
  const opener = answerConfirm(value)
  if (opener && typeof opener.focus === 'function' && opener.isConnected) {
    nextTick(() => opener.focus())
  }
}

function onKeydown(e) {
  if (e.key === 'Escape') {
    e.preventDefault()
    answer(false)
    return
  }
  if (e.key !== 'Tab' || !dialogEl.value) return
  const items = dialogEl.value.querySelectorAll('button')
  const first = items[0]
  const last = items[items.length - 1]
  const inside = dialogEl.value.contains(document.activeElement)
  if (!inside) {
    e.preventDefault()
    ;(e.shiftKey ? last : first).focus()
  } else if (e.shiftKey && document.activeElement === first) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault()
    first.focus()
  }
}

let previousOverflow = ''
function lock(on) {
  const app = document.getElementById('app')
  if (on) {
    document.addEventListener('keydown', onKeydown, true)
    previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    if (app) app.inert = true
  } else {
    document.removeEventListener('keydown', onKeydown, true)
    document.body.style.overflow = previousOverflow
    if (app) app.inert = false
  }
}

// flush 'sync': die Seite ist wieder bedienbar, bevor answer() den Fokus zurückgibt.
watch(() => state.open, (open) => lock(open), { flush: 'sync' })
watch(() => state.open, async (open) => {
  if (!open) return
  await nextTick()
  cancelBtn.value?.focus()
})

let removeHook = null
onMounted(() => {
  removeHook = router?.afterEach(() => { if (state.open) answer(false) })
})
onBeforeUnmount(() => {
  removeHook?.()
  if (state.open) lock(false)
})
watch(token, (value) => { if (!value && state.open) answer(false) })
</script>

<template>
  <Teleport to="body">
    <div v-if="state.open" class="confirm-backdrop" :class="state.themeClass" data-test="confirm-backdrop" @click.self="answer(false)">
      <div
        ref="dialogEl"
        class="confirm-dialog"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="confirm-dialog-title"
        aria-describedby="confirm-dialog-message"
      >
        <h2 id="confirm-dialog-title" class="confirm-dialog__title">{{ title }}</h2>
        <p id="confirm-dialog-message" class="confirm-dialog__message">{{ state.message }}</p>
        <div class="confirm-dialog__actions">
          <button ref="cancelBtn" type="button" class="btn btn-secondary" data-test="confirm-cancel" @click="answer(false)">
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="btn"
            :class="state.danger ? 'btn-danger' : 'btn-primary'"
            data-test="confirm-ok"
            @click="answer(true)"
          >
            {{ confirmText }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.confirm-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1100;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  background: var(--overlay);
}
.confirm-dialog {
  width: 100%;
  max-width: 440px;
  padding: 24px;
  background: var(--surface);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-pop);
}
.confirm-dialog__title { margin: 0 0 8px; font-size: 1.1rem; }
.confirm-dialog__message { margin: 0 0 20px; color: var(--text-2); overflow-wrap: anywhere; }
.confirm-dialog__actions { display: flex; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
</style>
