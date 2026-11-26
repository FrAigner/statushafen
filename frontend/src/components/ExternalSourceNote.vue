<script setup lang="ts">
/*
  Part D: "external" marker on a component that is linked to an
  external status page.
  - Marker with text ("external") and icon, never colour alone. The marker is a
    link to the source page if the address is http(s) (plain text otherwise).
  - The link opens in a new tab (target=_blank, rel with noopener noreferrer
    nofollow); this is shown as an icon and included in the name for screen readers.
  - Below it, as text (not only as a tooltip), it says which external component
    supplies the status; for a linked group the worst component
    ("worst"). The title attribute repeats it as a tooltip.
  - If the source is down (source_outage), its status no longer counts;
    then the note "data outdated" appears instead of a status.
  Colours only via tokens.
*/
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'
import StatusBadge from './StatusBadge.vue'
import { safeHttpUrl } from '../utils/externalStatus'
import { statusMeta } from '../utils/status'
import type { ExternalComponentRef } from '../types/api'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  // Linked external components (external_sources of the dashboard response).
  sources: ExternalComponentRef[]
  // Name of the own component for the list name.
  name?: string
}>(), {
  name: '',
})
// Stryker restore all

const { t } = useI18n()

const statusText = (status: string) => {
  const key = statusMeta(status).labelKey
  return key ? t(key) : t('external.unknownStatus')
}

const items = computed(() => props.sources.map((r) => {
  // For a group the worst component determines the status.
  const shown = r.is_group && r.worst ? r.worst : { name: r.name, status: r.status }
  const known = statusMeta(shown.status).level !== null
  const title = r.is_group && r.worst
    ? t('external.worstTitle', { source: r.source_name, group: r.name, name: r.worst.name, status: statusText(r.worst.status) })
    : t('external.singleTitle', { source: r.source_name, name: r.name, status: statusText(r.status) })
  return {
    key: `${r.source_id}-${r.external_component_id}`,
    source: r.source_name,
    href: safeHttpUrl(r.url),
    isGroup: !!r.is_group,
    name: r.name,
    shown,
    // Only show what we know: an "unknown" gets no status colour.
    badgeStatus: known ? shown.status : null,
    outdated: !!r.source_outage,
    title,
    linkLabel: t('external.linkLabel', { source: r.source_name, name: r.name }),
  }
}))
</script>

<template>
  <ul class="ext-list" :aria-label="t('external.listLabel', { name })">
    <li v-for="it in items" :key="it.key" class="ext-item">
      <a
        v-if="it.href"
        class="ext-chip ext-chip--link"
        :href="it.href"
        target="_blank"
        rel="noopener noreferrer nofollow"
        :title="it.title"
        :aria-label="it.linkLabel"
      >
        <AppIcon name="globe" :size="12" />
        <span class="ext-chip__tag">{{ t('external.badge') }}</span>
        <span class="ext-chip__src">{{ it.source }}</span>
        <AppIcon name="external-link" :size="12" />
      </a>
      <span v-else class="ext-chip" :title="it.title">
        <AppIcon name="globe" :size="12" />
        <span class="ext-chip__tag">{{ t('external.badge') }}</span>
        <span class="ext-chip__src">{{ it.source }}</span>
      </span>

      <span class="ext-detail">
        <template v-if="it.outdated">{{ t('external.outdated') }}</template>
        <template v-else>
          <span class="ext-detail__what">
            <template v-if="it.isGroup">{{ t('external.groupOf', { name: it.name }) }}<template v-if="it.shown.name !== it.name"> · {{ t('external.worstComponent', { name: it.shown.name }) }}</template></template>
            <template v-else>{{ it.name }}</template>
          </span>
          <StatusBadge v-if="it.badgeStatus" variant="plain" :status="it.badgeStatus" />
          <span v-else class="ext-detail__unknown">{{ t('external.unknownStatus') }}</span>
        </template>
      </span>
    </li>
  </ul>
</template>

<style scoped>
.ext-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--sp-1); min-inline-size: 0; }
.ext-item { display: flex; flex-wrap: wrap; align-items: center; gap: var(--sp-1) var(--sp-2); min-inline-size: 0; }

.ext-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-1);
  padding: 1px 8px;
  border: 1px solid var(--border-strong);
  border-radius: var(--r-sm);
  background: var(--surface-2);
  color: var(--text-2);
  font-size: var(--fs-xs);
  font-weight: 600;
  line-height: 1.4;
  text-decoration: none;
  max-inline-size: 100%;
}
.ext-chip__tag { text-transform: uppercase; letter-spacing: 0.04em; }
.ext-chip__src { font-weight: 500; overflow-wrap: anywhere; }
.ext-chip--link { color: var(--primary); }
.ext-chip--link:hover { background: var(--sunken); text-decoration: underline; }
.ext-chip--link:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }

.ext-detail {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--sp-1) var(--sp-2);
  font-size: var(--fs-sm);
  color: var(--text-2);
  min-inline-size: 0;
  overflow-wrap: anywhere;
}
</style>
