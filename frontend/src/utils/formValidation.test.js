import { describe, it, expect, vi, afterEach } from 'vitest'
import { vueSources, templatePart, startTags } from '../test-support/templateScan'
import { installFormValidationGuard } from './formValidation'

// The screenshot showed "Fülle dieses Feld aus." already on hover
// over an empty required field, in the middle of the new help tooltips. Cause:
// for an invalid form field without a title attribute, Chromium and Firefox show
// the native validation message as a hover tooltip (Blink:
// InputType::DefaultToolTip returns ValidationMessage(), unless the form
// has `novalidate`). sit-onyx sets no title on its inputs (only with
// hideLabel), so the browser default applies. An empty title="" does not help in
// Chromium, because an empty title falls back to the default there.
//
// Fix at the root: every <form> gets `novalidate` (that switches off the
// hover hint), and a global submit guard (App.vue) brings back the
// required-field check on submit: checkValidity(), and on errors
// reportValidity() and stopping the submit event before the view's @submit
// handler runs.

describe('Formulare ohne nativen Hover-Hinweis', () => {
  it('jedes <form> im Frontend hat novalidate', () => {
    const forms = []
    for (const [file, src] of Object.entries(vueSources)) {
      for (const tag of startTags(templatePart(src))) {
        if (tag.name === 'form') forms.push({ file, novalidate: 'novalidate' in tag.attrs })
      }
    }
    expect(forms.length).toBeGreaterThan(20)
    expect(forms.filter(f => !f.novalidate).map(f => f.file)).toEqual([])
  })
})

describe('installFormValidationGuard', () => {
  let uninstall = () => {}
  afterEach(() => {
    uninstall()
    document.body.innerHTML = ''
  })

  function setup({ value = '', noValidate = true } = {}) {
    document.body.innerHTML = `<form><input name="n" required value="${value}"><button type="submit">ok</button></form>`
    const form = document.querySelector('form')
    form.noValidate = noValidate
    const onSubmit = vi.fn(e => e.preventDefault())
    form.addEventListener('submit', onSubmit)
    uninstall = installFormValidationGuard(document)
    return { form, onSubmit }
  }

  function submit(form) {
    const event = new Event('submit', { bubbles: true, cancelable: true })
    form.dispatchEvent(event)
    return event
  }

  it('stoppt das Absenden bei leerem Pflichtfeld und meldet den Fehler', () => {
    const { form, onSubmit } = setup()
    const report = vi.spyOn(form, 'reportValidity')

    const event = submit(form)

    expect(onSubmit).not.toHaveBeenCalled()
    expect(event.defaultPrevented).toBe(true)
    expect(report).toHaveBeenCalledOnce()
  })

  it('lässt ein gültiges Formular zum Handler durch', () => {
    const { form, onSubmit } = setup({ value: 'API' })
    const report = vi.spyOn(form, 'reportValidity')

    submit(form)

    expect(onSubmit).toHaveBeenCalledOnce()
    expect(report).not.toHaveBeenCalled()
  })

  it('greift nicht bei Formularen ohne novalidate (dort prüft der Browser selbst)', () => {
    const { form, onSubmit } = setup({ noValidate: false })
    const check = vi.spyOn(form, 'checkValidity')

    submit(form)

    expect(check).not.toHaveBeenCalled()
    expect(onSubmit).toHaveBeenCalledOnce()
  })

  it('greift nicht bei Formularen mit eigener Prüfung (data-self-validates)', () => {
    const { form, onSubmit } = setup()
    form.dataset.selfValidates = ''
    const check = vi.spyOn(form, 'checkValidity')

    submit(form)

    expect(check).not.toHaveBeenCalled()
    expect(onSubmit).toHaveBeenCalledOnce()
  })

  it('lässt sich wieder entfernen', () => {
    const { form, onSubmit } = setup()
    uninstall()

    submit(form)

    expect(onSubmit).toHaveBeenCalledOnce()
  })

  it('installiert sich ohne Argument am document', () => {
    const { form, onSubmit } = setup()
    uninstall()
    uninstall = installFormValidationGuard()

    submit(form)

    expect(onSubmit).not.toHaveBeenCalled()
  })
})
