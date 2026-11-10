import type { ReactNode } from 'react'

import { useMutation } from '../../lib/useMutation'
import {
  openExternal,
  openReleaseTerminal,
  type ReleaseCheck,
  type ReleaseCheckState,
  type ReleaseFix,
} from '../../platform'
import { quietButtonClass } from '../controls'
import { AlertIcon, CheckIcon, ExternalIcon, InfoIcon, XIcon } from '../icons'
import { ErrorText } from '../settings/rows'
import { CopyCommand } from './CopyCommand'

// checkIcons mark each state beside its words, so none is told by color alone: ✓ and ℹ
// only inform, ⚠ warns, and ✗ blocks the release.
const checkIcons: Readonly<Record<ReleaseCheckState, ReactNode>> = {
  ok: <CheckIcon className="mt-px size-4 shrink-0 text-success" />,
  info: <InfoIcon className="mt-px size-4 shrink-0 text-info" />,
  warn: <AlertIcon className="mt-px size-4 shrink-0 text-warning" />,
  block: <XIcon className="mt-px size-4 shrink-0 text-danger" />,
}

// checkStates say each state in words, for assistive technology, before the check's text.
const checkStates: Readonly<Record<ReleaseCheckState, string>> = {
  ok: 'Passed:',
  info: 'Note:',
  warn: 'Warning:',
  block: 'Blocks the release:',
}

export interface ReleaseChecksProps {
  checks: readonly ReleaseCheck[]
  // repo and dir are the repository and its clone, which Open in terminal opens.
  repo: string
  dir: string
  // checking reports the checks being read again, which Check again waits out.
  checking: boolean
  onCheckAgain: () => void
  // onOpenSettings opens Settings → Integrations, where magex is installed and updated.
  onOpenSettings: () => void
  // onWatch opens the run that holds the clone.
  onWatch: (runId: number) => void
}

// ReleaseChecks lists what is true about the clone, origin, GitHub, and magex right now,
// each with its state, the commands it offers to copy (which the app never runs), and
// the fixes beside it: open the settings, update magex there, open the terminal in the
// clone, check again, view the checks on GitHub, or watch the run that holds the clone.
export function ReleaseChecks({
  checks,
  repo,
  dir,
  checking,
  onCheckAgain,
  onOpenSettings,
  onWatch,
}: ReleaseChecksProps) {
  const terminal = useMutation(() => openReleaseTerminal(repo, dir))

  // fixButton is the button for one fix on a check.
  const fixButton = (check: ReleaseCheck, fix: ReleaseFix) => {
    switch (fix) {
      case 'openSettings':
        return (
          <button key={fix} type="button" onClick={onOpenSettings} className={quietButtonClass}>
            Open Settings
          </button>
        )
      case 'update':
        return (
          <button key={fix} type="button" onClick={onOpenSettings} className={quietButtonClass}>
            Update magex in Settings
          </button>
        )
      case 'openTerminal':
        return (
          <button
            key={fix}
            type="button"
            disabled={terminal.pending}
            onClick={() => {
              void terminal.run()
            }}
            className={quietButtonClass}
          >
            Open in terminal
          </button>
        )
      case 'checkAgain':
        return (
          <button key={fix} type="button" disabled={checking} onClick={onCheckAgain} className={quietButtonClass}>
            {checking ? 'Checking…' : 'Check again'}
          </button>
        )
      case 'view':
        return (
          <button
            key={fix}
            type="button"
            onClick={() => {
              openExternal(check.link)
            }}
            className={quietButtonClass}
          >
            View on GitHub
            <ExternalIcon className="size-3" />
          </button>
        )
      case 'watch':
        return (
          <button
            key={fix}
            type="button"
            onClick={() => {
              onWatch(check.runId)
            }}
            className={quietButtonClass}
          >
            Watch the run
          </button>
        )
    }
  }

  return (
    <div className="flex flex-col gap-1">
      <ul aria-label="Checks" className="flex flex-col gap-2">
        {checks.map((check) => (
          <li key={check.id} className="flex items-start gap-2 text-sm">
            {checkIcons[check.state]}
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <p className="text-text">
                <span className="sr-only">{checkStates[check.state]} </span>
                {check.text}
              </p>
              {check.commands.map((command) => (
                <CopyCommand key={command} command={command} />
              ))}
              {check.fixes.length > 0 && (
                <div className="flex flex-wrap items-center gap-1">
                  {check.fixes.map((fix) => fixButton(check, fix))}
                </div>
              )}
            </div>
          </li>
        ))}
      </ul>
      <ErrorText error={terminal.error} />
    </div>
  )
}
