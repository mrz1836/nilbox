import { useEffect, useState } from 'react'

import { brand } from '../generated/brand'
import { getAppInfo, type DataReset } from '../platform'
import { AlertIcon, XIcon } from './icons'

// DataResetNotice tells the user, once the window loads, that their data could not be
// read at startup: the database was set aside and the app started with an empty one. The
// shell reports it through the app info, since it happens before the window exists. The
// notice stays until it is dismissed, and only for this run: the next start reads a
// working database. The live region stays mounted, so the notice is announced when it
// appears.
export function DataResetNotice() {
  const [reset, setReset] = useState<DataReset | null>(null)

  useEffect(() => {
    let active = true

    getAppInfo()
      .then((info) => {
        if (active) {
          setReset(info.dataReset ?? null)
        }
      })
      .catch((err: unknown) => {
        console.error('could not read the app info', err)
      })

    return () => {
      active = false
    }
  }, [])

  return (
    <div role="status" aria-live="polite">
      {reset !== null && (
        <div className="mx-4 mt-4 flex items-start gap-2.5 rounded-md border border-warning py-2 pr-1.5 pl-3 text-sm">
          <AlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
          <p className="min-w-0 flex-1 py-px">
            {brand.name} couldn’t read its data, so it set it aside at{' '}
            <code className="font-mono text-xs break-all">{reset.path}</code> and started fresh.
          </p>
          <button
            type="button"
            aria-label="Dismiss notice"
            title="Dismiss"
            onClick={() => {
              setReset(null)
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
