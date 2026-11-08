import type { ReactNode } from 'react'

export interface ErrorBannerProps {
  // children is the message: what failed, and why when it is known.
  children: ReactNode
  // className places the banner, such as its margins.
  className?: string
}

// ErrorBanner reports a failure that holds up a whole page, such as a listing that could
// not load. It is an alert, so assistive tech reads it out as soon as it shows; render it
// only while there is something to report.
export function ErrorBanner({ children, className = '' }: ErrorBannerProps) {
  return (
    <p role="alert" className={`rounded-md border border-danger px-3 py-2 text-sm text-danger ${className}`}>
      {children}
    </p>
  )
}
