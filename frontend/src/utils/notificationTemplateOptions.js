// Sprachoptionen für die Benachrichtigungs-Templates: nur der
// Sprachname, ohne erklärenden Klammerzusatz. Dient als Fallback, solange
// für eine Statusseite noch kein echter Sprachkatalog konfiguriert ist
// - AdminNotificationTemplates.vue zieht die tatsächlich
// aktivierten Sprachen stattdessen dynamisch aus useContentLanguages.
export const languageOptions = [
  { label: 'Englisch', value: 'en' },
  { label: 'Deutsch', value: 'de' },
]

// Audience-Optionen (Stage 5 der Mehrsprachigkeit): lösen die
// vorherige Konvention auf, bei der die Zielgruppe implizit über
// Language=en ("staff") bzw. Language=de ("subscriber") codiert war.
export const audienceOptions = [
  { label: 'Intern (Staff)', value: 'staff' },
  { label: 'Abonnenten (Subscriber)', value: 'subscriber' },
]
