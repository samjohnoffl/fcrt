// Campaign & benchmark API endpoint tests — fetch is mocked, no network.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

describe('campaignApi', () => {
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

  it('list GETs /campaigns', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => [] })
    const { campaignApi } = await import('../api/endpoints')
    await campaignApi.list()
    const url = String(fetchMock.mock.calls[fetchMock.mock.calls.length - 1]?.[0])
    expect(url).toContain('/campaigns')
  })

  it('create POSTs to /campaigns', async () => {
    fetchMock.mockResolvedValue({
      ok: true, status: 201,
      json: async () => ({ id: 'abc', name: 'n', budget: 10 }),
    })
    const { campaignApi } = await import('../api/endpoints')
    await campaignApi.create({ name: 'n', description: 'd', budget: 10 })
    const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
    expect(String(call?.[0])).toContain('/campaigns')
    expect((call?.[1] as RequestInit).method).toBe('POST')
  })

  it('benchmark GETs /campaigns/{id}/benchmark without params', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
    const { campaignApi } = await import('../api/endpoints')
    await campaignApi.benchmark('camp-1')
    const url = String(fetchMock.mock.calls[fetchMock.mock.calls.length - 1]?.[0])
    expect(url).toContain('/campaigns/camp-1/benchmark')
    expect(url).not.toContain('?')
  })

  it('benchmark appends candidates query param', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
    const { campaignApi } = await import('../api/endpoints')
    await campaignApi.benchmark('camp-1', 250)
    const url = String(fetchMock.mock.calls[fetchMock.mock.calls.length - 1]?.[0])
    expect(url).toContain('/campaigns/camp-1/benchmark?candidates=250')
  })
})
