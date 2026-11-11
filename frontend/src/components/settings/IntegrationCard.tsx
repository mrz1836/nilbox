import { startTransition, useId, useOptimistic, useState, useTransition, type ReactNode, type SubmitEvent } from 'react'

import { brand } from '../../generated/brand'
import { attempt, start, type Result } from '../../lib/call'
import { exportLine } from '../../lib/shell'
import { formatDateTime } from '../../lib/time'
import {
  chooseFileToOpen,
  installIntegration,
  openExternal,
  reinstallIntegration,
  setIntegrationEnabled,
  setIntegrationPath,
  trashToolCopy,
  updateIntegration,
  type InstallProgress,
  type InstallResult,
  type IntegrationID,
  type IntegrationSwitch,
  type IntegrationTool,
  type IntegrationTools,
  type Settings,
  type TerminalID,
  type ToolCopy,
  type ToolSource,
  type ToolState,
} from '../../platform'
import { CommandLine } from '../CommandLine'
import { buttonClass, fieldClass, primaryButtonClass, quietButtonClass, Switch } from '../controls'
import { AlertIcon, CheckIcon, ChevronRightIcon, ExternalIcon, XIcon } from '../icons'
import { RunPanel } from '../scripts/RunPanel'
import { Spinner } from '../Spinner'
import { InstallDialog, type InstallKind } from './InstallDialog'
import { cardMatchClass, headingMatchClass, useSetting } from './match'
import { ErrorText, joinIds } from './rows'
import { TrashCopy } from './TrashCopy'

// sourceLabels names where a copy of a tool came from.
const sourceLabels: Readonly<Record<ToolSource, string>> = {
  app: `installed by ${brand.name}`,
  release: 'GitHub release',
  homebrew: 'Homebrew',
  goInstall: 'go install',
  development: 'development build',
}

// stateIcons mark a tool's status line: found, too old, or not usable at all.
const stateIcons: Readonly<Record<ToolState, ReactNode>> = {
  found: <CheckIcon className="mt-px size-3.5 shrink-0 text-success" />,
  update: <CheckIcon className="mt-px size-3.5 shrink-0 text-success" />,
  tooOld: <AlertIcon className="mt-px size-3.5 shrink-0 text-warning" />,
  wontRun: <XIcon className="mt-px size-3.5 shrink-0 text-danger" />,
  missing: <XIcon className="mt-px size-3.5 shrink-0 text-muted" />,
}

// mainKinds are what the card's button does in each state: install a missing tool, update
// one with a newer release or too old to drive, or install one that won't run again. A
// tool that is found has no button; Details offers to reinstall a copy the app installed.
const mainKinds: Readonly<Record<ToolState, InstallKind | null>> = {
  found: null,
  update: 'update',
  tooOld: 'update',
  wontRun: 'reinstall',
  missing: 'install',
}

// installers are the calls that do each kind of install.
const installers: Readonly<Record<InstallKind, (id: IntegrationID) => PromiseLike<InstallResult>>> = {
  install: installIntegration,
  update: updateIntegration,
  reinstall: reinstallIntegration,
}

// buttonLabel is the card's button, naming the version to install once it is known.
function buttonLabel(kind: InstallKind, tool: IntegrationTool): string {
  const { version } = tool.install

  switch (kind) {
    case 'install':
      return `Install ${tool.name} ${version}`.trimEnd()
    case 'update':
      return version === '' ? `Update ${tool.name}` : `Update to ${version}`
    case 'reinstall':
      return 'Reinstall'
  }
}

// megabytes is a number of bytes in megabytes, to one decimal, as Finder counts them.
function megabytes(bytes: number): string {
  return (bytes / 1_000_000).toFixed(1)
}

