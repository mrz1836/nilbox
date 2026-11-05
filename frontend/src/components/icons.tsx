import type { ReactNode } from 'react'

// Small line icons, drawn on a 24-unit grid in the current text color. They are
// decorative: the control that holds one carries the accessible name.

interface IconProps {
  className?: string
}

function Icon({ className = 'size-4', children }: IconProps & { children: ReactNode }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
    >
      {children}
    </svg>
  )
}

// SlidersIcon stands for settings.
export function SlidersIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1" />
      <circle cx="15" cy="6" r="2" />
      <circle cx="9" cy="12" r="2" />
      <circle cx="17" cy="18" r="2" />
    </Icon>
  )
}

// ExternalIcon marks a link that opens in the browser.
export function ExternalIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M14 4h6v6M20 4l-8.5 8.5M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
    </Icon>
  )
}

// PersonIcon stands in for an account with no login yet.
export function PersonIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="9" r="3.5" />
      <path d="M5.5 19.5a6.5 6.5 0 0 1 13 0" />
    </Icon>
  )
}

// ChevronLeftIcon stands for going back.
export function ChevronLeftIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M15 5l-7 7 7 7" />
    </Icon>
  )
}
