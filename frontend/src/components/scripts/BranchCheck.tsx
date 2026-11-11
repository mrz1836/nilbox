import { useEffect, useEffectEvent, useRef, useState, type ReactNode } from 'react'

import { attempt } from '../../lib/call'
import { isLive } from '../../lib/scriptRuns'
import { formatDateTime } from '../../lib/time'
import { useMutation } from '../../lib/useMutation'
import { useQuery } from '../../lib/useQuery'
import { getBranchStatus, getScriptRun, on, pullBranch, type BranchState, type BranchStatus } from '../../platform'
import { quietButtonClass } from '../controls'
import { AlertIcon, CheckIcon, ImportIcon, RefreshIcon } from '../icons'
import { Spinner } from '../Spinner'

// stateLabels are what the header says of each state, in a few words; the status's note
// says it in full, in the label's tooltip.
const stateLabels: Readonly<Record<BranchState, string>> = {
  upToDate: 'Up to date',
  behind: 'Update available',
  ahead: 'Ahead of origin',
  diverged: 'Diverged from origin',
  detached: 'No branch checked out',
  noUpstream: 'Not tracking origin',
  inProgress: 'git is in the middle of something',
  unknown: 'Couldn’t check origin',
  off: 'Scripts are off',
}

// statusTitle is a status in full, for its tooltip: its note, and when origin was asked.
function statusTitle(status: BranchStatus): string {
  return status.checkedAt === '' ? status.note : `${status.note} Checked ${formatDateTime(status.checkedAt)}.`
}

// StateLabel is what the header says of a status: a check for up to date, an alert for a
// status that couldn't be worked out, which says why in full, and a few words for the
// rest, the note in their tooltip.
function StateLabel({ status }: { status: BranchStatus }) {
  let icon: ReactNode = null
  let tone = 'text-muted'
  let text = stateLabels[status.state]

  if (status.state === 'upToDate') {
    icon = <CheckIcon className="size-3.5 text-muted" />
  } else if (status.state === 'behind') {
    tone = 'text-text'
  } else if (status.state === 'unknown') {
    icon = <AlertIcon className="size-3.5 shrink-0 text-warning" />
    text = status.note
  }

  return (
    <span title={statusTitle(status)} className={`inline-flex items-center gap-1 ${tone}`}>
      {icon}
      {text}
    </span>
  )
}

interface BranchCheckProps {
  // dir is the clone, with ~.
  dir: string
  // onStarted shows a pull that started, as a script's run shows; onPulled hears that it
  // ended, so the page reads the clone's scripts again.
  onStarted: (id: number) => void
  onPulled: () => void
}

// BranchCheck says, beside the branch in Repo scripts' header, whether origin has newer
// commits on the branch the clone follows, and offers Pull only then: git's
// fast-forward-only pull, which shows as a run beside the scripts. Origin's answer is kept
// five minutes, and the refresh button asks it again. Once a pull ends, the status is read
// again, from the answer the pull's own look at origin left, and the page's scripts too.
export function BranchCheck({ dir, onStarted, onPulled }: BranchCheckProps) {
  // check counts the checks after the first: refresh asks origin again.
  const [check, setCheck] = useState({ n: 0, refresh: false })
  const query = useQuery(() => getBranchStatus(dir, check.refresh), [dir, check.n])
  const pull = useMutation(() => pullBranch(dir))
  // pulling is the pull started here, until it ends, or null; settled is the last pull
  // whose end was heard, so an end heard twice is acted on once.
  const [pulling, setPulling] = useState<number | null>(null)
  const settled = useRef<number | null>(null)
  const status = query.data
  // checkAgain reads the status again, from the latest count, asking origin when refresh is set.
  const checkAgain = (refresh: boolean) => {
    setCheck((was) => ({ n: was.n + 1, refresh }))
  }

  // settle reads the pull id, and once it has ended, reads the status and the scripts again.
  const settle = useEffectEvent(async (id: number) => {
    const read = await attempt(getScriptRun(id))

    if (!read.ok || isLive(read.data.status) || settled.current === id) {
      return
    }

    settled.current = id
    setPulling((was) => (was === id ? null : was))
    checkAgain(false)
    onPulled()
  })

  // The pull is read once now, in case it ended before this listened, then again each time
  // the runs change, until it ends.
  useEffect(() => {
    if (pulling === null) {
      return undefined
    }

    void settle(pulling)

    return on('scriptruns:updated', () => {
      void settle(pulling)
    })
  }, [pulling])

  const startPull = async () => {
    const started = await pull.run()

    if (started.ok) {
      setPulling(started.data)
      onStarted(started.data)
    }
  }

  if (status === undefined) {
    return query.error === '' ? (
      <span className="inline-flex items-center gap-1">
        <Spinner className="size-3" />
        Checking origin…
      </span>
    ) : (
      <span className="text-danger">Couldn’t check the branch: {query.error}</span>
    )
  }

  const checking = query.pending
  const busy = pull.pending || pulling !== null

  return (
    <span className="inline-flex flex-wrap items-center gap-x-1.5">
      <StateLabel status={status} />
      {status.state === 'behind' && (
        <button
          type="button"
          disabled={busy}
          aria-label={busy ? `Pulling ${status.branch}` : `Pull ${status.branch}`}
          title={`Fast-forward ${status.branch} to origin/${status.upstream}`}
          onClick={() => {
            void startPull()
          }}
          className={quietButtonClass}
        >
          {busy ? <Spinner className="size-3" /> : <ImportIcon className="size-3.5" />}
          {busy ? 'Pulling…' : 'Pull'}
        </button>
      )}
      <button
        type="button"
        disabled={checking || busy}
        aria-label="Check origin again"
        title="Ask origin again whether it has newer commits"
        onClick={() => {
          checkAgain(true)
        }}
        className={quietButtonClass}
      >
        <RefreshIcon className={`size-3.5 ${checking ? 'motion-safe:animate-spin' : ''}`} />
      </button>
      {pull.error !== '' && (
        <span role="alert" className="text-danger">
          Couldn’t pull {status.branch}: {pull.error}
        </span>
      )}
    </span>
  )
}
