<script setup>
/*
  Redesign Stufe 3: Seitenkopf mit Brotkrumen, Titel (h1, per `level` h2),
  Untertitel und Aktionen (Slot "actions"). Ersetzt die .page-header-Varianten
  pro View. Der Seitentitel ist die einzige h1 der Seite (umsetzung.md,
  Barrierefreiheit); Views, die heute h2 nutzen, wechseln in Stufe 4.
  Brotkrumen: Einträge mit `to` sind Links, der letzte Eintrag (ohne `to`) ist
  die aktuelle Seite (aria-current="page").
*/
import { useI18n } from 'vue-i18n'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  title: { type: String, required: true },
  subtitle: { type: String, default: null },
  breadcrumbs: { type: Array, default: () => [] }, // [{ label, to? }]
  // Überschriftenebene: 1 (Standard, öffentliche Seiten) oder 2 (Admin-Views, dort
  // trägt die Topbar die h1, Marke).
  level: { type: Number, default: 1, validator: v => v === 1 || v === 2 },
})
// Stryker restore all

const { t } = useI18n()
</script>

<template>
  <header class="page-head">
    <nav v-if="breadcrumbs.length" class="page-head__crumbs" :aria-label="t('patterns.breadcrumb')">
      <ol>
        <li v-for="(crumb, index) in breadcrumbs" :key="index">
          <router-link v-if="crumb.to" :to="crumb.to">{{ crumb.label }}</router-link>
          <span v-else aria-current="page">{{ crumb.label }}</span>
        </li>
      </ol>
    </nav>
    <div class="page-head__row">
      <div class="page-head__titles">
        <component :is="`h${level}`" class="page-head__title">{{ title }}</component>
        <p v-if="subtitle" class="page-head__subtitle">{{ subtitle }}</p>
      </div>
      <div v-if="$slots.actions" class="page-head__actions"><slot name="actions" /></div>
    </div>
  </header>
</template>

<style scoped>
.page-head { margin-bottom: var(--sp-5); }
.page-head__crumbs ol {
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-1);
  margin: 0 0 var(--sp-2);
  padding: 0;
  list-style: none;
  font-size: var(--fs-sm);
  color: var(--text-2);
}
.page-head__crumbs li + li::before { content: '›'; margin-inline-end: var(--sp-1); color: var(--text-3); }
.page-head__crumbs a { color: var(--primary-text); text-decoration: none; }
.page-head__crumbs a:hover { text-decoration: underline; }
.page-head__crumbs a:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; border-radius: var(--r-sm); }
.page-head__row { display: flex; flex-wrap: wrap; align-items: flex-start; justify-content: space-between; gap: var(--sp-4); }
.page-head__titles { min-inline-size: 0; }
.page-head__title { margin: 0; font-size: var(--fs-2xl); font-weight: 600; line-height: 1.25; color: var(--text); }
.page-head__subtitle { margin: var(--sp-1) 0 0; font-size: var(--fs-base); color: var(--text-2); }
.page-head__actions { display: flex; flex-wrap: wrap; gap: var(--sp-2); align-items: center; }
</style>
