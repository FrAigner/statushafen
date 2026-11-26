import { describe, it, expect } from 'vitest'
import { canSendTestMessage, testMessagePayload } from './testMessageTarget'
import type { TestMessageForm } from './testMessageTarget'

describe('canSendTestMessage', () => {
  it.each([
    ['email', { Email: 'ops@example.com' }, true],
    ['email', { Email: '  ops@example.com ' }, true],
    ['email', { Email: '' }, false],
    ['email', { }, false],
    ['email', { Email: 'ops' }, false],
    ['email', { Email: 'ops@example' }, false],
    ['email', { Email: 'Ops <ops@example.com>' }, false],
    ['google_chat', { WebhookURL: 'https://chat.googleapis.com/v1/spaces/x' }, true],
    ['google_chat', { WebhookURL: 'http://chat.googleapis.com/v1/spaces/x' }, false],
    ['google_chat', { WebhookURL: 'chat.googleapis.com' }, false],
    ['google_chat', { WebhookURL: '' }, false],
    ['google_chat', { }, false],
    ['sms', { Email: 'ops@example.com' }, false],
  ])('%s %o -> %s', (Channel, fields, want) => {
    expect(canSendTestMessage({ Channel, ...fields })).toBe(want)
  })
})

describe('testMessagePayload', () => {
  it('sends only the target of the chosen channel and the admin language', () => {
    // A stale Language on the form must not be sent.
    expect(testMessagePayload({ Channel: 'email', Email: ' a@b.de ', WebhookURL: 'https://x', Language: 'fr' } as TestMessageForm, 'en'))
      .toEqual({ Channel: 'email', Email: 'a@b.de', Language: 'en' })
    expect(testMessagePayload({ Channel: 'google_chat', Email: 'a@b.de', WebhookURL: ' https://x/y ' }, 'de'))
      .toEqual({ Channel: 'google_chat', WebhookURL: 'https://x/y', Language: 'de' })
  })
  it('copes with a missing target field', () => {
    expect(testMessagePayload({ Channel: 'email' }, 'de')).toEqual({ Channel: 'email', Email: '', Language: 'de' })
    expect(testMessagePayload({ Channel: 'google_chat' }, 'en')).toEqual({ Channel: 'google_chat', WebhookURL: '', Language: 'en' })
  })
})
