import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import FormField from './FormField.vue'
import { axe } from '../test-support/axe.js'

let wrapper = null

// Feld mit Slot-Props, wie es Views verwenden.
function mountField(props = {}) {
  wrapper = mount(FormField, {
    props: { label: 'Name', ...props },
    slots: {
      default: `<template #default="f">
        <input :id="f.id" :aria-describedby="f.describedby" :aria-invalid="f.invalid" :required="f.required">
      </template>`,
    },
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
  return wrapper
}

beforeEach(() => {
  i18n.global.locale.value = 'de'
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('FormField', () => {
  it('verbindet Label und Feld über die erzeugte ID', () => {
    const w = mountField()
    const id = w.find('input').attributes('id')
    expect(id).toBeTruthy()
    expect(w.find('label').attributes('for')).toBe(id)
    expect(w.find('label').text()).toBe('Name')
  })

  it('übernimmt eine vorgegebene Feld-ID (fieldId)', () => {
    const w = mountField({ fieldId: 'meine-id' })
    expect(w.find('input').attributes('id')).toBe('meine-id')
    expect(w.find('label').attributes('for')).toBe('meine-id')
  })

  it('zeigt bei Pflichtfeld den Stern (für Screenreader versteckt) und setzt required am Feld', () => {
    const w = mountField({ required: true })
    const star = w.find('.form-field__required')
    expect(star.text()).toBe('*')
    expect(star.attributes('aria-hidden')).toBe('true')
    expect(w.find('.form-field__optional').exists()).toBe(false)
    expect(w.find('input').attributes('required')).toBeDefined()
    expect(w.find('label').text()).not.toContain('*')
  })

  it('zeigt bei freiwilligem Feld „optional“ statt des Sterns', () => {
    const w = mountField()
    expect(w.find('.form-field__optional').text()).toBe('optional')
    expect(w.find('.form-field__required').exists()).toBe(false)
    expect(w.find('input').attributes('required')).toBeUndefined()
  })

  it('ohne Hinweis und Fehler gibt es weder Meldungszeile noch aria-describedby', () => {
    const w = mountField()
    expect(w.find('.form-field__message').exists()).toBe(false)
    expect(w.find('input').attributes('aria-describedby')).toBeUndefined()
    expect(w.find('input').attributes('aria-invalid')).toBeUndefined()
  })

  it('verbindet den Hinweis per aria-describedby mit dem Feld', () => {
    const w = mountField({ hint: 'Wird öffentlich angezeigt.' })
    const hint = w.find('.form-field__message')
    expect(hint.text()).toBe('Wird öffentlich angezeigt.')
    expect(hint.attributes('role')).toBeUndefined()
    expect(w.find('input').attributes('aria-describedby')).toBe(hint.attributes('id'))
    expect(w.find('input').attributes('aria-invalid')).toBeUndefined()
  })

  it('Fehler ersetzt den Hinweis, ist ein Alarm und setzt aria-invalid', () => {
    const w = mountField({ hint: 'Wird öffentlich angezeigt.', error: 'Name ist erforderlich.' })
    const messages = w.findAll('.form-field__message')
    expect(messages).toHaveLength(1)
    expect(messages[0].classes()).toContain('form-field__message--error')
    expect(messages[0].attributes('role')).toBe('alert')
    expect(messages[0].text()).toBe('Name ist erforderlich.')
    expect(w.find('input').attributes('aria-describedby')).toBe(messages[0].attributes('id'))
    expect(w.find('input').attributes('aria-invalid')).toBe('true')
    expect(w.classes()).toContain('form-field--invalid')
  })

  it('mehrere Felder in einer Seite bekommen verschiedene IDs', () => {
    const Page = {
      components: { FormField },
      template: `<div>
        <FormField label="A" hint="a" v-slot="f"><input :id="f.id" :aria-describedby="f.describedby"></FormField>
        <FormField label="B" hint="b" v-slot="f"><input :id="f.id" :aria-describedby="f.describedby"></FormField>
      </div>`,
    }
    wrapper = mount(Page, { attachTo: document.body, global: { plugins: [i18n] } })
    const ids = wrapper.findAll('input').map(i => i.attributes('id'))
    const hints = wrapper.findAll('.form-field__message').map(m => m.attributes('id'))
    expect(new Set(ids).size).toBe(2)
    expect(new Set(hints).size).toBe(2)
  })

  it('hängt die Hilfe (v-help) an das Feld: Verweis auf den Tooltip neben dem Hinweis', () => {
    const w = mountField({ help: 'components.name', hint: 'Hinweis' })
    const describedby = w.find('input').attributes('aria-describedby').split(' ')
    expect(describedby).toHaveLength(2)
    expect(describedby).toContain(w.find('.form-field__message').attributes('id'))
    expect(describedby).toContain('help-components-name')
  })

  it('ohne help hängt kein Tooltip am Feld', () => {
    const w = mountField({ hint: 'Hinweis' })
    expect(w.find('input').attributes('aria-describedby')).not.toContain('help-')
  })

  it('hat keine axe-Funde (frei, Pflicht mit Hinweis, Fehler)', async () => {
    expect(await axe(mountField())).toHaveNoViolations()
    wrapper.unmount()
    expect(await axe(mountField({ required: true, hint: 'Hinweis', help: 'components.name' }))).toHaveNoViolations()
    wrapper.unmount()
    expect(await axe(mountField({ required: true, error: 'Fehler' }))).toHaveNoViolations()
  })
})
