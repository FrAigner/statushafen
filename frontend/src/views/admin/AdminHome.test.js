import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AdminHome from './AdminHome.vue'

// Die Startseite /admin hat keinen eigenen Inhalt; AdminLayout leitet weiter.
describe('AdminHome', () => {
  it('rendert nur einen leeren Platzhalter', () => {
    const wrapper = mount(AdminHome)
    expect(wrapper.text()).toBe('')
  })
})
