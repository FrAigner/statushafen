// Language options for the notification templates: only the language name,
// without an explanatory suffix in parentheses. Serves as the fallback while
// a status page has no real language catalogue configured yet
// - AdminNotificationTemplates.vue takes the actually enabled
// languages dynamically from useContentLanguages instead.
export const languageOptions = [
  { label: 'Englisch', value: 'en' },
  { label: 'Deutsch', value: 'de' },
]

// Audience options (stage 5 of multilingual support): they replace the
// previous convention where the audience was implicitly encoded via
// Language=en ("staff") or Language=de ("subscriber").
export const audienceOptions = [
  { label: 'Intern (Staff)', value: 'staff' },
  { label: 'Abonnenten (Subscriber)', value: 'subscriber' },
]
