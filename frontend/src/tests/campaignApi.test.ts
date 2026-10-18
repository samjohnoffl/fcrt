// Campaign & adaptive-discovery API endpoint tests — fetch is mocked, no network.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

describe('campaign & discovery APIs', () => {
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

  function lastCall() {
    return fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
  }

  it('campaign list GETs /campaigns', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => [] })
    const { campaignApi } = await import('../api/endpoints')
    await campaignApi.list()
    expect(String(lastCall()?.[0])).toContain('/campaigns')
  })

  it('campaign create POSTs to /campaigns', async () => {
    fetchMock.mockResolvedValue({
      ok: true, status: 201,
      json: async () => ({ id: 'abc', name: 'n', budget: 10 }),
    })
    const { campaignApi } = await import('../api/endpoints')
    await campaignApi.create({ name: 'n', description: 'd', budget: 10 })
    expect(String(lastCall()?.[0])).toContain('/campaigns')
    expect((lastCall()?.[1] as RequestInit).method).toBe('POST')
  })

  it('discovery run POSTs strategy and budget to the cohort', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 201, json: async () => ({}) })
    const { discoveryApi } = await import('../api/endpoints')
    await discoveryApi.run('cohort-1', { strategy: 'adaptive', budget: 7 })
    const call = lastCall()
    expect(String(call?.[0])).toContain('/cohorts/cohort-1/discovery')
    expect((call?.[1] as RequestInit).method).toBe('POST')
    expect(String((call?.[1] as RequestInit).body)).toContain('"strategy":"adaptive"')
    expect(String((call?.[1] as RequestInit).body)).toContain('"budget":7')
  })

  it('discovery latest GETs the cohort run history', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
    const { discoveryApi } = await import('../api/endpoints')
    await discoveryApi.latest('cohort-1')
    expect(String(lastCall()?.[0])).toContain('/cohorts/cohort-1/discovery')
    expect((lastCall()?.[1] as RequestInit).method).toBe('GET')
  })

  it('benchmark GETs the cohort benchmark, not the removed campaign endpoint', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
    const { discoveryApi } = await import('../api/endpoints')
    await discoveryApi.benchmark('cohort-1')
    const url = String(lastCall()?.[0])
    expect(url).toContain('/cohorts/cohort-1/benchmark')
    expect(url).not.toContain('/campaigns/')
  })
})
