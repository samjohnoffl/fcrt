// Product logo: a shield with a signal trace, rendered as inline SVG so it
// stays crisp at any size and needs no asset pipeline. The tile carries the
// navy→teal brand gradient; the glyph inherits currentColor.

export function Logo({ size = 34 }: { size?: number }) {
  return (
    <span
      className="brand-logo"
      style={{ width: size, height: size, borderRadius: Math.round(size * 0.24) }}
      aria-hidden="true"
    >
      <svg viewBox="0 0 32 32" width={Math.round(size * 0.62)} height={Math.round(size * 0.62)}>
        <path
          d="M16 3.6 25.6 7.4v7.2c0 5.9-4 10.2-9.6 12.1-5.6-1.9-9.6-6.2-9.6-12.1V7.4L16 3.6Z"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinejoin="round"
        />
        <path
          d="M9.8 15.6h3.1l1.9-4.3 2.4 8.4 1.9-4.1h3.1"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </span>
  )
}
