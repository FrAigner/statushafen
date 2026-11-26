import { ref, onMounted, watch } from 'vue'

const theme = ref(localStorage.getItem('theme') || 'light')

export function useTheme() {
  const toggleTheme = () => {
    theme.value = theme.value === 'light' ? 'dark' : 'light'
  }

  const applyTheme = () => {
    document.documentElement.dataset.theme = theme.value
    // sit-onyx components (e.g. OnyxButton) do not react to our data-theme
    // attribute but to prefers-color-scheme by default; they additionally
    // recognize a .dark class on <html> (see sit-onyx/dist/style.css:
    // ":root { color-scheme: light } .dark { color-scheme: dark }"). Without
    // this sync the app's theme toggle gets out of step with sit-onyx's own
    // OS-based dark mode detection: e.g. the app in dark mode while the
    // browser/OS is set to light makes OnyxButton render with light colors
    // (unreadable on the dark background).
    document.documentElement.classList.toggle('dark', theme.value === 'dark')
    localStorage.setItem('theme', theme.value)
  }

  onMounted(() => {
    applyTheme()
  })

  watch(theme, () => {
    applyTheme()
  })

  return { theme, toggleTheme }
}
