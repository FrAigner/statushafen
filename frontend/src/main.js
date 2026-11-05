import { createApp, computed } from 'vue'
import { createOnyx } from 'sit-onyx'
import deDE from 'sit-onyx/locales/de-DE.json'
import './assets/tokens.css'
import 'sit-onyx/style.css'
import './style.css'
import './admin.css'
import App from './App.vue'
import router from './router'
import i18n from './i18n'
import { applyGlobalOverrides } from './i18n/overrides'

const app = createApp(App)

app.config.errorHandler = (err, instance, info) => {
  console.error('VUE GLOBAL ERROR:', err, 'INFO:', info)
}

// sit-onyx-eigene Strings (Validierungsmeldungen, Breadcrumb-Labels etc.)
// folgen sonst immer 'en-US', unabhängig von der App-Sprache.
const ONYX_LOCALE_MAP = { de: 'de-DE', en: 'en-US' }
app.use(createOnyx({
  i18n: {
    locale: computed(() => ONYX_LOCALE_MAP[i18n.global.locale.value] ?? 'en-US'),
    messages: { 'de-DE': deDE },
  },
}))
app.use(router)
app.use(i18n)

// Globale UI-Text-Overrides vor dem ersten Render
// laden, damit angepasste Texte sofort erscheinen statt erst nach einem
// kurzen Flackern des Bundle-Defaults. Schlägt der Request fehl, mountet die
// App trotzdem mit den Bundle-Defaults (siehe applyGlobalOverrides).
await applyGlobalOverrides(i18n.global.locale.value).catch(() => {})
app.mount('#app')
