import type { ReactNode } from 'react'

import { markRanges } from '../lib/query'

export interface HighlightProps {
  text: string
  // marks is what to mark, as markTerms joins it; '' marks nothing.
  marks: string
}

// Highlight shows text with the parts a filter looks for marked. The mark sets its own
// colors, rather than the browser's yellow, so it reads in both themes. It renders React
// nodes only: the text is never written as markup.
export function Highlight({ text, marks }: HighlightProps) {
  const ranges = markRanges(text, marks)

  if (ranges.length === 0) {
    return text
  }

  const parts: ReactNode[] = []
  let at = 0

  for (const [from, to] of ranges) {
    if (from > at) {
      parts.push(text.slice(at, from))
    }

    parts.push(
      <mark key={from} className="rounded-sm bg-warning/25 text-inherit">
        {text.slice(from, to)}
      </mark>,
    )
    at = to
  }

  if (at < text.length) {
    parts.push(text.slice(at))
  }

  return parts
}
