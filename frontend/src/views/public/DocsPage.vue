<script setup>
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppIcon from '../../components/AppIcon.vue'
import {
  SECTION_IDS,
  extractTitle,
  stripTitle,
  plainText,
  renderMarkdown,
  resolveSection,
} from '../../docs'

// Doku-Seite. Ein Abschnitt je Unterseite (/docs/<abschnitt>),
// Überschriften darin per Anker erreichbar (/docs/vorfaelle#status-ablauf).
// Inhalte und Titel kommen ausschließlich aus src/docs/<sprache>/*.md.
const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()

const sections = computed(() => SECTION_IDS.map(id => {
  const resolved = resolveSection(id, locale.value)
  return {
    id,
    title: extractTitle(resolved?.markdown) || id,
    text: plainText(resolved?.markdown),
  }
}))

const currentId = computed(() => route.params.section || SECTION_IDS[0])
const current = computed(() => resolveSection(currentId.value, locale.value))
const title = computed(() => extractTitle(current.value?.markdown))
const rendered = computed(() => renderMarkdown(stripTitle(current.value?.markdown)))
const currentIndex = computed(() => SECTION_IDS.indexOf(currentId.value))
const prev = computed(() => currentIndex.value > 0 ? sections.value[currentIndex.value - 1] : null)
const next = computed(() => {
  // Der Pager erscheint nur bei einem gefundenen Abschnitt, der Index ist dann >= 0.
  const i = currentIndex.value
  return i < SECTION_IDS.length - 1 ? sections.value[i + 1] : null
})

const query = ref('')
const filteredSections = computed(() => {
  // Gleiche Normalisierung wie für den Text (z. B. "X-Webhook-Token").
  const q = plainText(query.value).trim()
  if (!q) return sections.value
  return sections.value.filter(s => plainText(s.title).includes(q) || s.text.includes(q))
})

const navOpen = ref(false)

const scrollToHash = async () => {
  await nextTick()
  const id = decodeURIComponent(route.hash.slice(1))
  if (!id) return
  document.getElementById(id)?.scrollIntoView({ block: 'start' })
}

// /docs#vorfaelle (Abschnitt als Anker auf der Startseite der Doku) leitet auf
// die Unterseite /docs/vorfaelle um, damit beide Schreibweisen funktionieren.
const redirectSectionHash = () => {
  const id = route.hash.slice(1)
  if (!route.params.section && SECTION_IDS.includes(id)) {
    router.replace({ path: `/docs/${id}` })
    return true
  }
  return false
}

onMounted(() => {
  if (!redirectSectionHash()) scrollToHash()
})

watch(() => route.fullPath, () => {
  navOpen.value = false
  if (!redirectSectionHash()) scrollToHash()
})

// Links im gerenderten Markdown, die innerhalb der App bleiben (/docs/...,
// #anker), über den Router öffnen statt die Seite neu zu laden.
const onContentClick = (event) => {
  const link = event.target.closest('a[href]')
  if (!link) return
  const href = link.getAttribute('href')
  if (href.startsWith('#')) {
    event.preventDefault()
    router.push({ path: route.path, hash: href })
  } else if (href.startsWith('/') && !href.startsWith('//') && !href.startsWith('/api/')) {
    event.preventDefault()
    router.push(href)
  }
}

// Fokus auf den Inhalt, damit Tastatur- und Screenreader-Nutzer oben weiterlesen.
const contentEl = ref(null)
const backToTop = () => {
  window.scrollTo({ top: 0, behavior: 'smooth' })
  contentEl.value.focus()
}
</script>

