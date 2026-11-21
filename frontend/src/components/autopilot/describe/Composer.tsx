import { useId, type KeyboardEvent } from 'react'

import { describeSeesText } from '../../../lib/autopilot'
import type { Chip } from '../../../lib/describeit'
import type { DescribeAttachRef, DescribeStatus } from '../../../platform'
import { buttonClass, ErrorText, fieldClass, primaryButtonClass } from '../../controls'
import { AlertIcon, StopIcon, XIcon } from '../../icons'

export interface ComposerProps {
  text: string
  onText: (text: string) => void
  // chips are what the next message attaches, each removable.
  chips: readonly Chip[]
  onRemove: (ref: DescribeAttachRef) => void
  // running reports a turn running, which Stop stops; sending and stopping, either under
  // way; and error, why the last send or stop failed.
  running: boolean
  sending: boolean
  stopping: boolean
  error: string
  // status is whether Claude Code can run a turn now, undefined until it is read; checking
  // reports it read again, as Check again asks.
  status: DescribeStatus | undefined
  checking: boolean
  onCheck: () => void
  onSend: () => void
  onStop: () => void
}

// Composer is where the user writes to Claude: what they attach, as chips, their words,
// which ⌘↩ sends, and Send, or Stop while a turn runs; under it, what Claude sees of them.
// While Claude Code is missing, too old, or signed out, it is off, and says why and the fix.
export function Composer({
  text,
  onText,
  chips,
  onRemove,
  running,
  sending,
  stopping,
  error,
  status,
  checking,
  onCheck,
  onSend,
  onStop,
}: ComposerProps) {
  const id = useId()
  const notReady = status !== undefined && !status.ready
  const canSend = !notReady && !running && !sending && text.trim() !== ''

  const send = () => {
    if (canSend) {
      onSend()
    }
  }

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && e.metaKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      send()
    }
  }

  return (
    <form
      aria-label="Your message"
      onSubmit={(e) => {
        e.preventDefault()
        send()
      }}
      className="sticky bottom-0 flex flex-col gap-2 border-t border-border bg-background pt-3 pb-1"
    >
      {notReady && (
        <div
          role="alert"
          className="flex flex-wrap items-start gap-3 rounded-md border border-warning/60 bg-warning/10 px-3 py-2 text-sm"
        >
          <AlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <p>{status.problem}</p>
            {status.fix !== '' && <p className="text-xs text-muted">{status.fix}</p>}
          </div>
          <button type="button" disabled={checking} onClick={onCheck} className={buttonClass}>
            {checking ? 'Checking…' : 'Check again'}
          </button>
        </div>
      )}
      {chips.length > 0 && (
        <ul aria-label="Attached to your message" className="flex flex-wrap gap-1.5">
          {chips.map((c) => (
            <li
              key={`${c.ref.kind}:${String(c.ref.id)}`}
              className="inline-flex items-center gap-1 rounded-full border border-border bg-surface py-0.5 pr-1 pl-2.5 text-xs text-text"
            >
              {c.label}
              <button
                type="button"
                aria-label={`Remove ${c.label}`}
                onClick={() => {
                  onRemove(c.ref)
                }}
                className="inline-flex size-4 items-center justify-center rounded-full text-muted hover:bg-raised hover:text-text"
              >
                <XIcon className="size-3" />
              </button>
            </li>
          ))}
        </ul>
      )}
      <label htmlFor={id} className="sr-only">
        Tell Autopilot what you want
      </label>
      <textarea
        id={id}
        rows={3}
        value={text}
        disabled={notReady}
        placeholder="Tell Autopilot what you want…"
        aria-describedby={`${id}-sees`}
        onChange={(e) => {
          onText(e.target.value)
        }}
        onKeyDown={onKeyDown}
        className={`${fieldClass} block w-full resize-y leading-relaxed`}
      />
      <div className="flex flex-wrap items-center gap-3">
        <p id={`${id}-sees`} className="min-w-0 flex-1 text-xs text-muted">
          {describeSeesText}
        </p>
        {running ? (
          <button type="button" disabled={stopping} onClick={onStop} className={buttonClass}>
            <StopIcon className="size-3.5 text-danger" />
            {stopping ? 'Stopping…' : 'Stop'}
          </button>
        ) : (
          <button type="submit" disabled={!canSend} className={primaryButtonClass}>
            {sending ? 'Sending…' : 'Send'}
            <kbd aria-hidden="true" className="font-sans text-xs text-muted">
              ⌘↩
            </kbd>
          </button>
        )}
      </div>
      <ErrorText error={error} />
    </form>
  )
}
