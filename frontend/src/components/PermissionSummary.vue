<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RESOURCES, ACTIONS } from '../utils/permissions'
import { NAV_GROUPS } from '../utils/adminNav'
import { summarizePermissions } from '../utils/permissionMatrix'

// Rechte einer Rolle kompakt für die Rollenliste, z. B.
// „Komponenten: CRUD“ statt vieler einzelner Rechte-Pillen. Es erscheinen die
// ersten `max` Ressourcen, der Rest steht als „+n“; die vollständige Aufzählung
// steht im Tooltip und für Screenreader im Text (die sichtbaren Kürzel sind
// aria-hidden, „CRUD“ allein sagt Screenreadern nichts). Sind alle Tabs
// vollständig vergeben, steht „Alle Rechte“. Die Chips brechen um und kürzen zu
// lange Namen, die Zelle läuft nie über.
// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  permissions: { type: String, default: '' },
  max: { type: Number, default: 3 },
})
// Stryker restore all
const { t, te } = useI18n()

const labelKeys = Object.fromEntries(
  NAV_GROUPS.flatMap(g => g.items).filter(i => i.resource).map(i => [i.resource, i.labelKey]))
const resourceKeys = NAV_GROUPS.flatMap(g => g.items).filter(i => i.resource).map(i => i.resource)

const label = (resource) => t(`adminNav.items.${labelKeys[resource]}`)
const specialLabel = (key) => (te(`roles.matrix.labels.${key}`) ? t(`roles.matrix.labels.${key}`) : key)

const summary = computed(() => summarizePermissions(props.permissions, resourceKeys))

const everything = computed(() => summary.value.rows.length === RESOURCES.length
  && summary.value.rows.every(r => r.actions.length === ACTIONS.length))

// Alle Einträge als Chips: Ressource mit Kürzeln, dann Sonderrechte.
const chips = computed(() => [
  ...summary.value.rows.map(r => ({ key: r.resource, text: `${label(r.resource)}: ${r.letters}` })),
  ...summary.value.special.map(p => ({ key: p, text: specialLabel(p) })),
])
const shown = computed(() => chips.value.slice(0, props.max))
const hidden = computed(() => Math.max(0, chips.value.length - props.max))

const detail = computed(() => {
  if (everything.value) return t('roles.summary.all')
  return [
    ...summary.value.rows.map(r => `${label(r.resource)}: ${r.actions.map(a => t(`roles.matrix.${a}`)).join(', ')}`),
    ...summary.value.special.map(specialLabel),
  ].join('; ')
})
</script>

<template>
  <span v-if="chips.length" class="perm-summary" :title="detail">
    <span class="sr-only">{{ detail }}</span>
    <span class="perm-summary__chips" aria-hidden="true">
      <span v-if="everything" class="tag-pill perm-summary__chip">{{ t('roles.summary.all') }}</span>
      <template v-else>
        <span v-for="chip in shown" :key="chip.key" class="tag-pill perm-summary__chip">{{ chip.text }}</span>
        <span v-if="hidden" class="tag-pill perm-summary__chip perm-summary__more">{{ t('roles.summary.more', { n: hidden }) }}</span>
      </template>
    </span>
  </span>
  <span v-else class="perm-summary perm-summary--none">{{ t('roles.summary.none') }}</span>
</template>

<style scoped>
.perm-summary {
  display: block;
  min-width: 0;
  max-width: 100%;
}
.perm-summary__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  min-width: 0;
}
.perm-summary__chip {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.perm-summary--none {
  color: var(--text-3);
}
</style>
