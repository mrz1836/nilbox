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
export function SlidersIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1" />
      <circle cx="15" cy="6" r="2" />
      <circle cx="9" cy="12" r="2" />
      <circle cx="17" cy="18" r="2" />
    </Icon>
  )
}

// ExternalIcon marks a link that opens in the browser.
export function ExternalIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M14 4h6v6M20 4l-8.5 8.5M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
    </Icon>
  )
}

// PersonIcon stands in for an account with no login yet.
export function PersonIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="9" r="3.5" />
      <path d="M5.5 19.5a6.5 6.5 0 0 1 13 0" />
    </Icon>
  )
}

// ChevronLeftIcon stands for going back.
export function ChevronLeftIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M15 5l-7 7 7 7" />
    </Icon>
  )
}

// ChevronDownIcon marks a control that opens a menu.
export function ChevronDownIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M7 10l5 5 5-5" />
    </Icon>
  )
}

// PullRequestIcon stands for a pull request: two commits and a branch merging back.
export function PullRequestIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="6" cy="6" r="2.25" />
      <circle cx="6" cy="18" r="2.25" />
      <circle cx="18" cy="18" r="2.25" />
      <path d="M6 8.25v7.5M18 15.75V9.5a3 3 0 0 0-3-3h-4.5M12.5 4l-2.5 2.5 2.5 2.5" />
    </Icon>
  )
}

// IssueIcon stands for an issue.
export function IssueIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="8.25" />
      <circle cx="12" cy="12" r="1.75" fill="currentColor" />
    </Icon>
  )
}

// CheckIcon marks something that passed.
export function CheckIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M5 12.5l4.5 4.5L19 7.5" />
    </Icon>
  )
}

// XIcon marks something that failed.
export function XIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M6.5 6.5l11 11M17.5 6.5l-11 11" />
    </Icon>
  )
}

// DotIcon marks something still running.
export function DotIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="4" fill="currentColor" stroke="none" />
    </Icon>
  )
}

// RingIcon marks something absent, such as a commit without checks.
export function RingIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="5" />
    </Icon>
  )
}

// EyeIcon stands for reviews.
export function EyeIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z" />
      <circle cx="12" cy="12" r="2.75" />
    </Icon>
  )
}

// CommentIcon stands for comments.
export function CommentIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M6.5 4.5h11a2 2 0 0 1 2 2V14a2 2 0 0 1-2 2H11l-4.5 3.5V16a2 2 0 0 1-2-2V6.5a2 2 0 0 1 2-2z" />
    </Icon>
  )
}

// TerminalIcon stands for opening a session in the terminal.
export function TerminalIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M5 7.5l4.5 4.5L5 16.5M12.5 16.5H19" />
    </Icon>
  )
}

// PlusIcon stands for adding something.
export function PlusIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 5v14M5 12h14" />
    </Icon>
  )
}

// SearchIcon marks a search field.
export function SearchIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="11" cy="11" r="6" />
      <path d="M20 20l-4.5-4.5" />
    </Icon>
  )
}

// MoreIcon opens a menu of further actions.
export function MoreIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="6" cy="12" r="1.25" fill="currentColor" stroke="none" />
      <circle cx="12" cy="12" r="1.25" fill="currentColor" stroke="none" />
      <circle cx="18" cy="12" r="1.25" fill="currentColor" stroke="none" />
    </Icon>
  )
}

// CopyIcon stands for duplicating something.
export function CopyIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="8.5" y="8.5" width="11" height="11" rx="2" />
      <path d="M15.5 8.5V6.5a2 2 0 0 0-2-2h-7a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h2" />
    </Icon>
  )
}

// TrashIcon stands for deleting something.
export function TrashIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4.5 7h15M10 7V5h4v2M6.5 7l1 12a1.5 1.5 0 0 0 1.5 1.5h6a1.5 1.5 0 0 0 1.5-1.5l1-12M10 11v6M14 11v6" />
    </Icon>
  )
}

// ResetIcon stands for restoring an original.
export function ResetIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4.5 9.5A8 8 0 1 1 4 13M4.5 4.5v5h5" />
    </Icon>
  )
}

// RefreshIcon stands for reading something from GitHub again.
export function RefreshIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M19.5 9.5A8 8 0 1 0 20 13M19.5 4.5v5h-5" />
    </Icon>
  )
}