// progressText is how far a running install has got: the download, in megabytes once its
// size is known, then the checksum, a trial run, and putting it in place; or the tool's
// own updater, running.
function progressText({ step, done, total }: InstallProgress, binary: string): string {
  switch (step) {
    case 'downloading':
      return total > 0 ? `Downloading… ${megabytes(done)} of ${megabytes(total)} MB` : 'Downloading…'
    case 'verifying':
      return 'Checking the checksum…'
    case 'trying':
      return 'Trying it…'
    case 'updating':
      return `${binary} is updating itself…`
    default:
      return 'Installing…'
  }
}

// folderOf is the folder a copy is in, as its display path names it.
function folderOf(copy: ToolCopy): string {
  return copy.displayPath.slice(0, copy.displayPath.lastIndexOf('/'))
}

// switchCaption says what a tool's switch means: on or off as the user set it, or, while
// automatic, on because the tool was found, or off because it wasn't.
function switchCaption(set: IntegrationSwitch, on: boolean): string {
  if (set === 'auto') {
    return on ? 'On · found automatically' : 'Off'
  }

  return set === 'on' ? 'On' : 'Off'
}

// whereText is where a copy is, and where it points when it is a link.
function whereText(copy: ToolCopy): string {
  return copy.link ? `${copy.displayPath} → ${copy.target}` : copy.displayPath
}

// statusText is a tool's status line, by its state: the copy in use and where it came
// from, or what is wrong with it.
function statusText(tool: IntegrationTool): string {
  const { copy } = tool

  switch (tool.state) {
    case 'found':
    case 'update':
      return `${tool.binary} ${copy.version.raw} · ${whereText(copy)} · ${sourceLabels[copy.source]}`
    case 'tooOld':
      return `${tool.binary} ${copy.version.raw} is older than ${tool.minVersion}, which ${brand.name} needs`
    case 'wontRun':
      return `${copy.displayPath} is there but won’t run: ${copy.error}`
    case 'missing':
      return 'Not on this Mac'
  }
}

// versionText is a copy's version with the commit it was built from and when, as far as
// it printed them.
function versionText(copy: ToolCopy): string {
  const { raw, commit, built } = copy.version

  return [raw, commit !== '' && `commit ${commit}`, built !== '' && `built ${built}`].filter(Boolean).join(' · ')
}

// otherText is another copy of a tool, as Details lists it: its version and where it came
// from, or why it won't run.
function otherText(copy: ToolCopy): string {
  const source = sourceLabels[copy.source]

  return copy.runs
    ? `${copy.displayPath} ${copy.version.raw} (${source})`
    : `${copy.displayPath} (${source}) won’t run: ${copy.error}`
}

// LinkButton opens a page on GitHub in the browser.
function LinkButton({ url, children }: { url: string; children: string }) {
  return (
    <button
      type="button"
      title={url}
      onClick={() => {
        openExternal(url)
      }}
      className="inline-flex items-center gap-1 font-medium text-accent hover:underline"
    >
      {children}
      <ExternalIcon className="size-3" />
    </button>
  )
}

interface DetailsProps {
  tool: IntegrationTool
  id: string
  // busy holds the controls while a change is stored or an install runs.
  busy: boolean
  // save stores a change and resolves with how it went.
  save: (call: () => PromiseLike<Settings>) => Promise<Result<Settings>>
  // reinstall asks to install the latest release over the copy the app installed, or is
  // null when it can't.
  reinstall: (() => void) | null
  // trash moves a copy to the Trash and resolves with how it went.
  trash: (path: string) => Promise<Result<unknown>>
}

