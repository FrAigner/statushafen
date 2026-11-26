// einzige Datenquelle der Platzhalter für Benachrichtigungs-
// Templates. Die aufklappbare Liste „Verfügbare Platzhalter“ und die
// Autovervollständigung beim Tippen von `{` in AdminNotificationTemplates.vue
// lesen beide hieraus, damit ein neuer Platzhalter nur an einer Stelle
// ergänzt werden muss. `name` ist der Feldname im Go-Template
// (`{{.Name}}`), `descKey` der i18n-Schlüssel der Beschreibung.
export const NOTIFICATION_TEMPLATE_PLACEHOLDERS = [
  { name: 'Title', descKey: 'notificationTemplates.placeholders.title' },
  { name: 'IncidentTitle', descKey: 'notificationTemplates.placeholders.incidentTitleAlias' },
  { name: 'Level', descKey: 'notificationTemplates.placeholders.level' },
  { name: 'Message', descKey: 'notificationTemplates.placeholders.message' },
  // formatierte Nachricht als fertiges, bereinigtes Mail-HTML.
  { name: 'MessageHTML', descKey: 'notificationTemplates.placeholders.messageHtml' },
  { name: 'Status', descKey: 'notificationTemplates.placeholders.status' },
  { name: 'StatusLabel', descKey: 'notificationTemplates.placeholders.statusLabel' },
  { name: 'ComponentNames', descKey: 'notificationTemplates.placeholders.componentNames' },
  { name: 'Tags', descKey: 'notificationTemplates.placeholders.tags' },
  { name: 'Action', descKey: 'notificationTemplates.placeholders.action' },
  { name: 'Language', descKey: 'notificationTemplates.placeholders.language' },
  { name: 'FrontendURL', descKey: 'notificationTemplates.placeholders.frontendUrl' },
  { name: 'UnsubscribeURL', descKey: 'notificationTemplates.placeholders.unsubscribeUrl' },
  // nur in der Bestätigungsmail des Double-Opt-in belegt.
  { name: 'ConfirmURL', descKey: 'subscribeConfirm.templatePlaceholderConfirmUrl' },
  // Meldungen externer Quellen: nur dort belegt.
  { name: 'SourceName', descKey: 'notificationTemplates.placeholders.sourceName' },
  { name: 'SourceURL', descKey: 'notificationTemplates.placeholders.sourceUrl' },
  { name: 'FromAddress', descKey: 'notificationTemplates.placeholders.fromAddress' },
]
