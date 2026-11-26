import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import App from './App.vue'
import i18n from './i18n'

// The app installs the submit guard for novalidate forms
// (utils/formValidation.js) and removes it again on unmount.

describe('App', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  function submitEmptyRequiredForm() {
    document.body.innerHTML = '<form novalidate><input required></form>'
    const form = document.querySelector('form')
    const onSubmit = vi.fn()
    form.addEventListener('submit', onSubmit)
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    return onSubmit
  }

  it('bindet den gemeinsamen Bestätigungsdialog ein', async () => {
    const wrapper = mount(App, { global: { plugins: [i18n], stubs: { RouterView: true } } })
    expect(wrapper.findComponent({ name: 'ConfirmDialog' }).exists()).toBe(true)
    wrapper.unmount()
  })

  it('stoppt das Absenden ungültiger Formulare, solange die App läuft', () => {
    const wrapper = mount(App, { global: { plugins: [i18n], stubs: { RouterView: true } } })

    expect(submitEmptyRequiredForm()).not.toHaveBeenCalled()

    wrapper.unmount()
    expect(submitEmptyRequiredForm()).toHaveBeenCalledOnce()
  })
})