<template>
  <div class="docs" :class="{ 'is-nav-open': navOpen }">
    <button
      type="button"
      class="docs-nav-toggle"
      aria-controls="docs-sidebar"
      :aria-expanded="navOpen ? 'true' : 'false'"
      @click="navOpen = !navOpen"
    >
      <AppIcon :name="navOpen ? 'x' : 'chevron-down'" :size="16" />
      {{ navOpen ? t('docs.navClose') : t('docs.navOpen') }}
    </button>

    <aside id="docs-sidebar" class="docs-sidebar">
      <div class="docs-search">
        <label for="docs-search-input" class="docs-search-label">{{ t('docs.searchLabel') }}</label>
        <input
          id="docs-search-input"
          v-model="query"
          type="search"
          class="docs-search-input"
          :placeholder="t('docs.searchPlaceholder')"
        />
      </div>
      <nav class="docs-nav" :aria-label="t('docs.navLabel')">
        <ul>
          <li v-for="s in filteredSections" :key="s.id">
            <router-link
              :to="`/docs/${s.id}`"
              class="docs-nav-link"
              :class="{ active: s.id === currentId }"
              :aria-current="s.id === currentId ? 'page' : undefined"
            >{{ s.title }}</router-link>
          </li>
        </ul>
        <p v-if="filteredSections.length === 0" class="docs-nav-empty" role="status">
          {{ t('docs.searchEmpty') }}
        </p>
      </nav>
    </aside>

    <article class="docs-content">
      <template v-if="current">
        <p v-if="current.fallback" class="docs-fallback" role="note">
          {{ t('docs.fallbackNotice') }}
        </p>

        <h1 id="docs-title" ref="contentEl" class="docs-title" tabindex="-1">{{ title }}</h1>

        <nav v-if="rendered.headings.length" class="docs-toc" :aria-label="t('docs.tocLabel')">
          <p class="docs-toc-title">{{ t('docs.tocTitle') }}</p>
          <ul>
            <li v-for="h in rendered.headings" :key="h.id" :class="`docs-toc-level-${h.depth}`">
              <router-link :to="{ path: route.path, hash: `#${h.id}` }">{{ h.text }}</router-link>
            </li>
          </ul>
        </nav>

        <!-- eslint-disable-next-line vue/no-v-html -- Markdown aus dem Repo, durch DOMPurify bereinigt (src/docs/index.js) -->
        <div class="docs-markdown" @click="onContentClick" v-html="rendered.html" />

        <nav class="docs-pager" :aria-label="t('docs.pagerLabel')">
          <router-link v-if="prev" :to="`/docs/${prev.id}`" class="docs-pager-link docs-pager-prev">
            <AppIcon name="arrow-left" :size="14" /> {{ prev.title }}
          </router-link>
          <span v-else />
          <router-link v-if="next" :to="`/docs/${next.id}`" class="docs-pager-link docs-pager-next">
            {{ next.title }} <AppIcon name="arrow-right" :size="14" />
          </router-link>
        </nav>

        <button type="button" class="docs-back-to-top" @click="backToTop">
          {{ t('docs.backToTop') }}
        </button>
      </template>
      <div v-else class="docs-not-found" role="alert">
        <h1>{{ t('docs.notFoundTitle') }}</h1>
        <p>{{ t('docs.notFoundText') }}</p>
        <router-link to="/docs">{{ t('docs.notFoundLink') }}</router-link>
      </div>
    </article>
  </div>
</template>

<style scoped>
.docs {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  gap: 32px;
  align-items: start;
}

.docs-nav-toggle {
  display: none;
}

.docs-sidebar {
  position: sticky;
  top: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  max-height: calc(100vh - 32px);
  overflow-y: auto;
}

.docs-search-label {
  display: block;
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--text-2);
  margin-bottom: 4px;
}

.docs-search-input {
  box-sizing: border-box;
  width: 100%;
  height: 36px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text);
  font-family: var(--font-sans);
  font-size: 0.85rem;
}

.docs-search-input:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 1px;
}

.docs-nav ul {
  list-style: none;
  margin: 0;
  padding: 0;
}

.docs-nav-link {
  display: block;
  padding: 6px 10px;
  border-radius: var(--radius-sm);
  color: var(--text-2);
  font-size: 0.875rem;
  line-height: 1.35;
}

.docs-nav-link:hover {
  background: var(--surface-2);
  color: var(--text);
  text-decoration: none;
}

.docs-nav-link.active {
  background: var(--primary-soft);
  color: var(--primary);
  font-weight: 600;
}

.docs-nav-link:focus-visible,
.docs-pager-link:focus-visible,
.docs-toc a:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 1px;
}

.docs-nav-empty {
  font-size: 0.85rem;
  color: var(--text-3);
  padding: 6px 10px;
}

.docs-content {
  min-width: 0;
  color: var(--text);
}

.docs-fallback {
  padding: 10px 14px;
  margin-bottom: 16px;
  border: 1px solid var(--border);
  border-left: 3px solid var(--primary);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-2);
  font-size: 0.875rem;
}

