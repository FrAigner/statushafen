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

// sit-onyx's own strings (validation messages, breadcrumb labels etc.)
// would otherwise always follow 'en-US', regardless of the app language.
const ONYX_LOCALE_MAP = { de: 'de-DE', en: 'en-US' }
app.use(createOnyx({
  i18n: {
    locale: computed(() => ONYX_LOCALE_MAP[i18n.global.locale.value] ?? 'en-US'),
    messages: { 'de-DE': deDE },
  },
}))
app.use(router)
app.use(i18n)

// Load the global UI text overrides before the first render, so that
// customized texts appear immediately instead of only after a brief flicker
// of the bundle default. If the request fails, the app still mounts with the
// bundle defaults (see applyGlobalOverrides).
await applyGlobalOverrides(i18n.global.locale.value).catch(() => {})
app.mount('#app')
