import { describe, it, expect, vi, afterEach } from 'vitest'
import { vueSources, templatePart, startTags } from '../test-support/templateScan.js'
import { installFormValidationGuard } from './formValidation.js'

// Der Screenshot zeigte „Fülle dieses Feld aus.“ schon beim Hover
// über ein leeres Pflichtfeld, mitten in den neuen Hilfe-Tooltips. Ursache:
// Chromium und Firefox zeigen bei einem ungültigen Formularfeld ohne
// title-Attribut die native Validierungsmeldung als Hover-Tooltip an (Blink:
// InputType::DefaultToolTip liefert ValidationMessage(), außer das Formular
// hat `novalidate`). sit-onyx setzt an seinen Inputs kein title (nur bei
// hideLabel), also greift der Browser-Default. Ein leeres title="" hilft in
// Chromium nicht, weil ein leerer Titel dort wieder auf den Default fällt.
//
// Lösung an der Wurzel: Jedes <form> bekommt `novalidate` (das schaltet den
// Hover-Hinweis ab), und ein globaler Submit-Wächter (App.vue) holt die
// Pflichtfeld-Prüfung beim Absenden zurück: checkValidity(), bei Fehlern
// reportValidity() und das Submit-Event stoppen, bevor der @submit-Handler
// der View läuft.

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
