import type { ReactNode } from 'react'

import { XIcon } from './icons'

export interface ErrorBannerProps {
  // children is the message: what failed, and why when it is known.
  children: ReactNode
  // className places the banner, such as its margins.
  className?: string
  // onClose, when set, puts a button on the banner that closes it.
  onClose?: () => void
}

// ErrorBanner reports a failure that holds up a whole page, such as a listing that could
// not load. It is an alert, so assistive tech reads it out as soon as it shows; render it
// only while there is something to report. Given onClose, it has a button to close it.
export function ErrorBanner({ children, className = '', onClose }: ErrorBannerProps) {
  return (
    <div
      role="alert"
      className={`flex items-start gap-3 rounded-md border border-danger px-3 py-2 text-sm text-danger ${className}`}
    >
      <div className="min-w-0 flex-1">{children}</div>
      {onClose !== undefined && (
        <button
          type="button"
          aria-label="Close"
          title="Close"
          onClick={onClose}
          className="-my-1 -mr-1.5 flex size-7 shrink-0 items-center justify-center rounded-md text-danger/80 transition-colors hover:bg-danger/10 hover:text-danger"
        >
          <XIcon className="size-4" />
        </button>
      )}
    </div>
  )
}
