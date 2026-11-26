<script setup lang="ts">
// Redesign 4a: status per row via StatusBadge (shape + colour +
// text) instead of a colour dot and .badge-* classes; the indent comes from
// classes instead of an inline style; groups can be expanded and collapsed
// with the keyboard (button with aria-expanded).
import { ref } from 'vue'
import AppIcon from './AppIcon.vue'
import StatusBadge from './StatusBadge.vue'
import ExternalSourceNote from './ExternalSourceNote.vue'
import { visibleExternalSources } from '../utils/externalStatus'
import type { Component, ExternalComponentRef, Tag } from '../types/api'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  node: Component
  depth?: number
}>(), {
  depth: 0
})
// Stryker restore all

// Expanded automatically if the status is not operational
const isOpen = ref(props.node.Status !== 'operational')

// Linked external sources ("external" marker with a link).
// Component.external_sources is typed loosely (ExternalStatusRef); the dashboard sends the full link.
const externalSources = visibleExternalSources(props.node) as ExternalComponentRef[]

const hasChildren = props.node.Children && props.node.Children.length > 0

// Replacement for the former node.Country: hide the reserved global tag,
// because - like "global" before - it carries no additional information for
// visitors.
const nonGlobalTagNames = (tags: Tag[] | undefined) =>
  (tags || []).filter(t => !(t.StatusPageID == null && t.Name?.toLowerCase() === 'global'))
    .map(t => t.Name).join(', ')

// Indent as a class (tree-d0 … tree-d5) instead of an inline style; deeper
// nested levels share the last step, so the row does not disappear on
// narrow screens.
const MAX_INDENT_LEVEL = 5
const indentClass = `tree-d${Math.min(props.depth, MAX_INDENT_LEVEL)}`

const toggle = () => { isOpen.value = !isOpen.value }
</script>

<template>
  <div class="tree-node">
    <!-- Node row -->
    <div
      class="tree-row"
      :class="[indentClass, { 'tree-row--group': hasChildren }, `tree-row--${node.Status}`]"
      @click="hasChildren ? toggle() : null"
    >
      <button
        v-if="hasChildren"
        type="button"
        class="tree-toggle"
        :class="{ open: isOpen }"
        :aria-expanded="isOpen ? 'true' : 'false'"
        :aria-label="node.Name"
      >
        <AppIcon name="chevron-right" :size="12" />
      </button>
      <span v-else class="tree-toggle tree-toggle--spacer" aria-hidden="true">
        <AppIcon name="chevron-right" :size="12" />
      </span>

      <span class="tree-name">{{ node.Name }}</span>
      <span v-if="node.Description" class="tree-desc">{{ node.Description }}</span>

      <span class="tree-spacer"></span>

      <span v-if="nonGlobalTagNames(node.Tags)" class="meta-tag">
        {{ nonGlobalTagNames(node.Tags) }}
      </span>
      <StatusBadge class="tree-status" :status="node.Status" />
    </div>

    <!-- Marker for external sources below the row -->
    <div v-if="externalSources.length > 0" class="tree-ext" :class="[indentClass, `tree-row--${node.Status}`]">
      <ExternalSourceNote :sources="externalSources" :name="node.Name" />
    </div>

    <!-- Children (collapsible) -->
    <Transition name="tree-slide">
      <div v-if="hasChildren && isOpen" class="tree-children">
        <ComponentTreeNode
          v-for="child in node.Children"
          :key="child.ID"
          :node="child"
          :depth="depth + 1"
        />
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.tree-row {
  --indent: 0;
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  padding-block: var(--sp-3);
  padding-inline: calc(var(--sp-4) + var(--indent) * 22px) var(--sp-4);
  border-bottom: 1px solid var(--border);
  transition: background-color 0.12s;
}
.tree-d1 { --indent: 1; }
.tree-d2 { --indent: 2; }
.tree-d3 { --indent: 3; }
.tree-d4 { --indent: 4; }
.tree-d5 { --indent: 5; }

