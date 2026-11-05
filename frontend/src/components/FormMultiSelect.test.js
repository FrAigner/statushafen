import { describe, it, expect, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import i18n from '../i18n'
import FormMultiSelect from './FormMultiSelect.vue'
import { useFormPanel } from '../composables/useFormPanel'
import { axe } from '../test-support/axe.js'

// Bewusst das ECHTE OnyxSelect (kein Stub): frühere Fehler betrafen genau das,
// was Onyx am Eingabefeld rendert (id, aria-invalid, aria-describedby).
const options = [{ value: '1', label: 'API' }, { value: '2', label: 'Web' }]

let wrapper = null
afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

function mountField(props = {}) {
  wrapper = mount(FormMultiSelect, {
    props: { label: 'Betroffene Komponente(n)', options, modelValue: [], required: true, ...props },
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
  return wrapper
}

const input = () => wrapper.find('input[role="combobox"]')

describe('FormMultiSelect', () => {
  // Regression: `label for` zeigte ins Leere, weil die Feld-ID nie am
  // Eingabefeld von OnyxSelect ankam.
  it('verknüpft das Label mit dem Eingabefeld der Mehrfachauswahl', () => {
    mountField()
    const label = wrapper.find('label.form-field__label')
    expect(label.text()).toBe('Betroffene Komponente(n)')
    const target = document.getElementById(label.attributes('for'))
    expect(target).not.toBeNull()
    expect(target.tagName).toBe('INPUT')
    expect(target.getAttribute('role')).toBe('combobox')
  })

  // Regression (Sichtprüfung): Onyx-`required` blendete beim Fokus einen
  // englischen Tooltip „Please fill out this field.“ ein.
  it('kennzeichnet Pflicht per aria-required, ohne Onyx-required (kein englischer Tooltip)', async () => {
    mountField({ error: 'Fehler' })
    await flushPromises()
    expect(input().attributes('aria-required')).toBe('true')
    expect(input().attributes('required')).toBeUndefined()
    await wrapper.setProps({ required: false })
    await flushPromises()
    expect(input().attributes('aria-required')).toBeUndefined()
  })

  it('setzt ohne Fehler weder aria-invalid noch aria-describedby', () => {
    mountField()
    expect(input().attributes('aria-invalid')).toBeUndefined()
    expect(input().attributes('aria-describedby')).toBeUndefined()
  })

  // Regression: Der Pflichtfeld-Fehler stand nicht am Feld.
  it('zeigt den Fehler einmal in der FormField-Zeile und setzt aria-invalid/-describedby am Eingabefeld', async () => {
    mountField({ error: 'Komponente ist ein Pflichtfeld.' })
    await flushPromises()
    const messages = wrapper.findAll('.form-field__message--error')
    expect(messages).toHaveLength(1)
    expect(messages[0].attributes('role')).toBe('alert')
    expect(input().attributes('aria-invalid')).toBe('true')
    expect(input().attributes('aria-describedby')).toBe(messages[0].attributes('id'))
    expect(wrapper.find('.onyx-form-element-v2__message').exists()).toBe(false)
  })

  it('nimmt aria-invalid zurück und wechselt beim Hinweis auf dessen ID, sobald der Fehler weg ist', async () => {
    mountField({ error: 'Fehler', hint: 'Mehrere möglich' })
    await flushPromises()
    await wrapper.setProps({ error: null })
    await flushPromises()
    expect(input().attributes('aria-invalid')).toBeUndefined()
    expect(input().attributes('aria-describedby')).toBe(wrapper.find('.form-field__message').attributes('id'))
    await wrapper.setProps({ hint: null })
    await flushPromises()
    expect(input().attributes('aria-describedby')).toBeUndefined()
  })

  it('reicht die Auswahl per v-model weiter', async () => {
    mountField()
    wrapper.findComponent({ name: 'OnyxSelect' }).vm.$emit('update:modelValue', ['2'])
    expect(wrapper.emitted('update:modelValue')).toEqual([[['2']]])
  })

  it('ist barrierefrei (axe), auch mit Fehler', async () => {
    mountField({ error: 'Pflichtfeld' })
    await flushPromises()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // useFormPanel.focusInvalid() springt aufs Eingabefeld der Auswahl.
  it('erhält den Fokus über useFormPanel.validateRequired, wenn keine Komponente gewählt ist', async () => {
    const Host = defineComponent({
      setup() {
        const panel = useFormPanel()
        const ids = ref([])
        return { ...panel, ids }
      },
      render() {
        return h('form', { ref: 'formEl' }, [
          h(FormMultiSelect, {
            modelValue: this.ids, options, required: true, label: 'Komponenten',
            error: this.errors.ComponentIDs || null,
          }),
        ])
      },
    })
    wrapper = mount(Host, { attachTo: document.body, global: { plugins: [i18n] } })
    wrapper.vm.formEl = wrapper.element
    const ok = await wrapper.vm.validateRequired([{ key: 'ComponentIDs', value: wrapper.vm.ids, message: 'Pflicht' }])
    await flushPromises()
    expect(ok).toBe(false)
    expect(document.activeElement).toBe(wrapper.find('input[role="combobox"]').element)
  })
})
