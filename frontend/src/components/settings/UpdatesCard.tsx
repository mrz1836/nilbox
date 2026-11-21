import { useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { attempt } from '../../lib/call'
import { formatAgo } from '../../lib/time'
import { aboutLine, locationWords, sentence, type Offer, type RestartStep } from '../../lib/update'
import type { MoveControl } from '../../lib/useMove'
import type { UpdateControl } from '../../lib/useUpdate'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  checkForUpdates,
  openExternal,
  setUpdateMode,
  showInstallLog,
  type Settings,
  type UpdateMode,
  type UpdateStatus,
} from '../../platform'
import { buttonClass, chipClass, ErrorText, fieldClass, primaryButtonClass } from '../controls'
import { AlertIcon, ExternalIcon, FolderIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { rowMatchClass, useSetting } from './match'
import { Group, joinIds, rowDescribedBy, SettingRow } from './rows'

// modes are the choices of what to do when a new version is out, in the order Settings
// offers them.
const modes: readonly { value: UpdateMode; label: string }[] = [
  { value: 'download', label: 'Download it and tell me' },
  { value: 'notify', label: 'Just tell me' },
  { value: 'off', label: 'Don’t check' },
]

// noteOf is what the Updates row says under the status: a moment's wait while the last
// update finishes, quietly, or the last failure.
function noteOf(flow: RestartStep, failure: string): { text: string; tone: string } {
  return flow.step === 'waiting'
    ? { text: sentence(flow.message), tone: 'text-muted' }
    : { text: failure, tone: 'text-danger' }
}

// StatusLine says where the update stands: the words, how far a download has got, a check
// that found nothing new with how long ago it ended, and a note under them; nothing until
// the status has loaded. The words and the note are live regions, so Check for Updates'
// result is read out where it shows, but a download's climbing percentage and the time
// ago are not.
function StatusLine({ status, flow, id }: { status: UpdateStatus | null; flow: RestartStep; id: string }) {
  const line = status === null ? { text: '', progress: null, checkedAt: '', note: '' } : aboutLine(status)
  const note = noteOf(flow, line.note)

  return (
    <div id={id} className="mt-0.5 text-xs text-muted">
      <p className="min-h-4">
        <span role="status">{line.text}</span>
        {line.progress !== null && <span className="tabular-nums"> {line.progress}%</span>}
        {line.checkedAt !== '' && (
          <span>
            {' · checked '}
            <RelativeTime iso={line.checkedAt} show={formatAgo} />
          </span>
        )}
      </p>
      <div role="status">{note.text !== '' && <p className={`mt-0.5 ${note.tone}`}>{note.text}</p>}</div>
    </div>
  )
}

// UpdatesRow is the Updates row: the build's update status, Check for Updates, and what
// the update offers (offerOf), as the banner offers it: Restart to update, Update, or Try
// again, which run the window's one restart, and Download manually. In a narrow window the
// buttons wrap under the status, as the banner's do, so its words keep their room.
function UpdatesRow({ update }: { update: UpdateControl }) {
  const id = useId()
  const [marks] = useSetting('Updates')
  const { status, flow, offer } = update
  const [checking, setChecking] = useState(false)
  const [error, setError] = useState('')
  const enabled = status?.enabled ?? false
  const state = status?.state ?? 'idle'
  // busy is any step of the update under way, a moment's wait and the app quitting on its
  // own to finish an update included, which holds the buttons.
  const busy =
    checking ||
    (status?.checking ?? false) ||
    state === 'downloading' ||
    state === 'installing' ||
    state === 'finishing' ||
    (flow.step !== 'idle' && flow.step !== 'failed')

  const check = async () => {
    setError('')
    setChecking(true)

    // A check that fails says so in the status; only a call that can't check rejects.
    const result = await attempt(checkForUpdates())

    setChecking(false)

    if (!result.ok) {
      setError(result.error)
    }

    void update.reload()
  }

  return (
    <div
      {...marks}
      className={`flex flex-wrap items-center justify-between gap-x-6 gap-y-3 px-4 py-3.5 ${rowMatchClass}`}
    >
      <div className="min-w-0 flex-[1_1_16rem]">
        <p className="flex items-center gap-2 text-sm font-medium">
          Updates
          {status?.flavor === 'rehearsal' && <span className={chipClass}>Rehearsal build</span>}
        </p>
        <StatusLine status={status} flow={flow} id={`${id}-status`} />
        <ErrorText id={`${id}-error`} error={error} className="mt-1" />
      </div>
      {enabled && (
        <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-2">
          <button
            type="button"
            disabled={busy}
            aria-describedby={joinIds(`${id}-status`, error !== '' && `${id}-error`)}
            onClick={() => {
              void check()
            }}
            className={buttonClass}
          >
            {checking || status?.checking === true ? 'Checking…' : 'Check for Updates'}
          </button>
          {offer.page !== '' && (
            <button
              type="button"
              onClick={() => {
                openExternal(offer.page)
              }}
              className={buttonClass}
            >
              Download manually
              <ExternalIcon className="size-3.5 text-muted" />
            </button>
          )}
          {offer.action !== null && (
            <button type="button" disabled={busy} onClick={update.restart} className={primaryButtonClass}>
              {offer.label}
            </button>
          )}
        </div>
      )}
    </div>
  )
}

// InstallFailedRow says the update the last restart didn't install, why, and shows its log
// in Finder when one was kept.
function InstallFailedRow({ status }: { status: UpdateStatus }) {
  const [error, setError] = useState('')
  const failed = status.installFailed

  const showLog = async () => {
    setError('')

    const result = await attempt(showInstallLog())

    if (!result.ok) {
      setError(result.error)
    }
  }

  return (
    <div className="flex items-center justify-between gap-6 px-4 py-3.5">
      <div className="flex min-w-0 gap-2.5">
        <AlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
        <div className="min-w-0">
          <p className="text-sm font-medium">The update to {failed.version} didn’t install</p>
          <p className="mt-0.5 text-xs text-muted">{failed.reason}</p>
          {!failed.retryable && (
            <p className="mt-0.5 text-xs text-muted">
              Trying it again would fail the same way, so download it from its release page.
            </p>
          )}
          <ErrorText error={error} className="mt-1" />
        </div>
      </div>
      {failed.log && (
        <button
          type="button"
          onClick={() => {
            void showLog()
          }}
          className={`${buttonClass} shrink-0`}
        >
          <FolderIcon className="size-3.5 text-muted" />
          Show log in Finder
        </button>
      )}
    </div>
  )
}

// LocationRow says the app can't update where it runs, and how to let it: Move to
// Applications, wherever the update's offer has it (offerOf, as the banner and the move
// dialog offer it), which runs the window's one move (lib/useMove.ts), held while it is
// under way.
function LocationRow({ status, offer, move }: { status: UpdateStatus; offer: Offer; move: MoveControl }) {
  const { title, hint } = locationWords(status.location)

  return (
    <div className="flex items-center justify-between gap-6 px-4 py-3.5">
      <div className="flex min-w-0 gap-2.5">
        <AlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
        <div className="min-w-0">
          <p className="text-sm font-medium">{title}</p>
          <p className="mt-0.5 text-xs text-muted">{hint}</p>
        </div>
      </div>
      {offer.move && (
        <button
          type="button"
          disabled={move.dialog !== null && move.dialog.work !== null}
          onClick={move.move}
          className={`${primaryButtonClass} shrink-0`}
        >
          Move to Applications
        </button>
      )}
    </div>
  )
}

// ModeRow chooses what happens when a new version is out. A build that doesn't update
// keeps the choice, for a build that does, and shows it held.
function ModeRow({
  settings: stored,
  onSaved,
  status,
  onChanged,
}: {
  settings: Settings
  onSaved: (settings: Settings) => void
  status: UpdateStatus | null
  onChanged: () => void
}) {
  const id = useId()
  const controlId = `${id}-mode`
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const off = status !== null && !status.enabled

  return (
    <SettingRow
      label="When a new version is out"
      controlId={controlId}
      description={
        off
          ? 'Kept for builds that update themselves.'
          : `${brand.name} looks a minute after it opens, then every 6 hours. Check for Updates works with every choice.`
      }
      error={error}
    >
      <select
        id={controlId}
        aria-describedby={rowDescribedBy(controlId, error)}
        disabled={off}
        value={settings.updateMode}
        onChange={(e) => {
          const mode = modes.find((m) => m.value === e.target.value)?.value

          if (mode !== undefined) {
            void save({ updateMode: mode }, () => setUpdateMode(mode)).then((result) => {
              if (result.ok) {
                onChanged()
              }
            })
          }
        }}
        className={fieldClass}
      >
        {modes.map((m) => (
          <option key={m.value} value={m.value}>
            {m.label}
          </option>
        ))}
      </select>
    </SettingRow>
  )
}

export interface UpdatesCardProps {
  update: UpdateControl
  // settings is null until it has loaded; the choice of what to do shows once it has.
  settings: Settings | null
  onSaved: (settings: Settings) => void
}

// UpdatesCard is Settings → About's updates, under the version: where the update stands,
// Check for Updates with its result in place, Restart to update or Update once a newer
// version is found, why the last restart didn't install with its log, why the app can't
// update where it runs, and what to do when a new version is out.
export function UpdatesCard({ update, settings, onSaved }: UpdatesCardProps) {
  const { status } = update
  const enabled = status?.enabled ?? false

  return (
    <Group>
      <UpdatesRow update={update} />
      {enabled && status !== null && status.installFailed.version !== '' && <InstallFailedRow status={status} />}
      {enabled && status !== null && !status.location.ok && (
        <LocationRow status={status} offer={update.offer} move={update.move} />
      )}
      {settings !== null && (
        <ModeRow
          settings={settings}
          onSaved={onSaved}
          status={status}
          onChanged={() => {
            void update.reload()
          }}
        />
      )}
    </Group>
  )
}
