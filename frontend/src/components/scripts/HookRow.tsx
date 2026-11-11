import { useState } from 'react'

import {
  actionButton,
  actionWords,
  ciNote,
  hookState,
  lastChecksWords,
  ours,
  question,
  uninstallable,
} from '../../lib/hooks'
import { formatAgo } from '../../lib/time'
import { useMutation } from '../../lib/useMutation'
import {
  installHook,
  openHooksTerminal,
  reinstallHook,
  replaceHook,
  runChecks,
  showHooksConfig,
  uninstallHook,
  updateHook,
  type HookAction,
  type HookActionRequest,
  type HookCheck,
  type HookChecksScope,
  type HookRepo,
  type RunsIn,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { buttonClass } from '../controls'
import { ChevronDownIcon, DotIcon, InfoIcon, MoreIcon } from '../icons'
import { MenuButton, type MenuItem } from '../Menu'
import { RelativeTime } from '../RelativeTime'
import { ErrorText } from '../settings/rows'
import { ToneIcon, toneWords } from './ToneIcon'

// actionCalls start each action in a clone, and return its run's ID.
const actionCalls: Readonly<Record<HookAction, (req: HookActionRequest) => PromiseLike<number>>> = {
  install: installHook,
  update: updateHook,
  reinstall: reinstallHook,
  replace: replaceHook,
  uninstall: uninstallHook,
}

// checksNote says once, under the run items, which checks change files.
const checksNote = 'eof, fumpt, and whitespace fix files. whitespace also stages its fixes.'

// Start is a run a row starts: an action, with the shared hooks folder it confirms, or
// the checks, on every file, the staged ones, or one check on every file.
type Start =
  | { kind: 'action'; action: HookAction; confirm: string }
  | { kind: 'checks'; scope: HookChecksScope; check: HookCheck | '' }

// Pressed is what the row did last, whose failure shows: start a run, or open the clone.
type Pressed = 'start' | 'open'

export interface HookRowProps {
  repo: HookRepo
  // version is the go-pre-commit in use, for the note on the one the repository's CI pins.
  version: string
  // watch is the run going in the clone, which a busy row's Watch opens, or null.
  watch: number | null
  // runsIn is where runs go, as set.
  runsIn: RunsIn
  // onOpenRun shows a run, as each run the row starts is shown.
  onOpenRun: (id: number) => void
}

// HookRow is one clone with a go-pre-commit config: its name and what its pre-commit hook
// is, with how its newest checks went; what else is true of it (other hook types
// go-pre-commit installed, the go-pre-commit its CI pins, the run that holds it, why its
// hooks couldn't be read); its button (Install, Update, Reinstall, Replace…, Run checks,
// or none while turned off); and a menu of everything else: the checks, Reinstall,
// Uninstall…, the config, and the terminal. Replace…, Uninstall…, and any action in a
// shared hooks folder ask first. Each run it starts opens, so its log and result show.
export function HookRow({ repo, version, watch, runsIn, onOpenRun }: HookRowProps) {
  const [asking, setAsking] = useState<HookAction | null>(null)
  const [pressed, setPressed] = useState<Pressed>('start')
  const start = useMutation((how: Start) =>
    how.kind === 'checks'
      ? runChecks({ dir: repo.dir, scope: how.scope, check: how.check, runsIn })
      : actionCalls[how.action]({ dir: repo.dir, confirm: how.confirm, runsIn }),
  )
  const open = useMutation((what: 'terminal' | 'config') =>
    what === 'terminal' ? openHooksTerminal(repo.dir) : showHooksConfig(repo.dir),
  )
  const state = hookState(repo)
  const button = actionButton(repo)
  const checks = lastChecksWords(repo.lastChecks)
  const ci = ciNote(repo.ciVersion, version)
  const busy = repo.busy !== ''
  const held = busy || start.pending
  const error = pressed === 'start' ? start.error : open.error

  // begin starts a run, and shows it once started.
  const begin = async (how: Start) => {
    setPressed('start')

    const started = await start.run(how)

    if (started.ok) {
      onOpenRun(started.data)
    }
  }

  // act runs an action, asking first when it sets another tool's hook aside, uninstalls,
  // or changes a hooks folder other repositories share.
  const act = (action: HookAction) => {
    if (action === 'replace' || action === 'uninstall' || repo.shared) {
      setAsking(action)
    } else {
      void begin({ kind: 'action', action, confirm: '' })
    }
  }

  const openIt = (what: 'terminal' | 'config') => {
    setPressed('open')
    void open.run(what)
  }

  const checkItems: MenuItem[] = [
    {
      label: 'Run checks on all files',
      disabled: held,
      onSelect: () => {
        void begin({ kind: 'checks', scope: 'all', check: '' })
      },
    },
    {
      label: 'Run checks on staged files',
      disabled: held,
      onSelect: () => {
        void begin({ kind: 'checks', scope: 'staged', check: '' })
      },
    },
    {
      label: 'Run one check',
      disabled: held,
      note: checksNote,
      items: () =>
        repo.checks.map((c) => ({
          label: c.on ? c.name : `${c.name} (off)`,
          onSelect: () => {
            void begin({ kind: 'checks', scope: 'one', check: c.name })
          },
        })),
    },
  ]

  const moreItems = (): MenuItem[] => [
    ...checkItems,
    {
      label: 'Reinstall the hook',
      divided: true,
      disabled: held || !ours(repo),
      onSelect: () => {
        act('reinstall')
      },
    },
    {
      label: 'Uninstall the hook…',
      danger: true,
      disabled: held || !uninstallable(repo),
      onSelect: () => {
        act('uninstall')
      },
    },
    {
      label: 'Open the config',
      divided: true,
      onSelect: () => {
        openIt('config')
      },
    },
    {
      label: 'Open in terminal',
      onSelect: () => {
        openIt('terminal')
      },
    },
  ]

  return (
    <li className="flex items-start gap-3 rounded-md px-3 py-2 hover:bg-raised/60">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <p className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm">
          <span className="font-medium">{repo.name}</span>
          <span className="flex items-center gap-1 text-xs text-text">
            <ToneIcon tone={state.tone} />
            <span className="sr-only">{toneWords[state.tone]} </span>
            {state.text}
          </span>
          {checks !== '' && (
            <button
              type="button"
              title={repo.lastChecks.summary}
              onClick={() => {
                onOpenRun(repo.lastChecks.id)
              }}
              className="text-xs text-muted hover:text-text hover:underline"
            >
              · {checks}
              {repo.lastChecks.finishedAt !== '' && (
                <>
                  {' '}
                  <RelativeTime iso={repo.lastChecks.finishedAt} show={formatAgo} />
                </>
              )}
            </button>
          )}
        </p>
        {repo.error !== '' && <p className="text-xs text-danger">{repo.error}</p>}
        {repo.alsoInstalled.length > 0 && (
          <p className="text-xs text-muted">Also installed: {repo.alsoInstalled.join(', ')}</p>
        )}
        {ci !== '' && (
          <p className="flex items-center gap-1 text-xs text-muted">
            <InfoIcon className="size-3.5 shrink-0 text-info" />
            {ci}
          </p>
        )}
        {busy && (
          <p className="flex items-center gap-1 text-xs text-text">
            <DotIcon className="size-3.5 shrink-0 text-warning" />
            {repo.busy}
            {watch !== null && (
              <>
                {' · '}
                <button
                  type="button"
                  aria-label={`Watch the run in ${repo.name}`}
                  onClick={() => {
                    onOpenRun(watch)
                  }}
                  className="font-medium text-accent hover:underline"
                >
                  Watch
                </button>
              </>
            )}
          </p>
        )}
        <ErrorText error={error} />
      </div>
      <div className="flex shrink-0 items-center gap-1">
        {button !== null && (
          <button
            type="button"
            aria-label={`${actionWords[button.action]} the hook in ${repo.name}${button.asks ? '…' : ''}`}
            disabled={held}
            onClick={() => {
              act(button.action)
            }}
            className={buttonClass}
          >
            {button.label}
          </button>
        )}
        {repo.group === 'ready' && (
          <MenuButton label={`Run checks in ${repo.name}`} items={checkItems} className={buttonClass}>
            Run checks
            <ChevronDownIcon className="size-3.5 text-muted" />
          </MenuButton>
        )}
        {repo.group === 'off' && <span className="text-xs text-muted">Turned on and off in .github/env/</span>}
        <MenuButton
          label={`More for ${repo.name}`}
          items={moreItems}
          className="flex size-7 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text"
        >
          <MoreIcon className="size-4" />
        </MenuButton>
      </div>
      {asking !== null && (
        <AskFirst
          repo={repo}
          action={asking}
          onConfirm={() => {
            setAsking(null)
            void begin({ kind: 'action', action: asking, confirm: repo.shared ? repo.hooksDir : '' })
          }}
          onCancel={() => {
            setAsking(null)
          }}
        />
      )}
    </li>
  )
}

// AskFirst is the question an action asks before it runs.
function AskFirst({
  repo,
  action,
  onConfirm,
  onCancel,
}: {
  repo: HookRepo
  action: HookAction
  onConfirm: () => void
  onCancel: () => void
}) {
  const asked = question(repo, action)

  return (
    <ConfirmDialog
      title={asked.title}
      confirmLabel={asked.confirm}
      tone={asked.danger ? 'danger' : 'primary'}
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      {asked.text}
    </ConfirmDialog>
  )
}
