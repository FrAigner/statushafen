import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuth } from './useAuth'
import { useApi } from './useApi'

// Switch the help tooltips in the admin area on and off per user. Stored on
// the server at the user (PATCH /api/me {"help_tooltips": bool}, output in
// /api/me, the login response and the SSO token), so the choice applies on
// all devices. Default is on: if the field is missing (old client state, test
// setups), the tooltips count as switched on.
//
// The switch takes effect immediately via currentUser (local state); the
// server's response is not awaited. If saving fails, the choice is kept for
// this session and saveError names the problem (as with the language choice,
// useLanguage.ts).

let auth: ReturnType<typeof useAuth> | null = null
const user = () => (auth ??= useAuth()).currentUser

export const helpTooltipsEnabled = computed(() => user().value?.help_tooltips !== false)

export function useHelpTooltips() {
  const { t } = useI18n()
  const { apiFetch } = useApi()
  const saveError = ref('')

  const setHelpTooltips = async (on: boolean) => {
    const current = user()
    if (!current.value) return
    current.value = { ...current.value, help_tooltips: on }
    saveError.value = ''
    try {
      await apiFetch('/api/me', { method: 'PATCH', body: JSON.stringify({ help_tooltips: on }) })
    } catch (err) {
      console.error('Konnte Hilfe-Einstellung nicht speichern:', err)
      saveError.value = t('helpTooltips.saveError')
    }
  }

  return { enabled: helpTooltipsEnabled, setHelpTooltips, saveError }
}
