// Syntax-highlighted inline JSON viewer.
// No external dependency — pure DOM rendering.

interface Props {
  value: unknown
  maxHeight?: number
}

function highlight(json: string): string {
  return json
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(
      /("(\\u[\dA-Fa-f]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?)/g,
      (match) => {
        let cls = 'json-number'
        if (/^"/.test(match)) {
          cls = /:$/.test(match) ? 'json-key' : 'json-string'
        } else if (/true|false/.test(match)) {
          cls = 'json-bool'
        } else if (/null/.test(match)) {
          cls = 'json-null'
        }
        return `<span class="${cls}">${match}</span>`
      },
    )
}

export function JsonViewer({ value, maxHeight = 480 }: Props) {
  const json = JSON.stringify(value, null, 2)
  return (
    <div
      className="json-viewer"
      style={{ maxHeight }}
      role="region"
      aria-label="JSON content"
      // eslint-disable-next-line react/no-danger
      dangerouslySetInnerHTML={{ __html: highlight(json) }}
    />
  )
}

// Collapsible wrapper
import { useState } from 'react'
export function CollapsibleJson({ label, value }: { label: string; value: unknown }) {
  const [open, setOpen] = useState(false)
  return (
    <div>
      <button
        className="collapsible-trigger"
        onClick={() => setOpen(o => !o)}
        aria-expanded={open}
      >
        <span className={`collapsible-arrow${open ? ' open' : ''}`}>▶</span>
        {label}
      </button>
      {open && (
        <div className="collapsible-body">
          <JsonViewer value={value} />
        </div>
      )}
    </div>
  )
}
