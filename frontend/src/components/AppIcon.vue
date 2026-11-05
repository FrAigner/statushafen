<script setup>
// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 16 },
})
// Stryker restore all

// Minimal inline icon set (stroke-based, 24x24 viewBox) replacing emoji glyphs
// used throughout the app. Keep additions primitive (line/circle/rect/polyline)
// rather than hand-rolled bezier paths to avoid malformed glyphs.
</script>

<template>
  <svg
    class="app-icon"
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <!-- sun (light mode) -->
    <template v-if="name === 'sun'">
      <circle cx="12" cy="12" r="4" />
      <line x1="12" y1="2" x2="12" y2="4.5" />
      <line x1="12" y1="19.5" x2="12" y2="22" />
      <line x1="2" y1="12" x2="4.5" y2="12" />
      <line x1="19.5" y1="12" x2="22" y2="12" />
      <line x1="4.9" y1="4.9" x2="6.6" y2="6.6" />
      <line x1="17.4" y1="17.4" x2="19.1" y2="19.1" />
      <line x1="4.9" y1="19.1" x2="6.6" y2="17.4" />
      <line x1="17.4" y1="6.6" x2="19.1" y2="4.9" />
    </template>

    <!-- moon (dark mode) -->
    <template v-else-if="name === 'moon'">
      <path d="M20 14.5a8.5 8.5 0 11-10.5-11 7 7 0 0010.5 11z" />
    </template>

    <!-- search / magnifier -->
    <template v-else-if="name === 'search'">
      <circle cx="10.5" cy="10.5" r="6.5" />
      <line x1="20" y1="20" x2="15.2" y2="15.2" />
    </template>

    <!-- close / x -->
    <template v-else-if="name === 'x'">
      <line x1="5" y1="5" x2="19" y2="19" />
      <line x1="19" y1="5" x2="5" y2="19" />
    </template>

    <!-- pencil / edit -->
    <template v-else-if="name === 'pencil'">
      <path d="M4 16.5V20h3.5L18.5 9 15 5.5 4 16.5z" />
      <line x1="13.2" y1="7.3" x2="16.7" y2="10.8" />
    </template>

    <!-- trash / delete -->
    <template v-else-if="name === 'trash'">
      <line x1="4" y1="7" x2="20" y2="7" />
      <path d="M9 7V4.5h6V7" />
      <path d="M6 7l1 12.5A1.5 1.5 0 008.5 21h7a1.5 1.5 0 001.5-1.5L18 7" />
      <line x1="10" y1="11" x2="10" y2="16.5" />
      <line x1="14" y1="11" x2="14" y2="16.5" />
    </template>

    <!-- mail / email -->
    <template v-else-if="name === 'mail'">
      <rect x="3" y="5.5" width="18" height="13" rx="1.5" />
      <polyline points="3.5,6.5 12,13 20.5,6.5" />
    </template>

    <!-- bell / notifications -->
    <template v-else-if="name === 'bell'">
      <path d="M6 10a6 6 0 0112 0v4.5l1.7 2.5H4.3L6 14.5V10z" />
      <path d="M9.7 19.5a2.3 2.3 0 004.6 0" />
    </template>

    <!-- lock / private -->
    <template v-else-if="name === 'lock'">
      <rect x="5" y="10.5" width="14" height="10" rx="1.5" />
      <path d="M8 10.5V7.5a4 4 0 018 0v3" />
    </template>

    <!-- shield / sso -->
    <template v-else-if="name === 'shield'">
      <path d="M12 3l7 3v5.2c0 4.6-3 8.3-7 9.8-4-1.5-7-5.2-7-9.8V6l7-3z" />
      <polyline points="9,12 11,14 15.5,9.5" />
    </template>

    <!-- user -->
    <template v-else-if="name === 'user'">
      <circle cx="12" cy="8" r="3.3" />
      <path d="M5 20c0-3.5 3.1-6.3 7-6.3s7 2.8 7 6.3" />
    </template>

    <!-- logout / sign-out -->
    <template v-else-if="name === 'logout'">
      <path d="M9 4.5H5.5A1.5 1.5 0 004 6v12a1.5 1.5 0 001.5 1.5H9" />
      <line x1="10.5" y1="12" x2="21" y2="12" />
      <polyline points="17,8 21,12 17,16" />
    </template>

    <!-- users / group -->
    <template v-else-if="name === 'users'">
      <circle cx="9" cy="8.3" r="3" />
      <path d="M3.5 19c0-3 2.5-5.3 5.5-5.3s5.5 2.3 5.5 5.3" />
      <circle cx="17.2" cy="8.8" r="2.4" />
      <path d="M15.3 13.9c2.6.3 4.7 2.4 4.7 5.1" />
    </template>

    <!-- key / roles -->
    <template v-else-if="name === 'key'">
      <circle cx="8" cy="14.5" r="3.5" />
      <line x1="10.5" y1="12" x2="20" y2="2.5" />
      <line x1="15.5" y1="7" x2="18.5" y2="10" />
      <line x1="18" y1="4.5" x2="20.5" y2="7" />
    </template>

    <!-- globe / pages -->
    <template v-else-if="name === 'globe'">
      <circle cx="12" cy="12" r="8.5" />
      <ellipse cx="12" cy="12" rx="3.5" ry="8.5" />
      <line x1="3.5" y1="12" x2="20.5" y2="12" />
    </template>

    <!-- file-text / audit log -->
    <template v-else-if="name === 'file-text'">
      <path d="M6.5 3.5h8l4 4v13a1 1 0 01-1 1h-11a1 1 0 01-1-1v-16a1 1 0 011-1z" />
      <line x1="9" y1="12" x2="15" y2="12" />
      <line x1="9" y1="15.5" x2="15" y2="15.5" />
    </template>

    <!-- chart-bar / overview -->
    <template v-else-if="name === 'chart-bar'">
      <line x1="4" y1="20.5" x2="20" y2="20.5" />
      <rect x="5.5" y="13" width="3.2" height="7.5" />
      <rect x="10.4" y="8.5" width="3.2" height="12" />
      <rect x="15.3" y="4.5" width="3.2" height="16" />
    </template>

    <!-- grid / components -->
    <template v-else-if="name === 'grid'">
      <rect x="3.5" y="3.5" width="7" height="7" rx="1" />
      <rect x="13.5" y="3.5" width="7" height="7" rx="1" />
      <rect x="3.5" y="13.5" width="7" height="7" rx="1" />
      <rect x="13.5" y="13.5" width="7" height="7" rx="1" />
    </template>

    <!-- alert-triangle / incidents & warnings -->
    <template v-else-if="name === 'alert-triangle'">
      <path d="M12 4L2.5 20h19L12 4z" />
      <line x1="12" y1="10" x2="12" y2="14.5" />
      <line x1="12" y1="17" x2="12" y2="17.05" />
    </template>

    <!-- alert-circle -->
    <template v-else-if="name === 'alert-circle'">
      <circle cx="12" cy="12" r="8.5" />
      <line x1="12" y1="7.5" x2="12" y2="12.5" />
      <line x1="12" y1="15.5" x2="12" y2="15.55" />
    </template>

    <!-- info (Hilfe-Tooltips, HelpTip.vue) -->
    <template v-else-if="name === 'info'">
      <circle cx="12" cy="12" r="8.5" />
      <line x1="12" y1="11" x2="12" y2="16.5" />
      <line x1="12" y1="7.5" x2="12" y2="7.55" />
    </template>

    <!-- check-circle / success -->
    <template v-else-if="name === 'check-circle'">
      <circle cx="12" cy="12" r="8.5" />
      <polyline points="8,12.3 10.7,15 15.5,9" />
    </template>

    <!-- chevron-down -->
    <template v-else-if="name === 'chevron-down'">
      <polyline points="5.5,9 12,15.5 18.5,9" />
    </template>

    <!-- chevron-right -->
    <template v-else-if="name === 'chevron-right'">
      <polyline points="9,5.5 15.5,12 9,18.5" />
    </template>

    <!-- more: drei Punkte (Zeilenmenü) -->
    <template v-else-if="name === 'more'">
      <circle cx="5" cy="12" r="1.2" />
      <circle cx="12" cy="12" r="1.2" />
      <circle cx="19" cy="12" r="1.2" />
    </template>

    <!-- arrow-left -->
    <template v-else-if="name === 'arrow-left'">
      <line x1="19" y1="12" x2="5" y2="12" />
      <polyline points="10.5,6 5,12 10.5,18" />
    </template>

    <!-- arrow-right -->
    <template v-else-if="name === 'arrow-right'">
      <line x1="5" y1="12" x2="19" y2="12" />
      <polyline points="13.5,6 19,12 13.5,18" />
    </template>

    <!-- settings / gear -->
    <template v-else-if="name === 'settings'">
      <circle cx="12" cy="12" r="3" />
      <path d="M19 12a7 7 0 00-.14-1.4l2-1.5-2-3.4-2.3.9a7 7 0 00-2.4-1.4L14 3h-4l-.16 2.2a7 7 0 00-2.4 1.4l-2.3-.9-2 3.4 2 1.5A7 7 0 005 12c0 .47.05.94.14 1.4l-2 1.5 2 3.4 2.3-.9a7 7 0 002.4 1.4L10 21h4l.16-2.2a7 7 0 002.4-1.4l2.3.9 2-3.4-2-1.5c.09-.46.14-.93.14-1.4z" />
    </template>

    <!-- folder -->
    <template v-else-if="name === 'folder'">
      <path d="M3.5 6.5a1 1 0 011-1h5l2 2.2h8a1 1 0 011 1V18a1 1 0 01-1 1h-15a1 1 0 01-1-1V6.5z" />
    </template>

    <!-- message-circle -->
    <template v-else-if="name === 'message-circle'">
      <path d="M3.5 12a8 8 0 1113.4 5.9L20 20l-4.6-1a8 8 0 01-11.9-7z" />
    </template>

    <!-- clipboard-list -->
    <template v-else-if="name === 'clipboard'">
      <rect x="5" y="4.5" width="14" height="16" rx="1.5" />
      <rect x="9" y="3" width="6" height="3" rx="1" />
      <line x1="8.5" y1="11" x2="15.5" y2="11" />
      <line x1="8.5" y1="14.5" x2="15.5" y2="14.5" />
    </template>

    <!-- zap / brand mark -->
    <template v-else-if="name === 'zap'">
      <polygon points="13,2.5 5,13.5 11,13.5 10,21.5 19,10 12.7,10" />
    </template>

    <!-- sparkles -->
    <template v-else-if="name === 'sparkles'">
      <path d="M11 3l1.3 3.7L16 8l-3.7 1.3L11 13l-1.3-3.7L6 8l3.7-1.3L11 3z" />
      <path d="M18 14l.8 2.2L21 17l-2.2.8L18 20l-.8-2.2L15 17l2.2-.8L18 14z" />
    </template>

    <!-- rocket -->
    <template v-else-if="name === 'rocket'">
      <path d="M12 2.5c3 1.5 5 4.7 5 8.5 0 2-.6 3.7-1.5 5L12 19l-3.5-3c-.9-1.3-1.5-3-1.5-5 0-3.8 2-7 5-8.5z" />
      <circle cx="12" cy="10" r="1.8" />
      <path d="M9 17l-2.5 4M15 17l2.5 4" />
    </template>

    <!-- building -->
    <template v-else-if="name === 'building'">
      <rect x="5" y="3.5" width="10" height="17" rx="0.5" />
      <line x1="8" y1="7" x2="8" y2="7.05" />
      <line x1="12" y1="7" x2="12" y2="7.05" />
      <line x1="8" y1="10.5" x2="8" y2="10.55" />
      <line x1="12" y1="10.5" x2="12" y2="10.55" />
      <line x1="8" y1="14" x2="8" y2="14.05" />
      <line x1="12" y1="14" x2="12" y2="14.05" />
      <path d="M15 11h4v9.5h-4" />
    </template>

    <!-- google brand mark -->
    <template v-else-if="name === 'google'">
      <path d="M20.5 12.3c0-.7-.06-1.4-.18-2H12v3.8h4.8a4.1 4.1 0 01-1.8 2.7v2.2h2.9c1.7-1.6 2.6-3.9 2.6-6.7z" fill="currentColor" stroke="none" />
      <path d="M12 21c2.4 0 4.5-.8 5.9-2.2l-2.9-2.2c-.8.5-1.8.9-3 .9-2.3 0-4.3-1.6-5-3.7H4v2.3A9 9 0 0012 21z" fill="currentColor" stroke="none" />
      <path d="M7 13.8a5.4 5.4 0 010-3.6V7.9H4a9 9 0 000 8.2l3-2.3z" fill="currentColor" stroke="none" />
      <path d="M12 6.6c1.3 0 2.5.45 3.4 1.3l2.6-2.6A9 9 0 004 7.9l3 2.3c.7-2.1 2.7-3.6 5-3.6z" fill="currentColor" stroke="none" />
    </template>

    <!-- github brand mark -->
    <template v-else-if="name === 'github'">
      <path
        d="M12 2.2a9.8 9.8 0 00-3.1 19.1c.5.1.7-.2.7-.5v-1.8c-2.7.6-3.3-1.3-3.3-1.3-.4-1.1-1-1.4-1-1.4-.9-.6.1-.6.1-.6 1 .1 1.5 1 1.5 1 .9 1.5 2.3 1.1 2.9.8.1-.7.4-1.1.6-1.4-2.2-.2-4.5-1.1-4.5-4.8 0-1 .4-1.9 1-2.6-.1-.2-.4-1.2.1-2.6 0 0 .8-.3 2.7 1a9.4 9.4 0 014.9 0c1.9-1.3 2.7-1 2.7-1 .5 1.4.2 2.4.1 2.6.6.7 1 1.6 1 2.6 0 3.7-2.3 4.6-4.5 4.8.4.3.7.9.7 1.9v2.8c0 .3.2.6.7.5A9.8 9.8 0 0012 2.2z"
        fill="currentColor" stroke="none"
      />
    </template>

    <!-- eye / show password -->
    <template v-else-if="name === 'eye'">
      <path d="M2 12s3.8-7 10-7 10 7 10 7-3.8 7-10 7-10-7-10-7z" />
      <circle cx="12" cy="12" r="3" />
    </template>

    <!-- eye-off / hide password -->
    <template v-else-if="name === 'eye-off'">
      <path d="M3.5 3.5l17 17" />
      <path d="M10.6 5.2A10.6 10.6 0 0112 5c6.2 0 10 7 10 7a17.5 17.5 0 01-4.1 4.9M6.9 6.9C4.4 8.5 2 12 2 12s3.8 7 10 7c1.4 0 2.6-.24 3.7-.66" />
      <path d="M9.6 9.6a3 3 0 004.2 4.2" />
    </template>

    <!-- clock (Wartungsfenster) -->
    <template v-else-if="name === 'clock'">
      <circle cx="12" cy="12" r="9" />
      <polyline points="12 7 12 12 16 14" />
    </template>

    <!-- external-link (Link auf eine fremde Seite) -->
    <template v-else-if="name === 'external-link'">
      <polyline points="14 4 20 4 20 10" />
      <line x1="20" y1="4" x2="11" y2="13" />
      <polyline points="18 14 18 20 4 20 4 6 10 6" />
    </template>

    <!-- activity / monitoring (pulse line) -->
    <template v-else-if="name === 'activity'">
      <polyline points="2.5,13 7,13 9.5,6.5 13.5,19 16.5,10 18.5,13 21.5,13" />
    </template>

    <!-- tag / tag catalog -->
    <template v-else-if="name === 'tag'">
      <path d="M11.5 3.5h6a1 1 0 011 1v6L10 19 3.5 12.5 11.5 3.5z" />
      <circle cx="15" cy="8" r="1.4" fill="currentColor" stroke="none" />
    </template>

    <!-- book / Dokumentation -->
    <template v-else-if="name === 'book'">
      <rect x="5" y="3" width="14" height="18" rx="1.5" />
      <line x1="9" y1="3" x2="9" y2="21" />
      <line x1="12" y1="8" x2="16" y2="8" />
      <line x1="12" y1="12" x2="16" y2="12" />
    </template>

    <!-- fallback: simple dot -->
    <template v-else>
      <circle cx="12" cy="12" r="2" fill="currentColor" stroke="none" />
    </template>
  </svg>
</template>

<style scoped>
.app-icon {
  flex-shrink: 0;
  display: inline-block;
  vertical-align: middle;
}
</style>