// Details is what a card shows once opened: the copy in use, with, for one the app
// installed, when and from which archive, and a way to reinstall it or move it to the
// Trash; the other copies found, with a way to move the one the terminal runs instead to
// the Trash; a way to use a different copy; and the tool's releases.
function Details({ tool, id, busy, save, reinstall, trash }: DetailsProps) {
  const { copy } = tool
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  const pathId = `${id}-path`
  const errorId = `${pathId}-error`
  // rows describe the copy in use, while there is one: its version only when it ran.
  const rows: [string, string][] = []

  if (copy.path !== '') {
    rows.push(['Path', whereText(copy)], ['Source', sourceLabels[copy.source]])

    if (copy.runs) {
      rows.push(['Version', versionText(copy)])
    }
  }

  // storePath makes path the copy in use, or, for '', the one the shell finds.
  const storePath = async (path: string) => {
    setError('')

    const result = await save(() => setIntegrationPath(tool.id, path))

    if (result.ok) {
      setDraft('')
    } else {
      setError(result.error)
    }
  }

  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault()
    void storePath(draft.trim())
  }

  const choose = async () => {
    const chosen = await attempt(
      chooseFileToOpen({ title: `Choose a copy of ${tool.binary}`, button: 'Use', filters: [] }),
    )

    if (!chosen.ok) {
      setError(chosen.error)
    } else if (chosen.data !== '') {
      setDraft(chosen.data)
      await storePath(chosen.data)
    }
  }

  return (
    <div id={`${id}-details`} className="flex flex-col gap-4 px-4 pb-4">
      {rows.length > 0 && (
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
          {rows.map(([term, value]) => (
            <div key={term} className="contents">
              <dt className="text-muted">{term}</dt>
              <dd className="font-mono break-all text-text">{value}</dd>
            </div>
          ))}
        </dl>
      )}
      {copy.source === 'app' && (
        <div className="flex flex-col items-start gap-2 text-xs">
          <p className="text-muted">
            Installed {formatDateTime(copy.installedAt)} from{' '}
            <span className="font-mono break-all text-text">{copy.archive}</span>
          </p>
          <div className="flex flex-wrap items-start gap-2">
            {reinstall !== null && (
              <button type="button" disabled={busy} onClick={reinstall} className={buttonClass}>
                Reinstall
              </button>
            )}
            <TrashCopy toolId={tool.id} copy={copy} label="Move to the Trash" disabled={busy} trash={trash} />
          </div>
        </div>
      )}
      {tool.others.length > 0 && (
        <div className="flex flex-col gap-1">
          <h4 id={`${id}-others`} className="text-xs font-medium">
            Other copies
          </h4>
          <ul aria-labelledby={`${id}-others`} className="flex flex-col gap-1 text-xs">
            {tool.others.map((other) =>
              other.shadowing ? (
                <li key={other.path} className="flex flex-col gap-1">
                  <p className="flex items-start gap-1.5 text-warning">
                    <AlertIcon className="mt-px size-3.5 shrink-0" />
                    <span>
                      Your terminal runs{' '}
                      <span className="font-mono">{`${other.displayPath} ${other.version.raw}`.trimEnd()}</span>, not
                      this one.
                    </span>
                  </p>
                  <div className="pl-5">
                    <TrashCopy
                      toolId={tool.id}
                      copy={other}
                      label="Move it to the Trash"
                      disabled={busy}
                      trash={trash}
                    />
                  </div>
                </li>
              ) : (
                <li key={other.path} className="font-mono break-all text-muted">
                  {otherText(other)}
                </li>
              ),
            )}
          </ul>
        </div>
      )}
      <form onSubmit={submit} className="flex flex-col gap-1.5">
        <label htmlFor={pathId} className="text-xs font-medium">
          Use a different copy
        </label>
        <div className="flex items-center gap-2">
          <input
            id={pathId}
            type="text"
            value={draft}
            placeholder={`~/bin/${tool.binary}`}
            spellCheck={false}
            aria-invalid={error !== ''}
            aria-describedby={joinIds(error !== '' && errorId)}
            onChange={(e) => {
              setDraft(e.target.value)
              setError('')
            }}
            className={`${fieldClass} min-w-0 flex-1 font-mono text-[0.8125rem]`}
          />
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              void choose()
            }}
            className={buttonClass}
          >
            Choose…
          </button>
          <button type="submit" disabled={busy || draft.trim() === ''} className={buttonClass}>
            Use
          </button>
        </div>
        <ErrorText id={errorId} error={error} />
      </form>
      {tool.chosen !== '' && (
        <div className="flex items-center justify-between gap-3">
          <p className="min-w-0 text-xs text-muted">
            You chose <span className="font-mono break-all text-text">{tool.chosen}</span>.
          </p>
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              void storePath('')
            }}
            className={`${quietButtonClass} shrink-0`}
          >
            Use the one your shell finds
          </button>
        </div>
      )}
      <p className="text-xs">
        <LinkButton url={tool.releasesUrl}>Releases on GitHub</LinkButton>
      </p>
    </div>
  )
}

