<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useApi } from '../../composables/useApi'
import { useLandingChrome } from '../../composables/useLandingChrome'
import { useI18n } from 'vue-i18n'
import AppIcon from '../../components/AppIcon.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import StatusIcon from '../../components/StatusIcon.vue'

const { apiFetch } = useApi()
const { t } = useI18n()
const { isLanding, ordersEnabled } = useLandingChrome()

// Richtung A: PublicLayout zeigt auf der Startseite die volle
// Kopfzeile mit Navigation; sie wird hier angemeldet und beim Verlassen
// zurückgenommen, damit Statusseiten ihren schlichten Kopf behalten.
isLanding.value = true
ordersEnabled.value = true
onBeforeUnmount(() => {
  isLanding.value = false
  ordersEnabled.value = true
})

// Richtung A: feste Beispieldaten für die dekorative Statuskarte im
// Hero (aria-hidden, keine echten Kundendaten). 30 Tage je Komponente als grobe
// Segmente; je ein Teilausfall (Form: Schraffur, Farbe: orange) an fester
// Position, damit die Darstellung stabil bleibt.
const MOCK_DAYS = 30
const MOCK_ROWS = [
  { key: 'mockApi', outliers: {} },
  { key: 'mockWebsite', outliers: { 20: 'partial' } },
  { key: 'mockPayments', outliers: { 9: 'partial' } },
].map(row => ({
  key: row.key,
  bars: Array.from({ length: MOCK_DAYS }, (_, i) => row.outliers[i] ?? 'operational'),
}))

// Funktionsabschnitt: Symbole und Texte aus der Vorlage (landing.featureNTitle/Desc).
const FEATURES = [
  { n: 1, icon: 'grid' },
  { n: 2, icon: 'alert-triangle' },
  { n: 3, icon: 'clock' },
  { n: 4, icon: 'bell' },
  { n: 5, icon: 'globe' },
  { n: 6, icon: 'shield' },
]

const pages = ref([])
const loading = ref(true)

// Anfangsbuchstabe für die Logo-Kachel der Beispielkarte und der Verzeichniskarten.
const initial = name => String(name).trim().charAt(0).toUpperCase()

onMounted(async () => {
  try {
    pages.value = await apiFetch('/api/public/pages')
  } catch (err) {
    console.error("Failed to load public pages", err)
  } finally {
    loading.value = false
  }
  try {
    const flag = await apiFetch('/api/public/orders/enabled')
    ordersEnabled.value = flag.enabled
  } catch (err) {
    console.error("Failed to load orders flag", err)
  }
})
</script>

<template>
  <div class="landing-page">
    <!-- Richtung A: Die Kopfzeile der Startseite liefert PublicLayout
         (useLandingChrome), der Fuß ebenfalls; hier steht der Seiteninhalt.
         Die Abschnitte laufen randlos, der Inhalt sitzt in .lp-in (Breite --lp-max). -->
    <section class="lp-hero" aria-labelledby="hero-title">
      <div class="lp-in lp-hero-in">
        <div class="lp-hero-copy">
          <p class="eyebrow">
            <StatusIcon status="operational" :size="16" />
            {{ t('landing.heroEyebrow') }}
          </p>
          <h1 id="hero-title" class="lp-title">{{ t('landing.heroTitle') }}</h1>
          <p class="lp-lead">{{ t('landing.heroSubtitle') }}</p>
          <div class="lp-cta">
            <router-link v-if="ordersEnabled" to="/order" class="btn btn-primary">
              {{ t('landing.ctaOrder') }}
              <AppIcon name="arrow-right" :size="16" />
            </router-link>
            <a href="#directory" class="btn btn-secondary">{{ t('landing.ctaDirectory') }}</a>
          </div>
        </div>

        <!-- Dekorative Beispielkarte: feste Daten, für Hilfstechnik ausgeblendet. -->
        <div class="lp-hero-visual mock" aria-hidden="true">
          <div class="mock-head">
            <span class="cust-logo">{{ initial(t('landing.mockName')) }}</span>
            <strong>{{ t('landing.mockName') }}</strong>
            <span class="mock-live"><span class="live-dot"></span>{{ t('landing.liveBadge') }}</span>
          </div>
          <StatusBadge class="mock-banner" status="operational" size="md" :label="t('landing.mockBanner')" />
          <div v-for="row in MOCK_ROWS" :key="row.key" class="mock-row">
            <span class="mock-name">{{ t(`landing.${row.key}`) }}</span>
            <div class="bars">
              <span v-for="(level, i) in row.bars" :key="i" class="bar" :class="`bar--${level}`"></span>
            </div>
            <StatusIcon :status="row.bars[MOCK_DAYS - 1]" :size="16" />
          </div>
        </div>
      </div>
    </section>

    <section id="features" class="lp-sec" aria-labelledby="features-title">
      <div class="lp-in">
        <div class="lp-sec-head">
          <h2 id="features-title">{{ t('landing.featuresTitle') }}</h2>
          <p class="lp-sec-sub">{{ t('landing.featuresSubtitle') }}</p>
        </div>
        <div class="feats">
          <div v-for="f in FEATURES" :key="f.n" class="feat">
            <span class="feat-ic"><AppIcon :name="f.icon" :size="20" /></span>
            <h3>{{ t(`landing.feature${f.n}Title`) }}</h3>
            <p>{{ t(`landing.feature${f.n}Desc`) }}</p>
          </div>
        </div>
      </div>
    </section>

    <section id="directory" class="lp-sec lp-sec--alt directory" aria-labelledby="directory-title">
      <div class="lp-in">
        <div class="lp-sec-head">
          <h2 id="directory-title">{{ t('landing.directoryTitle') }}</h2>
          <p class="lp-sec-sub">{{ t('landing.directorySubtitle') }}</p>
        </div>

        <div v-if="loading" class="directory-loading">{{ t('landing.loadingPages') }}</div>

        <div class="page-grid" v-else>
          <router-link v-for="p in pages" :key="p.ID" :to="`/status/${p.Slug}`" class="page-card">
            <div class="page-top">
              <span class="cust-logo" aria-hidden="true">{{ initial(p.Name) }}</span>
              <div class="page-id">
                <h3>{{ p.Name }}</h3>
                <span class="page-slug" :title="`/status/${p.Slug}`">/status/{{ p.Slug }}</span>
              </div>
              <span class="badge">{{ t('landing.liveBadge') }}</span>
            </div>
            <p>{{ p.Description || t('landing.defaultPageDescription') }}</p>
            <span class="page-go">
              {{ t('landing.openPage') }}
              <AppIcon name="arrow-right" :size="14" />
            </span>
          </router-link>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
