import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AdminHome from './AdminHome.vue'

// The start page /admin has no content of its own; AdminLayout redirects.
describe('AdminHome', () => {
  it('rendert nur einen leeren Platzhalter', () => {
    const wrapper = mount(AdminHome)
    expect(wrapper.text()).toBe('')
  })
})
