import { bulkDoneText, doneURL, type BulkLine } from '../../lib/githubThreads'
import { useNow } from '../../lib/useNow'
import { openExternal, type GitHubBulkDone } from '../../platform'
import { quietButtonClass, smallIconButtonClass } from '../controls'
import { AlertIcon, CircleCheckIcon, ExternalIcon, PauseIcon, StopIcon, XIcon } from '../icons'
import { ProgressStatus } from '../ProgressStatus'

// BulkDoneLine says how a bulk mark stands, in one line: going, with how far it got; paused,
// with why; or how it ended. A running one is a status, which a screen reader reads at each
// step.
export function BulkDoneLine({ line }: { line: BulkLine }) {
  const step = { text: line.text, share: line.share }

  switch (line.kind) {
    case 'running':
      return <ProgressStatus step={step} />
    case 'paused':
      return <ProgressStatus step={step} icon={<PauseIcon className="size-3.5 shrink-0" />} />
    case 'failing':
      return <ProgressStatus step={step} icon={<AlertIcon className="size-3.5 shrink-0" />} warning />
    case 'done':
      return (
        <p role="status" className="flex min-w-0 items-center gap-2 text-text">
          <CircleCheckIcon className="size-3.5 shrink-0 text-success" />
          <span className="truncate">{line.text}</span>
        </p>
      )
    case 'partial':
      return (
        <p role="status" className="flex min-w-0 items-center gap-2 text-warning">
          <AlertIcon className="size-3.5 shrink-0" />
          <span>{line.text}</span>
        </p>
      )
    case 'stopped':
      return (
        <p role="status" className="min-w-0 text-muted">
          {line.text}
        </p>
      )
  }
}

export interface BulkDoneBandProps {
  // bulk is how the bulk mark stands; the band shows nothing when there is none to show.
  bulk: GitHubBulkDone
  // onStop stops a running one, at once, and onDismiss forgets one that ended.
  onStop: () => void
  onDismiss: () => void
  className?: string
}

// BulkDoneBand is the GitHub tab's band for the bulk mark: while it runs, how far it got,
// with Stop, which takes effect at once and needs no asking, since stopping loses nothing;
// once it ended, how it went, with GitHub's Done view, until the user dismisses it.
export function BulkDoneBand({ bulk, onStop, onDismiss, className = '' }: BulkDoneBandProps) {
  const now = useNow()
  const line = bulkDoneText(now, bulk)

  if (line === null) {
    return null
  }

  return (
    <div className={`flex items-center gap-2 text-xs ${className}`}>
      <div className="min-w-0 flex-1">
        <BulkDoneLine line={line} />
      </div>
      {bulk.running ? (
        <button type="button" onClick={onStop} aria-label="Stop marking done" className={quietButtonClass}>
          <StopIcon className="size-3.5" />
          Stop
        </button>
      ) : (
        <>
          {!bulk.stopped && (
            <button
              type="button"
              onClick={() => {
                openExternal(doneURL)
              }}
              className={quietButtonClass}
            >
              <ExternalIcon className="size-3.5" />
              GitHub’s Done view
            </button>
          )}
          <button type="button" onClick={onDismiss} aria-label="Dismiss" className={smallIconButtonClass}>
            <XIcon className="size-3.5" />
          </button>
        </>
      )}
    </div>
  )
}