.tree-row--group {
  cursor: pointer;
  background: var(--surface-2);
}
.tree-row--group:hover { background: var(--sunken); }

/* Subtle status tint of the row; the badge carries the information itself. */
.tree-row--degraded_performance { background: var(--deg-soft); }
.tree-row--partial_outage { background: var(--part-soft); }
.tree-row--major_outage { background: var(--major-soft); }

/* Row with an external source: the divider only comes below the source line. */
.tree-row:has(+ .tree-ext) { border-bottom: 0; padding-block-end: var(--sp-1); }

/* External sources: indented below the name (24 px button + gap). */
.tree-ext {
  --indent: 0;
  padding-block: 0 var(--sp-3);
  padding-inline: calc(var(--sp-4) + var(--indent) * 22px + 36px) var(--sp-4);
  border-bottom: 1px solid var(--border);
}
.tree-ext.tree-d1 { --indent: 1; }
.tree-ext.tree-d2 { --indent: 2; }
.tree-ext.tree-d3 { --indent: 3; }
.tree-ext.tree-d4 { --indent: 4; }
.tree-ext.tree-d5 { --indent: 5; }
.tree-ext.tree-row--degraded_performance { background: var(--deg-soft); }
.tree-ext.tree-row--partial_outage { background: var(--part-soft); }
.tree-ext.tree-row--major_outage { background: var(--major-soft); }

.tree-toggle {
  inline-size: 24px;
  block-size: 24px;
  flex-shrink: 0;
  padding: 0;
  border: 0;
  border-radius: var(--r-sm);
  background: none;
  color: var(--text-3);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: transform 0.2s;
}
.tree-toggle:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
.tree-toggle.open { transform: rotate(90deg); }
.tree-toggle--spacer { visibility: hidden; }

.tree-name {
  font-size: var(--fs-base);
  font-weight: 500;
  color: var(--text);
  white-space: nowrap;
  flex-shrink: 0;
}
.tree-row--group .tree-name { font-weight: 600; }

.tree-desc {
  font-size: var(--fs-sm);
  font-weight: 400;
  color: var(--text-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tree-spacer { flex: 1; min-inline-size: 12px; }
.meta-tag { flex-shrink: 0; }
.tree-status { flex-shrink: 0; }

/* In a narrow view (390 px) the single-line row did not fit:
   .tree-name is nowrap and does not shrink, neither does the badge, so the
   row ran off the right edge (the page scrolled horizontally). Instead of
   truncating the name, the row wraps here: name (wraps itself) on line 1,
   description below, tag and badge on the last line. The spacer forces the
   line break with flex-basis 100 %; the indent below the name (24 px button +
   12 px gap) puts description and badge below the name instead of below the
   arrow. On wide screens everything stays on one line. */
@media (max-width: 600px) {
  .tree-row { flex-wrap: wrap; row-gap: 0; }

  .tree-name {
    flex: 1 1 0;
    min-width: 0;
    white-space: normal;
    overflow-wrap: anywhere;
  }

  .tree-desc {
    flex: 1 0 100%;
    box-sizing: border-box;
    padding-inline-start: 36px;
    margin-block-start: 2px;
    white-space: normal;
    overflow-wrap: anywhere;
  }

  .tree-spacer { flex: 0 0 100%; min-inline-size: 0; block-size: 0; }
  .tree-spacer + * { margin-inline-start: 36px; }

  .tree-row .meta-tag,
  .tree-row .tree-status { margin-block-start: var(--sp-2); }
}

/* Expand/collapse. All transitions ≤ 200 ms; off under prefers-reduced-motion
   (style.css). */
.tree-slide-enter-active,
.tree-slide-leave-active {
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  overflow: hidden;
}
.tree-slide-enter-from,
.tree-slide-leave-to { opacity: 0; max-height: 0; }
.tree-slide-enter-to,
.tree-slide-leave-from { opacity: 1; max-height: 2000px; }
</style>