// StarIcon marks a default.
export function StarIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 4.5l2.3 4.7 5.2.75-3.75 3.65.9 5.15L12 16.35l-4.65 2.4.9-5.15L4.5 9.95l5.2-.75z" />
    </Icon>
  )
}

// AlertIcon marks an error or a warning.
export function AlertIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 4.5l8.5 15h-17z" />
      <path d="M12 10v4M12 16.75v.25" />
    </Icon>
  )
}

// CircleCheckIcon marks something that checked out fine.
export function CircleCheckIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="8.25" />
      <path d="M8.5 12.25l2.5 2.5 4.5-5" />
    </Icon>
  )
}

// ChevronRightIcon marks a section that opens.
export function ChevronRightIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M10 6l6 6-6 6" />
    </Icon>
  )
}

// DocumentIcon stands for a prompt.
export function DocumentIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M7 3.5h7l4.5 4.5v11a1.5 1.5 0 0 1-1.5 1.5H7a1.5 1.5 0 0 1-1.5-1.5V5A1.5 1.5 0 0 1 7 3.5z" />
      <path d="M14 3.5V8h4.5M9 12.5h6M9 16h4" />
    </Icon>
  )
}

// ClockIcon stands for a snooze.
export function ClockIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5V12l3 2" />
    </Icon>
  )
}

// ArchiveIcon stands for dismissing an item out of the inbox.
export function ArchiveIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="3.5" y="4" width="17" height="4.5" rx="1" />
      <path d="M5 8.5V18a1.5 1.5 0 0 0 1.5 1.5h11A1.5 1.5 0 0 0 19 18V8.5M10 12.5h4" />
    </Icon>
  )
}

// PinIcon stands for pinning an item to the top of its list; filled, it marks one pinned.
export function PinIcon({ filled = false, ...props }: IconProps & { filled?: boolean }) {
  return (
    <Icon {...props}>
      <path d="M9.5 3.5h5v5L18 13H6l3.5-4.5z" fill={filled ? 'currentColor' : 'none'} />
      <path d="M8 3.5h8M12 13v7.5" />
    </Icon>
  )
}

// ShieldAlertIcon marks files that deserve a closer look before a review or a launch.
export function ShieldAlertIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 3.5l7 2.75v5.5c0 4.25-2.85 7.6-7 8.75-4.15-1.15-7-4.5-7-8.75v-5.5z" />
      <path d="M12 8.5v4M12 15.25v.25" />
    </Icon>
  )
}

// ShieldCheckIcon marks files that were checked and found nothing to point out.
export function ShieldCheckIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 3.5l7 2.75v5.5c0 4.25-2.85 7.6-7 8.75-4.15-1.15-7-4.5-7-8.75v-5.5z" />
      <path d="M9 12.25l2.25 2.25 4-4.25" />
    </Icon>
  )
}

// PlayIcon stands for running something.
export function PlayIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M7 4.5v15a.5.5 0 0 0 .77.42l11.5-7.5a.5.5 0 0 0 0-.84L7.77 4.08A.5.5 0 0 0 7 4.5Z" />
    </Icon>
  )
}

// TrophyIcon stands for an achievement.
export function TrophyIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M8 4h8v5a4 4 0 0 1-8 0V4ZM8 6H5a3 3 0 0 0 3 4M16 6h3a3 3 0 0 1-3 4M12 13v4M8.5 20h7M10 17h4" />
    </Icon>
  )
}

// SunIcon stands for the light theme.
export function SunIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M5.3 18.7l1.4-1.4M17.3 6.7l1.4-1.4" />
    </Icon>
  )
}

// MoonIcon stands for the dark theme.
export function MoonIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M20.5 13.6A8.5 8.5 0 1 1 10.4 3.5a6.6 6.6 0 0 0 10.1 10.1Z" />
    </Icon>
  )
}

// DisplayIcon stands for following the system, as the System theme does.
export function DisplayIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="3" y="4" width="18" height="12.5" rx="2" />
      <path d="M8.5 20.5h7M12 16.5v4" />
    </Icon>
  )
}

// BellIcon stands for notifications.
export function BellIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M6 16.5V11a6 6 0 0 1 12 0v5.5l1.5 2H4.5z" />
      <path d="M10 20.5a2 2 0 0 0 4 0" />
    </Icon>
  )
}

