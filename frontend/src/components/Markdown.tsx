import type { ReactElement } from 'react'

import { renderMarkdown } from '../lib/markdown'
import { openExternal } from '../platform'

const BASE = 'markdown min-w-0 space-y-2 text-[0.8125rem] leading-relaxed break-words text-text'

export interface MarkdownProps {
  // text is the markdown, as GitHub holds it.
  text: string
  // className adds classes to the container's own.
  className?: string
}

// Markdown shows text written in GitHub-flavoured markdown, such as a pull request's
// description, without loading anything it points to: its links open in the browser,
// and its images are links too (lib/markdown).
export function Markdown({ text, className }: MarkdownProps): ReactElement {
  return (
    <div className={className === undefined ? BASE : `${BASE} ${className}`}>
      {renderMarkdown(text, { openLink: openExternal })}
    </div>
  )
}
