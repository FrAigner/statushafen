// Typed helpers for tests that replace the global fetch.
//
// Tests only describe the part of a Response they need (ok, status, json, sometimes text or headers).
// A literal like { ok: true, json: async () => data } is not a Response for TypeScript, so the stubs
// are cast here in one place instead of in every test.
import { vi, type Mock } from 'vitest'

export type FetchFn = typeof globalThis.fetch
export type FetchMock = Mock<FetchFn>

// A partial Response: whatever the code under test reads from it.
export type ResponseStub = Partial<Omit<Response, 'json'>> & { json?: () => unknown; [key: string]: unknown }

export function asResponse(stub: ResponseStub): Response {
  return stub as unknown as Response
}

// vi.fn() typed like fetch. The implementation may resolve to a partial Response stub.
export function fetchMock(
  impl?: (input: RequestInfo | URL, init?: RequestInit) => ResponseStub | Response | Promise<ResponseStub | Response>,
): FetchMock {
  const mock = impl ? vi.fn(impl) : vi.fn()
  return mock as unknown as FetchMock
}

// The current global fetch as a mock (after global.fetch = fetchMock(...) or vi.stubGlobal).
export function mockedFetch(): FetchMock {
  return vi.mocked(globalThis.fetch)
}
