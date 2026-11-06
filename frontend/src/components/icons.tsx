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

// PlusIcon stands for adding something.
export function PlusIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M12 5v14M5 12h14" />
    </Icon>
  )
}

// SearchIcon marks a search field.
export function SearchIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="11" cy="11" r="6" />
      <path d="M20 20l-4.5-4.5" />
    </Icon>
  )
}

// MoreIcon opens a menu of further actions.
export function MoreIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="6" cy="12" r="1.25" fill="currentColor" stroke="none" />
      <circle cx="12" cy="12" r="1.25" fill="currentColor" stroke="none" />
      <circle cx="18" cy="12" r="1.25" fill="currentColor" stroke="none" />
    </Icon>
  )
}

// CopyIcon stands for duplicating something.
export function CopyIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <rect x="8.5" y="8.5" width="11" height="11" rx="2" />
      <path d="M15.5 8.5V6.5a2 2 0 0 0-2-2h-7a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h2" />
    </Icon>
  )
}

// TrashIcon stands for deleting something.
export function TrashIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M4.5 7h15M10 7V5h4v2M6.5 7l1 12a1.5 1.5 0 0 0 1.5 1.5h6a1.5 1.5 0 0 0 1.5-1.5l1-12M10 11v6M14 11v6" />
    </Icon>
  )
}

// ResetIcon stands for restoring an original.
export function ResetIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M4.5 9.5A8 8 0 1 1 4 13M4.5 4.5v5h5" />
    </Icon>
  )
}

// RefreshIcon stands for reading something from GitHub again.
export function RefreshIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M19.5 9.5A8 8 0 1 0 20 13M19.5 4.5v5h-5" />
    </Icon>
  )
}

// StarIcon marks a default.
export function StarIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M12 4.5l2.3 4.7 5.2.75-3.75 3.65.9 5.15L12 16.35l-4.65 2.4.9-5.15L4.5 9.95l5.2-.75z" />
    </Icon>
  )
}

// AlertIcon marks an error or a warning.
export function AlertIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M12 4.5l8.5 15h-17z" />
      <path d="M12 10v4M12 16.75v.25" />
    </Icon>
  )
}

// CircleCheckIcon marks something that checked out fine.
export function CircleCheckIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="12" r="8.25" />
      <path d="M8.5 12.25l2.5 2.5 4.5-5" />
    </Icon>
  )
}

// ChevronRightIcon marks a section that opens.
export function ChevronRightIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M10 6l6 6-6 6" />
    </Icon>
  )
}

// BracesIcon stands for template variables.
export function BracesIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M9 4.5H8a2 2 0 0 0-2 2V10a2 2 0 0 1-2 2 2 2 0 0 1 2 2v3.5a2 2 0 0 0 2 2h1M15 4.5h1a2 2 0 0 1 2 2V10a2 2 0 0 0 2 2 2 2 0 0 0-2 2v3.5a2 2 0 0 1-2 2h-1" />
    </Icon>
  )
}

// DocumentIcon stands for a prompt.
export function DocumentIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M7 3.5h7l4.5 4.5v11a1.5 1.5 0 0 1-1.5 1.5H7a1.5 1.5 0 0 1-1.5-1.5V5A1.5 1.5 0 0 1 7 3.5z" />
      <path d="M14 3.5V8h4.5M9 12.5h6M9 16h4" />
    </Icon>
  )
}

// ClockIcon stands for a snooze.
export function ClockIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5V12l3 2" />
    </Icon>
  )
}

// ArchiveIcon stands for dismissing an item out of the inbox.
export function ArchiveIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <rect x="3.5" y="4" width="17" height="4.5" rx="1" />
      <path d="M5 8.5V18a1.5 1.5 0 0 0 1.5 1.5h11A1.5 1.5 0 0 0 19 18V8.5M10 12.5h4" />
    </Icon>
  )
}