/* Richtung A, richtung-a.html#landing: Die Abschnitte laufen über die
   volle Fensterbreite (Punkteraster, Trennlinien), der Inhalt sitzt zentriert in
   .lp-in mit der Breite --lp-max. */
.lp-in {
  max-width: var(--lp-max);
  margin: 0 auto;
  padding: 0 var(--sp-5);
}

/* Hero: zweigeteilt, links Text, rechts die Beispielkarte; auf Mobil gestapelt. */
.lp-hero {
  padding: var(--sp-7) 0;
  /* Punkteraster und Unterkante wie in Richtung A. */
  background-image: radial-gradient(var(--border) 1px, transparent 1px);
  background-size: 20px 20px;
  background-position: -10px -10px;
  border-bottom: 1px solid var(--border);
}
.lp-hero-in {
  display: grid;
  /* minmax(0, …): sonst weitet der min-content der Beispielkarte die Spalte
     und die Karte ragt über den Seitenrand hinaus. */
  grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr);
  gap: var(--sp-6);
  align-items: center;
}
.eyebrow {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  padding: 4px 10px 4px 6px;
  border: 1px solid var(--border);
  border-radius: var(--r-pill, 999px);
  background: var(--surface);
  font-size: var(--fs-sm);
  font-weight: 600;
  color: var(--text-2);
  margin-bottom: var(--sp-4);
}
/* Die Breite 13ch hält den Umbruch der Vorlage; Wörter werden nie mitten im
   Wort getrennt (kein overflow-wrap/hyphens): das längste Wort ist kürzer. */
.lp-title {
  max-width: 13ch;
  font-size: var(--fs-3xl);
  font-weight: 800;
  line-height: 1.1;
  letter-spacing: -0.035em;
  color: var(--text);
  text-wrap: balance;
}
.lp-lead {
  font-size: var(--fs-md);
  color: var(--text-2);
  margin-top: var(--sp-4);
  max-width: 560px;
  line-height: 1.6;
}
.lp-cta {
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-3);
  margin-top: var(--sp-5);
}

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--sp-2);
  min-height: 46px;
  box-sizing: border-box;
  padding: var(--sp-2) var(--sp-5);
  border-radius: var(--r-md);
  font-size: var(--fs-md);
  font-weight: 600;
  text-decoration: none;
}

