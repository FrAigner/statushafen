import { describe, it, expect, vi } from 'vitest'
import LocalizedComponent from './LocalizedComponent.vue'
import { mountLocalizedComponent, setupLocalizedComponentTest } from '../test-support/localizedComponentTestUtils'
import { axe } from '../test-support/axe.js'

vi.mock('../composables/useApi', () => ({
  useApi: () => ({
    apiFetch: vi.fn(async () => [])
  })
}))

vi.mock('../composables/useLanguage', () => ({
  useLanguage: () => ({
    currentLanguageCode: { value: 'de' }
  })
}))

function componentFixture() {
  return {
    ID: 1,
    Name: 'Database',
    Description: 'Main database server'
  }
}

async function mountComponent({ component = componentFixture() } = {}) {
  return { wrapper: await mountLocalizedComponent(LocalizedComponent, { component }) }
}

describe('LocalizedComponent', () => {
  setupLocalizedComponentTest()

  it('displays component title', async () => {
    const component = componentFixture()
    const { wrapper } = await mountComponent({ component })
    expect(wrapper.text()).toContain(component.Name)
  })

  it('displays component description', async () => {
    const component = componentFixture()
    const { wrapper } = await mountComponent({ component })
    expect(wrapper.text()).toContain(component.Description)
  })

  it('handles missing description gracefully', async () => {
    const component = { ...componentFixture(), Description: null }
    const { wrapper } = await mountComponent({ component })
    expect(wrapper.text()).toContain(component.Name)
  })

  it('handles empty description', async () => {
    const component = { ...componentFixture(), Description: '' }
    const { wrapper } = await mountComponent({ component })
    expect(wrapper.text()).toContain(component.Name)
  })

  it('renders component structure', async () => {
    const component = componentFixture()
    const { wrapper } = await mountComponent({ component })
    const title = wrapper.find('h3')
    expect(title.exists()).toBe(true)
  })

  it('renders description paragraph when available', async () => {
    const component = componentFixture()
    const { wrapper } = await mountComponent({ component })
    const desc = wrapper.find('.component-description')
    expect(desc.exists()).toBe(true)
    expect(desc.text()).toBe(component.Description)
  })

  it('hides description when not available', async () => {
    const component = { ...componentFixture(), Description: '' }
    const { wrapper } = await mountComponent({ component })
    const desc = wrapper.find('.component-description')
    expect(desc.exists()).toBe(false)
  })

  it('handles very long component names', async () => {
    const longName = 'A'.repeat(200)
    const component = { ...componentFixture(), Name: longName }
    const { wrapper } = await mountComponent({ component })
    expect(wrapper.text()).toContain(longName)
  })

  it('applies correct CSS classes', async () => {
    const component = componentFixture()
    const { wrapper } = await mountComponent({ component })
    const root = wrapper.find('.component')
    expect(root.classes()).toContain('component')
  })

  it('hat keine axe-Funde', async () => {
    const { wrapper } = await mountComponent()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
