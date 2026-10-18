// Regression API endpoint tests — fetch is mocked, no network.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

describe('regressionApi', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    localStorage.setItem('aml_id_token', 'test-token')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.removeItem('aml_id_token')
    fetchMock.mockReset()
  })

  it('verify POSTs the pack to /regression/verify', async () => {
    fetchMock.mockResolvedValue({
      ok: true, status: 200,
      json: async () => ({ ok: true, pack_version: '1.0', total: 2, passed: 2, failed: 0, results: [] }),
    })
    const { regressionApi } = await import('../api/endpoints')
    const res = await regressionApi.verify({ version: '1.0', cases: [] })
    const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
    expect(String(call?.[0])).toContain('/regression/verify')
    expect((call?.[1] as RequestInit).method).toBe('POST')
    expect(res.ok).toBe(true)
    expect(res.total).toBe(2)
  })

  it('verify attaches the Authorization header', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => ({ ok: true }) })
    const { regressionApi } = await import('../api/endpoints')
    await regressionApi.verify({})
    const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
    const headers = (call?.[1] as RequestInit).headers as Record<string, string>
    expect(headers['Authorization']).toBe('Bearer test-token')
  })
})
