// Client-side pre-check for the "send test message" button in the subscriber
// form. The backend validates again (SSRF checks, bare address only).

const BARE_EMAIL = /^[^\s@<>"]+@[^\s@<>"]+\.[^\s@<>"]+$/

function isHttpsUrl(raw) {
  try {
    const u = new URL(raw)
    return u.protocol === 'https:' && u.hostname !== ''
  } catch {
    return false
  }
}

export function canSendTestMessage(form) {
  if (form.Channel === 'email') return BARE_EMAIL.test((form.Email || '').trim())
  if (form.Channel === 'google_chat') return isHttpsUrl((form.WebhookURL || '').trim())
  return false
}

// testMessagePayload sends only the target of the chosen channel; language is
// the admin UI language, not the subscriber's mail language.
export function testMessagePayload(form, language) {
  const payload = { Channel: form.Channel }
  if (form.Channel === 'email') payload.Email = (form.Email || '').trim()
  else payload.WebhookURL = (form.WebhookURL || '').trim()
  payload.Language = language
  return payload
}
