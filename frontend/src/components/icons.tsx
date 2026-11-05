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

// ChevronDownIcon marks a control that opens a menu.
export function ChevronDownIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M7 10l5 5 5-5" />
    </Icon>
  )
}

// PullRequestIcon stands for a pull request: two commits and a branch merging back.
export function PullRequestIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="6" cy="6" r="2.25" />
      <circle cx="6" cy="18" r="2.25" />
      <circle cx="18" cy="18" r="2.25" />
      <path d="M6 8.25v7.5M18 15.75V9.5a3 3 0 0 0-3-3h-4.5M12.5 4l-2.5 2.5 2.5 2.5" />
    </Icon>
  )
}

// IssueIcon stands for an issue.
export function IssueIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="12" r="8.25" />
      <circle cx="12" cy="12" r="1.75" fill="currentColor" />
    </Icon>
  )
}

// CheckIcon marks something that passed.
export function CheckIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M5 12.5l4.5 4.5L19 7.5" />
    </Icon>
  )
}

// XIcon marks something that failed.
export function XIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M6.5 6.5l11 11M17.5 6.5l-11 11" />
    </Icon>
  )
}

// DotIcon marks something still running.
export function DotIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="12" r="4" fill="currentColor" stroke="none" />
    </Icon>
  )
}

// RingIcon marks something absent, such as a commit without checks.
export function RingIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="12" r="5" />
    </Icon>
  )
}

// EyeIcon stands for reviews.
export function EyeIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z" />
      <circle cx="12" cy="12" r="2.75" />
    </Icon>
  )
}

// CommentIcon stands for comments.
export function CommentIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M6.5 4.5h11a2 2 0 0 1 2 2V14a2 2 0 0 1-2 2H11l-4.5 3.5V16a2 2 0 0 1-2-2V6.5a2 2 0 0 1 2-2z" />
    </Icon>
  )
}

// TerminalIcon stands for opening a session in the terminal.
export function TerminalIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M5 7.5l4.5 4.5L5 16.5M12.5 16.5H19" />
    </Icon>
  )
}
