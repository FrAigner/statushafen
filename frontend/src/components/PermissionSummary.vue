<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RESOURCES, ACTIONS, grantsAdminArea } from '../utils/permissions'
import { NAV_GROUPS } from '../utils/adminNav'
import type { NavItem } from '../utils/adminNav'

type ResourceItem = NavItem & { resource: string }
const hasResource = (item: NavItem): item is ResourceItem => Boolean(item.resource)
import { summarizePermissions } from '../utils/permissionMatrix'

// Compact permissions of a role for the role list, e.g. "Components: CRUD"
// instead of many single permission pills. The first `max` resources appear,
// the rest shows as "+n"; the full listing is in the tooltip and, for screen
// readers, in the text (the visible abbreviations are aria-hidden, "CRUD" alone
// tells screen readers nothing). If every tab is fully granted, it says
// "All permissions". The chips wrap and truncate overly long names, the cell
// never overflows.
// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  permissions?: string
  max?: number
}>(), {
  permissions: '',
  max: 3,
})
// Stryker restore all
const { t, te } = useI18n()

const labelKeys = Object.fromEntries(
  NAV_GROUPS.flatMap(g => g.items).filter(hasResource).map(i => [i.resource, i.labelKey]))
const resourceKeys = NAV_GROUPS.flatMap(g => g.items).filter(hasResource).map(i => i.resource)

const label = (resource: string) => t(`adminNav.items.${labelKeys[resource]}`)
const specialLabel = (key: string) => (te(`roles.matrix.labels.${key}`) ? t(`roles.matrix.labels.${key}`) : key)

const summary = computed(() => summarizePermissions(props.permissions, resourceKeys))
// Without an admin right the role is "status page only"; admin_dashboard has
// no effect then and is not listed.
const pageOnly = computed(() => !grantsAdminArea(props.permissions))

const everything = computed(() => summary.value.rows.length === RESOURCES.length
  && summary.value.rows.every(r => r.actions.length === ACTIONS.length))

// All entries as chips: resource with abbreviations, then special permissions.
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
  <span v-if="pageOnly" class="perm-summary perm-summary--none">{{ t('roles.summary.pageOnly') }}</span>
  <!-- With an admin right there is always at least one chip (pageOnly covers the rest). -->
  <span v-else class="perm-summary" :title="detail">
    <span class="sr-only">{{ detail }}</span>
    <span class="perm-summary__chips" aria-hidden="true">
      <span v-if="everything" class="tag-pill perm-summary__chip">{{ t('roles.summary.all') }}</span>
      <template v-else>
        <span v-for="chip in shown" :key="chip.key" class="tag-pill perm-summary__chip">{{ chip.text }}</span>
        <span v-if="hidden" class="tag-pill perm-summary__chip perm-summary__more">{{ t('roles.summary.more', { n: hidden }) }}</span>
      </template>
    </span>
  </span>
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
