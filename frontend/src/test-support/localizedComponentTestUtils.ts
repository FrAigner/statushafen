import { beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../i18n'

export async function mountLocalizedComponent(Component, props) {
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
    // Tests assertieren auf den deutschen Wortlaut übersetzter Texte;
    // ohne gespeicherte Präferenz würde detectInitialLocale() in der Testumgebung
    // (happy-dom navigator.language === 'en-US') sonst auf 'en' fallen.
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })
}
