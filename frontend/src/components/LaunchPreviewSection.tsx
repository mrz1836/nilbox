import { useId, useState } from 'react'

import { launchRequest, type Choices } from '../lib/launchChoices'
import { useDebouncedQuery } from '../lib/useQuery'
import { previewLaunch, type InboxItem, type LaunchPreview } from '../platform'
import { ChevronRightIcon } from './icons'

export interface LaunchPreviewSectionProps {
  item: InboxItem
  // choices are the launch menu's choices, which the preview plans with.
  choices: Choices
  // untrusted marks an outside contributor's item, whose override the choices may set.
  untrusted: boolean
}

// PreviewState is what the preview shows: the plan still on its way, the plan, or why
// there is none.
type PreviewState =
  { kind: 'loading' } | { kind: 'ready'; preview: LaunchPreview } | { kind: 'failed'; message: string }

// LaunchPreviewSection is the launch menu's Preview: on request, it shows exactly what the
// launch would run, which is the filled-in prompt, where it starts, and the command. It
// reloads as the choices change.
export function LaunchPreviewSection({ item, choices, untrusted }: LaunchPreviewSectionProps) {
  const id = useId()
  const [open, setOpen] = useState(false)
  // The plan is asked for again, once the choices rest, while the preview is open. While
  // they rest, the preview shows how the last ask went; once asked, it says it is planning.
  const planned = useDebouncedQuery(
    () => previewLaunch(launchRequest(item.id, choices, untrusted)),
    [item.id, choices, untrusted],
    120,
    { enabled: open },
  )
  const state: PreviewState =
    planned.pending && !planned.waiting
      ? { kind: 'loading' }
      : planned.error !== ''
        ? { kind: 'failed', message: planned.error }
        : planned.data === undefined
          ? { kind: 'loading' }
          : { kind: 'ready', preview: planned.data }

  return (
    <div className="border-t border-border">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={`${id}-preview`}
        onClick={() => {
          setOpen((was) => !was)
        }}
        className="flex w-full items-center gap-1.5 px-3.5 py-2 text-left text-[13px] text-muted transition-colors hover:text-text"
      >
        <ChevronRightIcon className={`size-3.5 transition-transform ${open ? 'rotate-90' : ''}`} />
        Preview
      </button>
      {open && (
        <div id={`${id}-preview`} className="flex flex-col gap-2 px-3.5 pb-3">
          {state.kind === 'loading' ? (
            <p className="text-xs text-muted">Planning the launch…</p>
          ) : state.kind === 'failed' ? (
            <p role="alert" className="text-xs text-danger">
              {state.message}
            </p>
          ) : (
            <>
              <pre className="max-h-44 overflow-y-auto rounded-md border border-border bg-background p-2.5 font-mono text-[11px] leading-[1.35rem] break-words whitespace-pre-wrap">
                {state.preview.prompt}
              </pre>
              <p className="text-xs text-muted">
                In <span className="font-mono text-text">{state.preview.dir}</span>
                {state.preview.worktree !== '' && (
                  <>
                    {' '}
                    · worktree <span className="font-mono text-text">{state.preview.worktree}</span>
                  </>
                )}{' '}
                · {state.preview.permission}
              </p>
              <code className="block rounded-md bg-raised px-2 py-1.5 font-mono text-[11px] leading-snug break-all text-text">
                {state.preview.command}
              </code>
            </>
          )}
        </div>
      )}
    </div>
  )
}
