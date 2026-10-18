// Recharts (used by the dashboard's coverage ring) relies on ResizeObserver,
// which jsdom does not implement. A no-op stub is enough for rendering.

class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

if (!('ResizeObserver' in globalThis)) {
  ;(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = ResizeObserverStub
}

import '@testing-library/jest-dom'
