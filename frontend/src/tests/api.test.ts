// API client unit tests — no network calls; fetch is mocked.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { FetchError } from '../api/client'

// We test the FetchError class structure independently since testing
// the fetch wrapper requires fetch mock infrastructure.
describe('FetchError', () => {
  it('is an instance of Error', () => {
    const e = new FetchError(404, 'not found', 'NOT_FOUND')
    expect(e instanceof Error).toBe(true)
    expect(e instanceof FetchError).toBe(true)
  })

  it('carries status, message, and code', () => {
    const e = new FetchError(503, 'Service Unavailable', 'SERVICE_DOWN')
    expect(e.status).toBe(503)
    expect(e.message).toBe('Service Unavailable')
    expect(e.code).toBe('SERVICE_DOWN')
    expect(e.name).toBe('FetchError')
  })

  it('defaults code to UNKNOWN when not supplied', () => {
    const e = new FetchError(500, 'oops')
    expect(e.code).toBe('UNKNOWN')
  })
})

describe('API client fetch behaviour', () => {
  const fetchMock = vi.fn()
  beforeEach(() => { vi.stubGlobal('fetch', fetchMock) })
  afterEach(() => { vi.unstubAllGlobals() })

  it('throws FetchError on non-OK response', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 401,
      statusText: 'Unauthorized',
      json: async () => ({ error: 'token expired', code: 'AUTH_EXPIRED' }),
    })
    const { api } = await import('../api/client')
    await expect(api.get('/cohorts')).rejects.toSatisfy(
      (e: unknown) => e instanceof FetchError && (e as FetchError).status === 401
    )
  })

  it('sends Authorization header when token present', async () => {
    localStorage.setItem('aml_id_token', 'test-token-xyz')
    fetchMock.mockResolvedValue({
      ok: true, status: 200, json: async () => [],
    })
    const { api } = await import('../api/client')
    await api.get('/cohorts')
    // token() reads localStorage at call time, so the header must be present.
    const calls = fetchMock.mock.calls
    const lastCall = calls[calls.length - 1]
    const headers = (lastCall[1] as RequestInit).headers as Record<string, string>
    expect(headers['Authorization']).toBe('Bearer test-token-xyz')
    localStorage.removeItem('aml_id_token')
  })

  it('returns undefined for 204 No Content', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 204 })
    const { api } = await import('../api/client')
    const result = await api.get('/something')
    expect(result).toBeUndefined()
  })

  it('calls the API on the same origin by default', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => [] })
    const { api } = await import('../api/client')
    await api.get('/cohorts')
    const url = fetchMock.mock.calls[fetchMock.mock.calls.length - 1][0]
    expect(url).toBe('/api/v1/cohorts')
  })

  it('honours VITE_API_BASE_URL for a backend on another origin', async () => {
    // Re-import the module so the build-time constant is re-evaluated.
    vi.resetModules()
    vi.stubEnv('VITE_API_BASE_URL', 'https://fcrt-api.fly.dev/')
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => [] })
    const { api } = await import('../api/client')
    await api.get('/cohorts')
    const url = fetchMock.mock.calls[fetchMock.mock.calls.length - 1][0]
    // The trailing slash is normalised away.
    expect(url).toBe('https://fcrt-api.fly.dev/api/v1/cohorts')
    vi.unstubAllEnvs()
    vi.resetModules()
  })

  it('reports a 2xx that is not JSON as an unreachable API', async () => {
    // A static host answers unrouted /api/** requests with index.html, so a
    // successful-looking response that will not parse means no backend is there.
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => { throw new SyntaxError("Unexpected token '<'") },
    })
    const { api } = await import('../api/client')
    // Assert on fields rather than instanceof: an earlier test resets the module
    // registry, so this import's FetchError is a different class object.
    const err = await api.get('/cohorts').catch((e: unknown) => e)
    expect(err).toMatchObject({ code: 'BAD_RESPONSE', status: 200 })
    expect((err as Error).message).toMatch(/not reachable/)
  })
})
