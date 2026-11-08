import { useState } from 'react'

import { brand } from '../generated/brand'
import { counted } from '../lib/format'
import { useQuery } from '../lib/useQuery'
import { getAppInfo, type DataReset, type Restored } from '../platform'
import { AlertIcon, CheckIcon, XIcon } from './icons'

// Path shows a file's path in the notice.
function Path({ path }: { path: string }) {
  return <code className="font-mono text-xs break-all">{path}</code>
}

// cameBack names what was brought back: "your prompt", or "your 4 prompts and 2 launch
// defaults".
function cameBack(r: Restored): string {
  if (r.prompts + r.launchDefaults === 1) {
    return r.prompts === 1 ? 'your prompt' : 'your launch default'
  }

  const parts = [
    ...(r.prompts > 0 ? [counted(r.prompts, 'prompt', 'prompts')] : []),
    ...(r.launchDefaults > 0 ? [counted(r.launchDefaults, 'launch default', 'launch defaults')] : []),
  ]

  return `your ${parts.join(' and ')}`
}

// Message words what happened to the data at startup.
function Message({ reset, restored }: { reset: DataReset | null; restored: Restored | null }) {
  if (reset === null) {
    return restored === null ? null : (
      <>
        {brand.name} started with a new database and brought back {cameBack(restored)} from its saved copy at{' '}
        <Path path={restored.from} />.
      </>
    )
  }

  return (
    <>
      {brand.name} couldn’t read its data, so it set it aside at <Path path={reset.path} /> and started fresh.{' '}
      {restored === null
        ? 'To bring back prompts from it, choose Restore from a backup on the Prompts page.'
        : `It brought back ${cameBack(restored)}.`}
    </>
  )
}

// DataResetNotice tells the user, once the window loads, what happened to their data at
// startup: that the database could not be read, so it was set aside and the app started
// with an empty one, and whether their prompts and launch defaults were brought back,
// from the database set aside or from the copies kept beside it. The shell reports it
// through the app info, since it happens before the window exists. The notice stays until
// it is dismissed, and only for this run: the next start reads a working database. The
// live region stays mounted, so the notice is announced when it appears.
export function DataResetNotice() {
  const info = useQuery(() => getAppInfo(), [], { log: 'could not read the app info' })
  const [dismissed, setDismissed] = useState(false)
  const reset = dismissed ? null : (info.data?.dataReset ?? null)
  const restored = dismissed ? null : (info.data?.restored ?? null)
  // A reset that brought nothing back warns; prompts brought back on their own reassure.
  const warning = reset !== null && restored === null

  return (
    <div role="status" aria-live="polite">
      {(reset !== null || restored !== null) && (
        <div
          className={`mx-4 mt-4 flex items-start gap-2.5 rounded-md border py-2 pr-1.5 pl-3 text-sm ${warning ? 'border-warning' : 'border-success'}`}
        >
          {warning ? (
            <AlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
          ) : (
            <CheckIcon className="mt-0.5 size-4 shrink-0 text-success" />
          )}
          <p className="min-w-0 flex-1 py-px">
            <Message reset={reset} restored={restored} />
          </p>
          <button
            type="button"
            aria-label="Dismiss notice"
            title="Dismiss"
            onClick={() => {
              setDismissed(true)
            }}
            className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-surface hover:text-text"
          >
            <XIcon className="size-3.5" />
          </button>
        </div>
      )}
    </div>
  )
}
