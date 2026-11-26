import { ref, onMounted, watch } from 'vue'

const theme = ref(localStorage.getItem('theme') || 'light')

export function useTheme() {
  const toggleTheme = () => {
    theme.value = theme.value === 'light' ? 'dark' : 'light'
  }

  const applyTheme = () => {
    document.documentElement.dataset.theme = theme.value
    // sit-onyx-Komponenten (z.B. OnyxButton) reagieren nicht auf unser
    // data-theme-Attribut, sondern standardmäßig auf prefers-color-scheme,
    // erkennen aber zusätzlich eine .dark-Klasse auf <html> (siehe
    // sit-onyx/dist/style.css: ":root { color-scheme: light } .dark {
    // color-scheme: dark }"). Ohne diesen Sync läuft der App-Theme-Toggle
    // aus dem Ruder mit sit-onyx' eigener OS-basierter Dark-Mode-Erkennung -
    // z.B. App im Dark-Mode, aber Browser/OS auf hell eingestellt, lässt
    // OnyxButton dann mit hellen (auf dem dunklen Hintergrund unleserlichen)
    // Farben rendern.
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