/* Inhaltsverzeichnis als Kasten über dem Text statt seitlich umflossen:
   Tabellen behalten so die volle Breite. */
.docs-toc {
  margin: 0 0 20px;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  font-size: 0.8rem;
}

.docs-toc-title {
  font-weight: 600;
  color: var(--text-2);
  margin-bottom: 6px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  font-size: 0.7rem;
}

.docs-toc ul {
  list-style: none;
  margin: 0;
  padding: 0;
  columns: 2;
  column-gap: 24px;
}

.docs-toc li {
  padding: 2px 0;
}

.docs-toc-level-3 {
  padding-left: 12px !important;
}

.docs-title {
  font-size: 1.6rem;
  line-height: 1.25;
  margin-bottom: 16px;
}

.docs-title:focus {
  outline: none;
}

.docs-markdown :deep(h2) {
  font-size: 1.2rem;
  margin: 28px 0 10px;
  padding-top: 8px;
  border-top: 1px solid var(--border);
}

.docs-markdown :deep(h3) {
  font-size: 1rem;
  margin: 20px 0 8px;
}

.docs-markdown :deep(h2),
.docs-markdown :deep(h3) {
  scroll-margin-top: 16px;
}

.docs-markdown :deep(p),
.docs-markdown :deep(ul),
.docs-markdown :deep(ol),
.docs-markdown :deep(table),
.docs-markdown :deep(pre),
.docs-markdown :deep(blockquote) {
  margin-bottom: 12px;
  line-height: 1.6;
  font-size: 0.95rem;
}

.docs-markdown :deep(ul),
.docs-markdown :deep(ol) {
  padding-left: 22px;
}

.docs-markdown :deep(li) {
  margin-bottom: 4px;
}

.docs-markdown :deep(code) {
  font-family: var(--font-mono);
  font-size: 0.85em;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--surface-2);
  border: 1px solid var(--border);
}

.docs-markdown :deep(pre) {
  padding: 12px 14px;
  border-radius: var(--radius-sm);
  background: var(--surface-2);
  border: 1px solid var(--border);
  overflow-x: auto;
}

.docs-markdown :deep(pre code) {
  padding: 0;
  border: none;
  background: none;
  font-size: 0.8rem;
}

.docs-markdown :deep(table) {
  display: block;
  overflow-x: auto;
  border-collapse: collapse;
  font-size: 0.875rem;
}

.docs-markdown :deep(th),
.docs-markdown :deep(td) {
  border: 1px solid var(--border);
  padding: 6px 10px;
  text-align: left;
  vertical-align: top;
}

.docs-markdown :deep(th) {
  background: var(--surface-2);
}

.docs-markdown :deep(blockquote) {
  padding: 8px 14px;
  border-left: 3px solid var(--border-strong);
  color: var(--text-2);
}

.docs-pager {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  margin-top: 32px;
  padding-top: 16px;
  border-top: 1px solid var(--border);
  font-size: 0.875rem;
}

.docs-pager-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.docs-pager-next {
  margin-left: auto;
  text-align: right;
}

.docs-back-to-top {
  margin-top: 16px;
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-2);
  font-family: var(--font-sans);
  font-size: 0.8rem;
  cursor: pointer;
}

.docs-back-to-top:hover {
  background: var(--surface-2);
  color: var(--text);
}

.docs-back-to-top:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 2px;
}

/* Schmal: Navigation als aufklappbarer Bereich über dem Inhalt,
   Inhaltsverzeichnis nicht mehr seitlich umflossen. */
@media (max-width: 767px) {
  .docs {
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
  }

  .docs-nav-toggle {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    box-sizing: border-box;
    height: 44px;
    padding: 0 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
    color: var(--text);
    font-family: var(--font-sans);
    font-size: 0.9rem;
    cursor: pointer;
  }

  .docs-nav-toggle:focus-visible {
    outline: 2px solid var(--focus);
    outline-offset: 2px;
  }

  .docs-sidebar {
    display: none;
    position: static;
    max-height: none;
    padding: 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
  }

  .docs.is-nav-open .docs-sidebar {
    display: flex;
  }

  .docs-toc ul {
    columns: 1;
  }
}
</style>
