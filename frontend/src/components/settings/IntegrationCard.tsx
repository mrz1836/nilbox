import { startTransition, useId, useOptimistic, useState, useTransition, type ReactNode, type SubmitEvent } from 'react'

import { brand } from '../../generated/brand'
import { attempt, start, type Result } from '../../lib/call'
import {
  chooseFileToOpen,
  openExternal,
  setIntegrationEnabled,
  setIntegrationPath,
  type IntegrationSwitch,
  type IntegrationTool,
  type Settings,
  type ToolCopy,
  type ToolSource,
  type ToolState,
} from '../../platform'
import { buttonClass, fieldClass, quietButtonClass, Switch } from '../controls'
import { AlertIcon, CheckIcon, ChevronRightIcon, ExternalIcon, XIcon } from '../icons'
import { cardMatchClass, headingMatchClass, useSetting } from './match'
import { ErrorText, joinIds } from './rows'

// sourceLabels names where a copy of a tool came from.
const sourceLabels: Readonly<Record<ToolSource, string>> = {
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
  // busy holds the controls while a change is stored.
  busy: boolean
  // save stores a change and resolves with how it went.
  save: (call: () => PromiseLike<Settings>) => Promise<Result<Settings>>
}

// Details is what a card shows once opened: the copy in use, the other copies found, a way
// to use a different one, and the tool's releases.
function Details({ tool, id, busy, save }: DetailsProps) {
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
      {tool.others.length > 0 && (
        <div className="flex flex-col gap-1">
          <h4 id={`${id}-others`} className="text-xs font-medium">
            Other copies
          </h4>
          <ul aria-labelledby={`${id}-others`} className="flex flex-col gap-1 text-xs">
            {tool.others.map((other) =>
              other.shadowing ? (
                <li key={other.path} className="flex items-start gap-1.5 text-warning">
                  <AlertIcon className="mt-px size-3.5 shrink-0" />
                  <span>
                    Your terminal runs{' '}
                    <span className="font-mono">{`${other.displayPath} ${other.version.raw}`.trimEnd()}</span>, not this
                    one.
                  </span>
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
}

// IntegrationCard is one tool the app runs: what it is for, its switch, what was found of
// it, and, behind Details, every copy found and a way to choose another. A change to the
// switch shows at once, and holds until the tools are read again.
export function IntegrationCard({ tool, checking, onSaved, reload }: IntegrationCardProps) {
  const id = useId()
  const [marks, match] = useSetting(tool.name)
  const [shownSwitch, showSwitch] = useOptimistic(tool.switch)
  const [busy, startSaving] = useTransition()
  const [switchError, setSwitchError] = useState('')
  const [open, setOpen] = useState(false)
  // The switch shows the tool as listed, or, while a flip is stored, as flipped.
  const on = shownSwitch === tool.switch ? tool.on : shownSwitch === 'on'

  // save stores a change with call, showing it at once with show, then hands the stored
  // settings on and reads the tools again. It resolves with how the change went.
  const save = (call: () => PromiseLike<Settings>, show?: () => void) =>
    new Promise<Result<Settings>>((resolve) => {
      startSaving(async () => {
        show?.()

        const result = await attempt(start(call))

        if (result.ok) {
          startTransition(() => {
            onSaved(result.data)
          })
          await reload()
        }

        resolve(result)
      })
    })

  // flip stores the opposite of what the switch shows, for good: on or off, never
  // automatic again.
  const flip = async () => {
    const next: IntegrationSwitch = on ? 'off' : 'on'

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
  }

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
            onChange={() => {
              void flip()
            }}
          />
        </div>
      </div>
      <div className="flex flex-col gap-1 px-4 pt-2.5 pb-3 text-xs">
        <ErrorText id={switchErrorId} error={switchError} />
        <p className={`flex items-start gap-1.5 ${tool.state === 'tooOld' ? 'text-warning' : 'text-text'}`}>
          {stateIcons[tool.state]}
          <span className="min-w-0 break-words">{statusText(tool)}</span>
          {(checking || tool.checking) && <span className="shrink-0 text-muted">Checking…</span>}
        </p>
        {tool.state === 'update' && (
          <p className="pl-5 text-text">
            {tool.latest.version} is available · <LinkButton url={tool.latest.url}>What’s new</LinkButton>
          </p>
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
        {open && <Details tool={tool} id={id} busy={busy} save={save} />}
      </div>
    </section>
  )
}
