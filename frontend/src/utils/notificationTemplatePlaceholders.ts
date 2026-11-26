// The only data source of the placeholders for notification templates.
// The collapsible "Available placeholders" list and the autocompletion when
// typing `{` in AdminNotificationTemplates.vue both read from here, so that a
// new placeholder only has to be added in one place. `name` is the field name
// in the Go template (`{{.Name}}`), `descKey` the i18n key of the description.
export const NOTIFICATION_TEMPLATE_PLACEHOLDERS = [
  { name: 'Title', descKey: 'notificationTemplates.placeholders.title' },
  { name: 'IncidentTitle', descKey: 'notificationTemplates.placeholders.incidentTitleAlias' },
  { name: 'Level', descKey: 'notificationTemplates.placeholders.level' },
  { name: 'Message', descKey: 'notificationTemplates.placeholders.message' },
  // Formatted message as finished, sanitized mail HTML.
  { name: 'MessageHTML', descKey: 'notificationTemplates.placeholders.messageHtml' },
  { name: 'Status', descKey: 'notificationTemplates.placeholders.status' },
  { name: 'StatusLabel', descKey: 'notificationTemplates.placeholders.statusLabel' },
  { name: 'ComponentNames', descKey: 'notificationTemplates.placeholders.componentNames' },
  { name: 'Tags', descKey: 'notificationTemplates.placeholders.tags' },
  { name: 'Action', descKey: 'notificationTemplates.placeholders.action' },
  { name: 'Language', descKey: 'notificationTemplates.placeholders.language' },
  { name: 'FrontendURL', descKey: 'notificationTemplates.placeholders.frontendUrl' },
  { name: 'UnsubscribeURL', descKey: 'notificationTemplates.placeholders.unsubscribeUrl' },
  // Only set in the double opt-in confirmation mail.
  { name: 'ConfirmURL', descKey: 'subscribeConfirm.templatePlaceholderConfirmUrl' },
  // Notifications of external sources: only set there.
  { name: 'SourceName', descKey: 'notificationTemplates.placeholders.sourceName' },
  { name: 'SourceURL', descKey: 'notificationTemplates.placeholders.sourceUrl' },
  { name: 'FromAddress', descKey: 'notificationTemplates.placeholders.fromAddress' },
]
