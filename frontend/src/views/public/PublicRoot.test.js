import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import PublicRoot from './PublicRoot.vue'

const LandingStub = { template: '<div class="landing-stub" />' }
const StatusPageStub = { props: ['slug'], template: '<div class="status-page-stub">{{ slug }}</div>' }

function mountWithHost(hostname) {
  vi.stubGlobal('location', { ...window.location, hostname })
  global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
  return mount(PublicRoot, {
    global: { stubs: { LandingPage: LandingStub, StatusPage: StatusPageStub } },
  })
}

describe('PublicRoot', () => {
  beforeEach(() => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', 'example.com')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
  })

  it('behandelt "staging.<basedomain>" als Staging-Umgebung, nicht als Mandanten-Slug', async () => {
    const wrapper = mountWithHost('staging.example.com')
    await flushPromises()

    expect(wrapper.findComponent(LandingStub).exists()).toBe(true)
    expect(wrapper.findComponent(StatusPageStub).exists()).toBe(false)
  })

  it('behandelt "www.<basedomain>" weiterhin als Startseite (Regression)', async () => {
    const wrapper = mountWithHost('www.example.com')
    await flushPromises()

    expect(wrapper.findComponent(LandingStub).exists()).toBe(true)
    expect(wrapper.findComponent(StatusPageStub).exists()).toBe(false)
  })

  it('behandelt eine echte Mandanten-Subdomain weiterhin als Statusseite', async () => {
    const wrapper = mountWithHost('acme.example.com')
    await flushPromises()

    const statusPage = wrapper.findComponent(StatusPageStub)
    expect(statusPage.exists()).toBe(true)
    expect(statusPage.props('slug')).toBe('acme')
  })

  it('ohne konfigurierte Basisdomain gilt keine Subdomain-Konvention: der Host wird als eigene Domain aufgelöst', async () => {
    vi.stubEnv('VITE_APP_BASE_DOMAINS', '')
    const wrapper = mountWithHost('acme.example.com')
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/public/resolve-domain?host=acme.example.com'), expect.anything())
    expect(wrapper.findComponent(StatusPageStub).exists()).toBe(false)
  })
})