export interface IntegrationCardProps {
  tool: IntegrationTool
  // checking reports a look for the tool under way, besides the tool's own checking.
  checking: boolean
  // onSaved receives every setting as stored after a change, as the setter returned it.
  onSaved: (settings: Settings) => void
  // reload reads the tools again after a change; it resolves once they are in.
  reload: () => Promise<void>
  // onTools receives the tools as an install or a move to the Trash left them, to show in
  // place of the list.
  onTools: (tools: IntegrationTools) => void
  // terminal is the terminal chosen for launches, which the run of a tool's own updater
  // names, or null until the settings have loaded.
  terminal: TerminalID | null
}

// Asking is the install the dialog asks about: its kind, and whether confirming it turns
// the tool on first.
interface Asking {
  kind: InstallKind
  turnOn: boolean
}

// IntegrationCard is one tool the app runs: what it is for, its switch, what was found of
// it, a button that installs, updates, or reinstalls it from its latest release, and,
// behind Details, every copy found and a way to choose another or move one to the Trash.
// A change to the switch shows at once, and holds until the tools are read again. Every
// install asks first, then shows its progress here as the tools are read again, and what
// it did, or why it failed, until the next change. A copy that updates itself runs its own
// updater: its run shows here, live, and once it ended and the tool was looked for again,
// what it did.
export function IntegrationCard({ tool, checking, onSaved, reload, onTools, terminal }: IntegrationCardProps) {
  const id = useId()
  const [marks, match] = useSetting(tool.name)
  const [shownSwitch, showSwitch] = useOptimistic(tool.switch)
  const [busy, startSaving] = useTransition()
  const [switchError, setSwitchError] = useState('')
  const [open, setOpen] = useState(false)
  const [asking, setAsking] = useState<Asking | null>(null)
  // outcome is what the last install did, or why it failed, until the next change.
  const [outcome, setOutcome] = useState<Result<string> | null>(null)
  // followed is the run of the tool's own updater the card shows, as an update started it
  // or the user opened it, until it is closed or the next change.
  const [followed, setFollowed] = useState<number | null>(null)
  const { copy, install: plan, selfUpdate } = tool
  // The switch shows the tool as listed, or, while a flip is stored, as flipped.
  const on = shownSwitch === tool.switch ? tool.on : shownSwitch === 'on'
  const installing = tool.progress.step !== ''
  // kind is what the card's button does, and blocked why there is none, or ''.
  const kind = mainKinds[tool.state]
  const blocked = kind === null ? '' : plan.blocked
  const folder = folderOf(copy)
  // ended is what the followed run did, once it ended and the tool was looked for again.
  const ended =
    followed !== null && selfUpdate.run === followed && !selfUpdate.running && selfUpdate.message !== ''
      ? selfUpdate
      : null
  // showable is the run of the tool's own updater going now that the card can show, once
  // its ID is known, while it doesn't already.
  const showable = selfUpdate.running && selfUpdate.run !== 0 && followed !== selfUpdate.run ? selfUpdate.run : null

  // act runs a change with call while the card is busy, showing it at once with show,
  // and resolves with how it went. What the last install did is cleared first: it shows
  // only until the next change.
  const act = <T,>(call: () => PromiseLike<T>, show?: () => void) =>
    new Promise<Result<T>>((resolve) => {
      setOutcome(null)
      setFollowed(null)
      startSaving(async () => {
        show?.()
        resolve(await attempt(start(call)))
      })
    })

  // store stores a setting with call, then hands the stored settings on and reads the
  // tools again.
  const store = async (call: () => PromiseLike<Settings>) => {
    const stored = await call()

    startTransition(() => {
      onSaved(stored)
    })
    await reload()

    return stored
  }

  // save stores a change with call, showing it at once with show, and resolves with how
  // it went.
  const save = (call: () => PromiseLike<Settings>, show?: () => void) => act(() => store(call), show)

  // setSwitch stores the switch for good, showing it at once, and reports whether it was
  // stored.
  const setSwitch = async (next: IntegrationSwitch) => {
    setSwitchError('')

    const result = await save(
      () => setIntegrationEnabled(tool.id, next),
      () => {
        showSwitch(next)
      },
    )

    if (!result.ok) {
      setSwitchError(result.error)
    }

    return result.ok
  }

  // flip stores the opposite of what the switch shows, for good: on or off, never
  // automatic again. Turning on a tool that is missing asks to install it, when it can
  // be, and stores nothing until the user says yes.
  const flip = () => {
    if (!on && tool.state === 'missing' && plan.blocked === '') {
      setAsking({ kind: 'install', turnOn: true })

      return
    }

    void setSwitch(on ? 'off' : 'on')
  }

  // runInstall runs the install the user confirmed, turning the tool on first when that
  // is how they asked, and shows the tools as it left them and what it did. An update the
  // tool runs itself says nothing yet: its run shows instead, and says what it did as it
  // ends.
  const runInstall = async (confirmed: Asking) => {
    if (confirmed.turnOn && !(await setSwitch('on'))) {
      return
    }

    const result = await act(async () => {
      const done = await installers[confirmed.kind](tool.id)

      startTransition(() => {
        onTools(done.tools)

        if (done.run !== 0) {
          setFollowed(done.run)
        }
      })

      return done.message
    })

    setOutcome(result.ok && result.data === '' ? null : result)
  }

  // trash moves a copy to the Trash, then shows the tools as it left them.
  const trash = (path: string) =>
    act(async () => {
      const tools = await trashToolCopy(tool.id, path)

      startTransition(() => {
        onTools(tools)
      })
    })

  const switchErrorId = `${id}-switch-error`

  return (
    <section
      {...marks}
      aria-labelledby={`${id}-name`}
      className={`rounded-lg border border-border bg-surface ${cardMatchClass}`}
    >
      <div className="flex items-start justify-between gap-6 px-4 pt-3.5">
        <div className="min-w-0">
          <h3 id={`${id}-name`} className={`text-sm font-semibold ${headingMatchClass(match)}`}>
            <label htmlFor={`${id}-switch`}>{tool.name}</label>
          </h3>
          <p id={`${id}-purpose`} className="mt-0.5 text-xs text-muted">
            {tool.purpose}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <span id={`${id}-caption`} className="text-xs text-muted">
            {switchCaption(shownSwitch, on)}
          </span>
          <Switch
            id={`${id}-switch`}
            checked={on}
            disabled={busy}
            describedBy={joinIds(`${id}-caption`, `${id}-purpose`, switchError !== '' && switchErrorId)}
            onChange={flip}
          />
        </div>
      </div>
      <div className="flex flex-col gap-1 px-4 pt-2.5 pb-3 text-xs">
        <ErrorText id={switchErrorId} error={switchError} />
        <div className="flex items-start justify-between gap-4">
          <div className="flex min-w-0 flex-col gap-1">
            {installing ? (
              <p className="flex items-start gap-1.5 text-text">
                <Spinner className="mt-px size-3.5" />
                <span className="min-w-0 break-words">{progressText(tool.progress, tool.binary)}</span>
                {showable !== null && (
                  <button
                    type="button"
                    onClick={() => {
                      setFollowed(showable)
                    }}
                    className="shrink-0 font-medium text-accent hover:underline"
                  >
                    Show the run
                  </button>
                )}
              </p>
            ) : (
              <p className={`flex items-start gap-1.5 ${tool.state === 'tooOld' ? 'text-warning' : 'text-text'}`}>
                {stateIcons[tool.state]}
                <span className="min-w-0 break-words">{statusText(tool)}</span>
                {(checking || tool.checking) && <span className="shrink-0 text-muted">Checking…</span>}
              </p>
            )}
            {tool.state === 'update' && (
              <p className="pl-5 text-text">
                {tool.latest.version} is available · <LinkButton url={tool.latest.url}>What’s new</LinkButton>
              </p>
            )}
          </div>
          {kind !== null && blocked === '' && (
            <button
              type="button"
              disabled={busy || installing}
              onClick={() => {
                setAsking({ kind, turnOn: false })
              }}
              className={`${primaryButtonClass} shrink-0`}
            >
              {buttonLabel(kind, tool)}
            </button>
          )}
        </div>
        {blocked !== '' && (
          <div className="flex flex-col items-start gap-1.5 pl-5">
            <p className="text-muted">{blocked}</p>
            {plan.brewCommand !== '' && <CommandLine command={plan.brewCommand} className="w-full max-w-sm" />}
          </div>
        )}
        {outcome !== null &&
          (outcome.ok ? (
            <p role="status" className="flex items-start gap-1.5 text-text">
              <CheckIcon className="mt-px size-3.5 shrink-0 text-success" />
              <span className="min-w-0 break-words">{outcome.data}</span>
            </p>
          ) : (
            <ErrorText error={outcome.error} />
          ))}
        {ended !== null &&
          (ended.failed ? (
            <ErrorText error={ended.message} />
          ) : (
            <p role="status" className="flex items-start gap-1.5 text-text">
              <CheckIcon className="mt-px size-3.5 shrink-0 text-success" />
              <span className="min-w-0 break-words">{ended.message}</span>
            </p>
          ))}
        {followed !== null && (
          <div className="mt-1.5 flex h-80 flex-col rounded-lg border border-border">
            <RunPanel
              id={followed}
              terminal={terminal}
              onSelect={setFollowed}
              onClose={() => {
                setFollowed(null)
              }}
            />
          </div>
        )}
        {copy.source === 'app' && !copy.onPath && (
          <div className="flex flex-col items-start gap-1.5">
            <p className="flex items-start gap-1.5 text-warning">
              <AlertIcon className="mt-px size-3.5 shrink-0" />
              <span>
                Your terminal won’t find <span className="font-mono">{tool.binary}</span> yet. Add{' '}
                <span className="font-mono break-all">{folder}</span> to your PATH: put this line in ~/.zprofile.
              </span>
            </p>
            <div className="w-full max-w-md pl-5">
              <CommandLine command={exportLine(folder)} prompt={false} />
            </div>
          </div>
        )}
        {tool.chosenMissing && (
          <p className="text-warning">
            The copy you chose, <span className="font-mono">{tool.chosen}</span>, isn’t there any more, so {brand.name}{' '}
            uses the one your shell finds.
          </p>
        )}
      </div>
      <div className="border-t border-border">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={`${id}-details`}
          onClick={() => {
            setOpen((was) => !was)
          }}
          className="flex w-full items-center gap-1 px-4 py-2 text-left text-xs font-medium text-muted transition-colors hover:text-text"
        >
          Details
          <ChevronRightIcon
            className={`size-3.5 transition-transform motion-reduce:transition-none ${open ? 'rotate-90' : ''}`}
          />
        </button>
        {open && (
          <Details
            tool={tool}
            id={id}
            busy={busy || installing}
            save={save}
            reinstall={
              plan.action === 'reinstall' && plan.blocked === ''
                ? () => {
                    setAsking({ kind: 'reinstall', turnOn: false })
                  }
                : null
            }
            trash={trash}
          />
        )}
      </div>
      {asking !== null && (
        <InstallDialog
          tool={tool}
          kind={asking.kind}
          onCancel={() => {
            setAsking(null)
          }}
          onConfirm={() => {
            setAsking(null)
            void runInstall(asking)
          }}
        />
      )}
    </section>
  )
}
