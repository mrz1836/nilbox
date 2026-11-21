import { useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { useMutation } from '../../lib/useMutation'
import { takeOverRun, type LaunchHold, type LaunchResult, type TakeOverRequest } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { ErrorText } from '../controls'
import { LaunchHoldDialog } from '../LaunchHoldDialog'

export interface TakeOverDialogProps {
  // sessionId is the run's session, whose conversation resumes.
  sessionId: string
  // live reports a run still working, which taking over stops first.
  live: boolean
  // terminal names the terminal the conversation opens in, as in "iTerm2", and where the
  // worktree it runs in ("pr-88", or '' for none known).
  terminal: string
  where: string
  // agentName names the agent, as in "Claude".
  agentName: string
  // onTaken hears the resumed session's launch, and onCancel that nothing was taken over.
  onTaken: (result: LaunchResult) => void
  onCancel: () => void
}

// TakeOverDialog asks before taking an agent's background run over: the app stops the
// agent, while it still works, and opens its conversation in the user's terminal, in its
// worktree, where they carry on. It says the run is no longer unattended from then on: it
// keeps its fence, and asks the user about whatever the fence doesn't allow rather than
// refusing it, unless they tick the profile's permissions for this one session, which the
// trust rule still holds an outside contributor's item to planning under. A launch held
// back, as for the pull request's files, asks as any launch does, and its confirmation
// takes over again with exactly that confirmed.
export function TakeOverDialog({
  sessionId,
  live,
  terminal,
  where,
  agentName,
  onTaken,
  onCancel,
}: TakeOverDialogProps) {
  const id = useId()
  const [profile, setProfile] = useState(false)
  const taker = useMutation((request: TakeOverRequest) => takeOverRun(request))
  const busy = taker.pending
  // held is the take over held back for a hold, with the request to send again.
  const [held, setHeld] = useState<{ hold: LaunchHold; request: TakeOverRequest } | null>(null)

  // take takes the run over with request; its failure shows as taker's error.
  const take = async (request: TakeOverRequest) => {
    setHeld(null)

    const result = await taker.run(request)

    if (!result.ok) {
      return
    }

    if (result.data.hold !== null) {
      setHeld({ hold: result.data.hold, request })

      return
    }

    onTaken(result.data)
  }

  if (held !== null) {
    return (
      <LaunchHoldDialog
        hold={held.hold}
        agentName={agentName}
        onCancel={onCancel}
        onConfirm={() => {
          const { hold, request } = held

          void take(
            hold.reason === 'running' ? { ...request, alongside: hold.key } : { ...request, confirmed: hold.key },
          )
        }}
      />
    )
  }

  const inWhere = where === '' ? '' : `, in ${where}`

  return (
    <ConfirmDialog
      title={live ? 'Take over this run?' : 'Take over this conversation?'}
      confirmLabel={busy ? (live ? 'Stopping the agent…' : 'Opening…') : 'Take over'}
      tone="primary"
      busy={busy}
      onConfirm={() => {
        void take({ sessionId, profile, alongside: '', confirmed: '' })
      }}
      onCancel={onCancel}
    >
      <div className="flex flex-col gap-3">
        <p>
          {live
            ? `${brand.name} stops the agent and opens its conversation in ${terminal}${inWhere}, where you can carry on.`
            : `${brand.name} opens its conversation in ${terminal}${inWhere}, where you can carry on.`}
        </p>
        <p>
          It’s no longer unattended once you take over. It keeps the run’s fence, and asks you about anything the fence
          doesn’t allow instead of refusing it.
        </p>
        <label htmlFor={`${id}-profile`} className="flex items-start gap-2 text-sm text-text">
          <input
            id={`${id}-profile`}
            type="checkbox"
            checked={profile}
            disabled={busy}
            onChange={(e) => {
              setProfile(e.target.checked)
            }}
            className="checkbox mt-0.5"
          />
          <span>
            Use the profile’s permissions for this session
            <span className="block text-xs text-muted">
              Only for this session. An outside contributor’s item still only plans.
            </span>
          </span>
        </label>
        <ErrorText error={taker.error} />
      </div>
    </ConfirmDialog>
  )
}