// AssignIcon stands for an item assigned to the user.
export function AssignIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="10" cy="8.5" r="3.5" />
      <path d="M3.5 19.5a6.5 6.5 0 0 1 10.5-5.1M16 17.5l2 2 3.5-4" />
    </Icon>
  )
}

// UnassignIcon stands for taking the user off an item's assignees: a person with a minus.
export function UnassignIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="10" cy="8.5" r="3.5" />
      <path d="M3.5 19.5a6.5 6.5 0 0 1 10.5-5.1M16 17.5h5.5" />
    </Icon>
  )
}

// EyeOffIcon stands for taking the user off a pull request's reviewers: a struck eye.
export function EyeOffIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M9.9 5.7A9.8 9.8 0 0 1 12 5.5c6 0 9.5 6.5 9.5 6.5a17 17 0 0 1-2.4 3.2M6.3 7.3C3.9 8.9 2.5 12 2.5 12S6 18.5 12 18.5a9.3 9.3 0 0 0 4.7-1.3" />
      <path d="M10 10.1a2.75 2.75 0 0 0 3.9 3.9M3.5 3.5l17 17" />
    </Icon>
  )
}

// LinkIcon stands for an item's address on the web, such as to copy it.
export function LinkIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7L11.8 5.2" />
      <path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7l1.7-1.7" />
    </Icon>
  )
}

// InboxIcon stands for what the inbox does with an item: a tray.
export function InboxIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4 13.5l2.2-7.3A1.5 1.5 0 0 1 7.6 5h8.8a1.5 1.5 0 0 1 1.4 1.2l2.2 7.3" />
      <path d="M4 13.5V18a1.5 1.5 0 0 0 1.5 1.5h13A1.5 1.5 0 0 0 20 18v-4.5h-4.5l-1 2h-5l-1-2z" />
    </Icon>
  )
}

// ImportIcon stands for bringing prompts in from a file: an arrow into a tray.
export function ImportIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 4v10.5M7.5 10l4.5 4.5 4.5-4.5M4.5 15.5v2A2.5 2.5 0 0 0 7 20h10a2.5 2.5 0 0 0 2.5-2.5v-2" />
    </Icon>
  )
}

// ExportIcon stands for writing prompts out to a file: an arrow out of a tray.
export function ExportIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M12 14.5V4M7.5 8.5 12 4l4.5 4.5M4.5 15.5v2A2.5 2.5 0 0 0 7 20h10a2.5 2.5 0 0 0 2.5-2.5v-2" />
    </Icon>
  )
}

// HistoryIcon stands for bringing back something kept from before: a clock turning back.
export function HistoryIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4.5 9.5A8 8 0 1 1 4 13M4.5 4.5v5h5M12 8v4l2.5 1.5" />
    </Icon>
  )
}

// ProjectIcon stands for a GitHub project: a board with columns of cards.
export function ProjectIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="3.5" y="3.5" width="17" height="17" rx="2.5" />
      <path d="M8 7.5v9M12 7.5v5M16 7.5v7" />
    </Icon>
  )
}

// SortIcon stands for the order of a list: an arrow up beside an arrow down.
export function SortIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M8 19V5M4.5 8.5 8 5l3.5 3.5M16 5v14M12.5 15.5 16 19l3.5-3.5" />
    </Icon>
  )
}

// StopIcon stands for stopping something under way, and marks something that was stopped.
export function StopIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="6.5" y="6.5" width="11" height="11" rx="2" />
    </Icon>
  )
}

// InfoIcon marks a note that only informs.
export function InfoIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="12" r="8.25" />
      <path d="M12 11v5.25M12 7.75v.25" />
    </Icon>
  )
}

// TagIcon stands for a release: the version tag it pushes.
export function TagIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4 5.5V11l8.5 8.5a1.5 1.5 0 0 0 2.1 0l4.9-4.9a1.5 1.5 0 0 0 0-2.1L11 4H5.5A1.5 1.5 0 0 0 4 5.5z" />
      <circle cx="8" cy="8" r="1.25" />
    </Icon>
  )
}

// AnchorIcon stands for the hooks: the pre-commit hook a repository's commits are held to.
export function AnchorIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="5.5" r="2" />
      <path d="M12 7.5V20M8.5 11h7M5 13.5a7 7 0 0 0 14 0" />
    </Icon>
  )
}
