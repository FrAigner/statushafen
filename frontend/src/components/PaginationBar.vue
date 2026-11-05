<script setup>
/*
  Wiederverwendbare Paginierung für lange Admin-Listen (clientseitig).
  Der Baustein hält KEINEN Listenzustand: Seite und Seitengröße kommen per
  v-model, damit ungespeicherte Eingaben im Elternzustand bleiben und nicht
  in der DOM-Zeile. Die Bereichsangabe „Zeilen x–y von z“ steht in einer
  aria-live-Region (höflich), damit Screenreader den Seitenwechsel hören.
  Nach einer Bedienung per Knopf/Seitengröße meldet `navigate`, dass der
  Elternteil den Fokus in die Liste setzen soll (sonst bleibt der Fokus auf
  einem inzwischen deaktivierten Knopf hängen).
*/
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  total: { type: Number, required: true },
  pageSizes: { type: Array, default: () => [25, 50, 100] },
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['navigate'])
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const page = defineModel('page', { type: Number, default: 1 })
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const pageSize = defineModel('pageSize', { type: Number, default: 50 })

const { t } = useI18n()

const totalPages = computed(() => Math.max(1, Math.ceil(props.total / pageSize.value)))
const current = computed(() => Math.min(Math.max(1, page.value), totalPages.value))
const from = computed(() => (props.total === 0 ? 0 : (current.value - 1) * pageSize.value + 1))
const to = computed(() => Math.min(props.total, current.value * pageSize.value))

const go = (target) => {
  page.value = target
  emit('navigate')
}
const changeSize = (event) => {
  pageSize.value = Number(event.target.value)
  page.value = 1
  emit('navigate')
}
</script>

<template>
  <nav class="pager" :aria-label="t('pagination.label')">
    <p class="pager-info" role="status" aria-live="polite" aria-atomic="true">
      {{ t('pagination.range', { from, to, total }) }}
    </p>
    <label class="pager-size">
      <span>{{ t('pagination.perPage') }}</span>
      <select class="form-select" :value="pageSize" @change="changeSize">
        <option v-for="size in pageSizes" :key="size" :value="size">{{ size }}</option>
      </select>
    </label>
    <div class="pager-nav">
      <button type="button" class="action-btn pager-btn" :disabled="current <= 1" @click="go(current - 1)">
        <AppIcon name="arrow-left" :size="13" /> {{ t('pagination.prev') }}
      </button>
      <span class="pager-page">{{ t('pagination.pageOf', { page: current, totalPages }) }}</span>
      <button type="button" class="action-btn pager-btn" :disabled="current >= totalPages" @click="go(current + 1)">
        {{ t('pagination.next') }} <AppIcon name="arrow-right" :size="13" />
      </button>
    </div>
  </nav>
</template>

<style scoped>
.pager { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--sp-3) var(--sp-4); padding: var(--sp-3) var(--sp-4); border-top: 1px solid var(--border); }
.pager-info { margin: 0; font-size: var(--fs-sm); color: var(--text-3); }
.pager-size { display: inline-flex; align-items: center; gap: var(--sp-2); font-size: var(--fs-sm); color: var(--text-3); }
.pager-size .form-select { inline-size: auto; min-inline-size: 72px; }
.pager-nav { display: inline-flex; align-items: center; gap: var(--sp-3); }
.pager-page { font-size: var(--fs-sm); color: var(--text-3); }
.pager-btn { display: inline-flex; align-items: center; gap: 6px; }
@media (max-width: 859px) {
  .pager { justify-content: center; }
}
</style>
