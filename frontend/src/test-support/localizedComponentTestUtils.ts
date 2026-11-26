import { beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import type { Component } from 'vue'
import i18n from '../i18n'

export async function mountLocalizedComponent(Component: Component, props?: Record<string, unknown>): Promise<VueWrapper> {
  const wrapper = mount(Component, {
    global: { plugins: [i18n] },
    props
  })
  await flushPromises()
  return wrapper
}

export function setupLocalizedComponentTest() {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    // Tests assert on the German wording of translated texts;
    // without a stored preference detectInitialLocale() would otherwise fall back
    // to 'en' in the test environment (happy-dom navigator.language === 'en-US').
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })
}
