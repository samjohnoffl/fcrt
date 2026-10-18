// Test helpers: render a component inside the providers it expects
// (React Query + a router) with retries disabled so failures surface fast.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { render } from '@testing-library/react'

export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: 0, gcTime: 0 },
      mutations: { retry: false },
    },
  })
}

export function renderWithProviders(ui: React.ReactNode, route = '/') {
  const client = makeQueryClient()
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  )
}

/** A fetch Response whose json() resolves to body. */
export function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? 'OK' : 'Error',
    json: async () => body,
  } as unknown as Response
}

/**
 * Install a fetch stub that routes by URL substring.
 * routes: [ [urlFragment, responseBody], ... ]
 */
export function stubFetch(routes: Array<[string, unknown]>) {
  return async (input: RequestInfo | URL): Promise<Response> => {
    const url = String(input)
    for (const [fragment, body] of routes) {
      if (url.includes(fragment)) return jsonResponse(body)
    }
    return jsonResponse({ data: [] })
  }
}