/* Beispielkarte */
.mock {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: var(--sp-4);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-2);
}
.mock-head {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--text);
}
.cust-logo {
  display: inline-grid;
  place-items: center;
  flex: none;
  width: 32px;
  height: 32px;
  border-radius: var(--r-md);
  background: var(--text);
  color: var(--surface);
  font-size: 15px;
  font-weight: 700;
  line-height: 1;
}
.mock-live {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--fs-xs);
  font-weight: 600;
  color: var(--ok-text);
}
.live-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--ok);
  animation: pulse 2s infinite;
}
@keyframes pulse {
  0% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--ok) 60%, transparent); }
  70% { box-shadow: 0 0 0 8px transparent; }
  100% { box-shadow: 0 0 0 0 transparent; }
}
@media (prefers-reduced-motion: reduce) {
  .live-dot { animation: none; }
}
.mock-banner {
  align-self: stretch;
  padding: 10px 12px;
  border-radius: var(--r-md);
  font-weight: 600;
}
.mock-row {
  display: grid;
  grid-template-columns: 120px minmax(0, 1fr) 16px;
  gap: var(--sp-3);
  align-items: center;
  font-size: var(--fs-sm);
  font-weight: 500;
  color: var(--text);
}
.bars {
  display: flex;
  gap: var(--bar-gap);
  height: 20px;
  align-items: stretch;
}
.bar {
  flex: 1;
  min-width: 1px;
  border-radius: var(--bar-r);
  background: var(--ok);
}
/* Teilausfall = Form (Schraffur) + Farbe (orange), nicht nur Farbe. */
.bar--partial {
  background: repeating-linear-gradient(135deg, var(--part) 0 2px, var(--part-soft) 2px 3px);
}

/* Abschnitte */
.lp-sec {
  padding: var(--sp-7) 0;
  /* Der Anker (#features, #directory) landet nicht unter der klebenden Kopfzeile. */
  scroll-margin-top: 64px;
}
.lp-sec--alt {
  background: var(--bg);
  border-top: 1px solid var(--border);
}
.lp-sec-head {
  max-width: 640px;
  margin-bottom: var(--sp-5);
}
.lp-sec-head h2 {
  font-size: var(--fs-2xl);
  font-weight: 650;
  letter-spacing: -0.015em;
  line-height: 1.2;
  margin-bottom: var(--sp-2);
  color: var(--text);
  text-wrap: balance;
}
.lp-sec-sub {
  color: var(--text-2);
}

/* Funktionen: drei Spalten, bei sechs Karten zwei Reihen. */
.feats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--sp-4);
}
.feat {
  background: var(--surface);
  padding: var(--sp-4);
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
}
.feat-ic {
  display: inline-grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: var(--r-md);
  background: var(--primary-soft);
  color: var(--primary-text);
  margin-bottom: var(--sp-3);
}
/* Überschriften-Reihenfolge h1 -> h2 (Abschnitt) -> h3 (Karte). */
.feat h3 {
  font-size: var(--fs-md);
  font-weight: 600;
  line-height: 1.3;
  margin-bottom: var(--sp-1);
  color: var(--text);
}
.feat p {
  color: var(--text-2);
  font-size: var(--fs-sm);
  line-height: 1.55;
}

/* Statusseiten-Verzeichnis */
.directory-loading {
  padding: var(--sp-6);
  color: var(--text-2);
}
.page-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: var(--sp-4);
}
.page-card {
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-1);
  padding: 20px;
  text-decoration: none;
  height: 100%;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.page-card:hover {
  border-color: var(--primary);
  box-shadow: var(--shadow-2);
}
.page-card:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 2px;
}
.page-top {
  display: flex;
  align-items: center;
  gap: 10px;
}
.page-id {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}
.page-id h3 {
  color: var(--text);
  margin: 0;
  font-size: var(--fs-base);
  font-weight: 600;
  overflow-wrap: anywhere;
}
.page-slug {
  font-family: var(--font-mono);
  font-size: var(--fs-xs);
  color: var(--text-3);
  /* eine Zeile, bei Platzmangel mit Auslassung (voller Slug im
     title) statt mitten im Wort umzubrechen; Karten bleiben gleich hoch. */
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
/* Status = Form + Farbe + Text: Punkt, grüne Fläche und das Wort „Live“. */
.badge {
  margin-left: auto;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--sp-1);
  background: var(--ok-soft);
  color: var(--ok-text); /* 4,5:1 auf dem Grünton */
  font-size: var(--fs-xs);
  font-weight: 700;
  padding: var(--sp-1) var(--sp-2);
  border-radius: var(--r-sm);
  text-transform: none;
  letter-spacing: 0;
}
.badge::before {
  content: '';
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--ok);
}
.page-card p {
  color: var(--text-2);
  font-size: var(--fs-sm);
  line-height: 1.5;
  flex-grow: 1;
}
.page-go {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--fs-sm);
  font-weight: 600;
  color: var(--primary-text);
}

@media (max-width: 900px) {
  .feats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .lp-in {
    padding: 0 var(--sp-4);
  }
  .lp-hero {
    padding: var(--sp-5) 0;
  }
  .lp-hero-in {
    grid-template-columns: 1fr;
    gap: var(--sp-5);
  }
  .lp-sec {
    padding: var(--sp-6) 0;
  }
  .feats {
    grid-template-columns: 1fr;
  }
  .lp-cta .btn {
    width: 100%;
  }
  .mock-row {
    grid-template-columns: 90px minmax(0, 1fr) 16px;
  }
}
</style>
