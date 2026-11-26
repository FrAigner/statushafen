import { describe, it, expect } from 'vitest'
import { languageOptions } from './notificationTemplateOptions'

describe('languageOptions', () => {
  it('zeigt nur den Sprachnamen ohne erklärenden Klammerzusatz', () => {
    expect(languageOptions).toEqual([
      { label: 'Englisch', value: 'en' },
      { label: 'Deutsch', value: 'de' },
    ])
  })

  it('enthält keinen Verweis auf "interne User" oder "Subscriber" mehr', () => {
    for (const option of languageOptions) {
      expect(option.label).not.toMatch(/interne User|Subscriber/)
    }
  })
})
