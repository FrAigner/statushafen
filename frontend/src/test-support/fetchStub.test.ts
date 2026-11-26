import { describe, it, expect, afterEach, vi } from 'vitest'
import { asResponse, fetchMock, mockedFetch } from './fetchStub'

describe('fetchStub', () => {
  const original = globalThis.fetch
  afterEach(() => {
    globalThis.fetch = original
  })

  it('mocks fetch without an implementation (resolves to undefined)', async () => {
    const mock = fetchMock()
    expect(vi.isMockFunction(mock)).toBe(true)
    expect(await mock('/api/x')).toBeUndefined()
  })

  it('mocks fetch with an implementation that returns a partial response', async () => {
    const mock = fetchMock(() => Promise.resolve({ ok: true, json: () => Promise.resolve({ a: 1 }) }))
    const res = await mock('/api/x', { method: 'POST' })
    expect(res.ok).toBe(true)
    expect(await res.json()).toEqual({ a: 1 })
    expect(mock).toHaveBeenCalledWith('/api/x', { method: 'POST' })
  })

  it('casts a partial stub to a Response without changing it', () => {
    const stub = { ok: false, status: 404 }
    expect(asResponse(stub)).toBe(stub)
  })

  it('returns the installed global fetch as a mock', () => {
    const mock = fetchMock()
    globalThis.fetch = mock
    expect(mockedFetch()).toBe(mock)
  })
})
