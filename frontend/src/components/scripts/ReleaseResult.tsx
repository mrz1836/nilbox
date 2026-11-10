import { useEffect, useEffectEvent, useId, useRef, useState, type ReactNode } from 'react'

import { brand } from '../../generated/brand'
import { prefersReducedMotion } from '../../lib/motion'
import { shortCommit } from '../../lib/git'
import { intentOf, resultHeading } from '../../lib/release'
import { useMutation } from '../../lib/useMutation'
import { useQuery } from '../../lib/useQuery'
import {
  copyText,
  getReleaseResult,
  openExternal,
  openScriptRunInTerminal,
  type GitOperation,
  type ReleaseResult as Result,
  type ScriptRun,
} from '../../platform'
import { Confetti, useConfetti } from '../Confetti'
import { buttonClass, primaryButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { AlertIcon, CheckIcon, CircleCheckIcon, CopyIcon, ExternalIcon, InfoIcon, XIcon } from '../icons'
import { ErrorText } from '../settings/rows'
import { CopyCommand } from './CopyCommand'

// burstSize is how much confetti a release on GitHub throws.
const burstSize = 90

// Tone is how a line of the result reads: done, a note, a warning, or what went wrong.
type Tone = 'ok' | 'info' | 'warn' | 'bad'

// toneIcons mark each line's tone beside its words.
const toneIcons: Readonly<Record<Tone, ReactNode>> = {
  ok: <CheckIcon className="mt-px size-4 shrink-0 text-success" />,
  info: <InfoIcon className="mt-px size-4 shrink-0 text-info" />,
  warn: <AlertIcon className="mt-px size-4 shrink-0 text-warning" />,
  bad: <XIcon className="mt-px size-4 shrink-0 text-danger" />,
}

// toneWords say each tone in words, for assistive technology.
const toneWords: Readonly<Record<Tone, string>> = { ok: 'Done:', info: 'Note:', warn: 'Warning:', bad: 'Problem:' }

// operations name what git is in the middle of in a clone.
const operations: Readonly<Record<Exclude<GitOperation, ''>, string>> = {
  rebasing: 'A rebase',
  merging: 'A merge',
  'cherry-picking': 'A cherry-pick',
  reverting: 'A revert',
}

// Line is one line of the result.
interface Line {
  tone: Tone
  text: string
}

// cloneLines say where the clone is now: back on the branch it was on, on another, on a
// detached commit, or in the middle of something git started.
function cloneLines(result: Result): Line[] {
  const lines: Line[] = []
  const was = result.original === '' ? '' : ` It was on ${result.original}.`

  if (result.back) {
    lines.push({ tone: 'ok', text: `Your clone is back on ${result.original}` })
  } else if (result.detached) {
    lines.push({ tone: 'warn', text: `Your clone is on a detached commit.${was}` })
  } else if (result.branch !== '') {
    lines.push({ tone: 'warn', text: `Your clone is on ${result.branch}.${was}` })
  }

  if (result.inProgress !== '') {
    lines.push({
      tone: 'bad',
      text: `${operations[result.inProgress]} is in progress in your clone. Finish or abort it in your terminal.`,
    })
  }

  return lines
}

// resultLines say what a release did, then where the clone is.
function resultLines(result: Result, branch: string): Line[] {
  const lines: Line[] = []
  const on = branch === '' ? '' : ` on ${branch}`

  switch (result.kind) {
    case 'released':
      lines.push({ tone: 'ok', text: `Tagged ${shortCommit(result.commit)}${on} and pushed the tag to origin` })

      if (result.moved) {
        lines.push({ tone: 'info', text: `Released ${result.tag}. ${result.version} appeared on GitHub meanwhile.` })
      }

      if (result.newer) {
        lines.push({
          tone: 'info',
          text: `${result.tag} tags ${shortCommit(result.commit)}, which is newer than the ${shortCommit(result.head)} you reviewed.`,
        })
      }

      break
    case 'notPushed':
      if (result.pushWarning !== '') {
        lines.push({ tone: 'warn', text: `magex said: ${result.pushWarning}` })
      }

      break
    case 'failed':
      if (result.tagged && result.taken) {
        lines.push({ tone: 'warn', text: `${result.tag} is tagged on this Mac at a commit GitHub doesn’t have.` })
      }

      break
    case 'unverified':
      lines.push({
        tone: 'warn',
        text: `It may be on GitHub: ${brand.name} couldn’t ask origin. ${result.error}`.trim(),
      })

      break
  }

  return [...lines, ...cloneLines(result)]
}

// commandLabel says what a command the result offers does.
function commandLabel(command: string): string {
  return command.startsWith('git push') ? 'To push it:' : 'To undo it:'
}

export interface ReleaseResultProps {
  // run is the release's run, ended.
  run: ScriptRun
  // repo is the repository released, "owner/name".
  repo: string
  // onTryAgain reopens the sheet with fresh checks; a release is never run again from its
  // stored command.
  onTryAgain: () => void
  onDone: () => void
}

// ReleaseResult is how a release came out, as the app verified it once magex ended: on
// GitHub, with links to the tag and the release workflow and a burst of confetti (none
// with reduced motion); tagged on this Mac only, with the commands to push or undo it,
// which the app never runs; or failed, with what happened in words and the line of
// magex's that says so. Each says where the clone is now. It is announced once as it
// shows.
export function ReleaseResult({ run, repo, onTryAgain, onDone }: ReleaseResultProps) {
  const id = useId()
  const query = useQuery(() => getReleaseResult(run.id), [run.id])
  const result = query.data
  const intent = intentOf(run)
  const copy = useMutation((tag: string) => copyText(tag))
  const terminal = useMutation(() => openScriptRunInTerminal(run.id))
  const [copied, setCopied] = useState(false)
  const heading = useRef<HTMLHeadingElement>(null)
  const { bursts, fire, land } = useConfetti()
  const [layer, setLayer] = useState<Element | null>(null)

  // A release on GitHub throws confetti once, from its heading, over the sheet.
  const celebrate = useEffectEvent(() => {
    const el = heading.current

    if (el === null || prefersReducedMotion()) {
      return
    }

    const r = el.getBoundingClientRect()

    setLayer(el.closest('dialog'))
    fire({ x: r.left + r.width / 2, y: r.top + r.height / 2 }, burstSize)
  })
  const released = result?.kind === 'released'

  useEffect(() => {
    if (released) {
      celebrate()
    }
  }, [released])

  if (result === undefined) {
    return query.error === '' ? (
      <p role="status" className="text-sm text-muted">
        Reading how the release came out…
      </p>
    ) : (
      <section aria-labelledby={`${id}-heading`} className="flex flex-col gap-3">
        <h3 id={`${id}-heading`} className="text-base font-semibold">
          {run.summary === '' ? 'The release ended without a result' : run.summary}
        </h3>
        <ErrorBanner>{query.error}</ErrorBanner>
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" onClick={onTryAgain} className={buttonClass}>
            Try again
          </button>
          <button type="button" onClick={onDone} className={primaryButtonClass}>
            Done
          </button>
        </div>
      </section>
    )
  }

  const tag = result.tag === '' ? result.version : result.tag
  const lines = resultLines(result, intent.branch)
  const { failure } = result
  const Icon = released ? CircleCheckIcon : result.kind === 'failed' ? XIcon : AlertIcon
  const iconTone = released ? 'text-success' : result.kind === 'failed' ? 'text-danger' : 'text-warning'

  const copyTag = async () => {
    setCopied(false)
    setCopied((await copy.run(tag)).ok)
  }

  return (
    <section
      role="status"
      aria-labelledby={`${id}-heading`}
      className="flex flex-col gap-3 rounded-lg border border-border bg-background/40 px-4 py-3"
    >
      <h3 ref={heading} id={`${id}-heading`} className="flex items-start gap-2 text-base font-semibold">
        <Icon className={`mt-0.5 size-5 shrink-0 ${iconTone}`} />
        {resultHeading(result, repo)}
      </h3>
      {failure !== null && result.kind !== 'failed' && (
        <p className="flex items-start gap-2 text-sm text-text">
          {toneIcons.bad}
          <span>
            <span className="sr-only">{toneWords.bad} </span>
            {failure.heading}
          </span>
        </p>
      )}
      {failure !== null && failure.quote !== '' && (
        <blockquote className="rounded-md bg-background px-3 py-2 font-mono text-xs break-all whitespace-pre-wrap text-text">
          {failure.quote}
        </blockquote>
      )}
      {failure?.bug === true && (
        <p className="text-sm text-muted">This is a bug in {brand.name}: please report it from Settings → About.</p>
      )}
      {lines.length > 0 && (
        <ul className="flex flex-col gap-1.5">
          {lines.map((line) => (
            <li key={line.text} className="flex items-start gap-2 text-sm text-text">
              {toneIcons[line.tone]}
              <span>
                <span className="sr-only">{toneWords[line.tone]} </span>
                {line.text}
              </span>
            </li>
          ))}
        </ul>
      )}
      {result.commands.length > 0 && (
        <div className="flex flex-col gap-1.5">
          {result.commands.map((command) => (
            <CopyCommand key={command} command={command} label={commandLabel(command)} />
          ))}
          <p className="text-xs text-muted">{brand.name} runs neither. Open your terminal in the clone to run one.</p>
        </div>
      )}
      <ErrorText error={copy.error || terminal.error} />
      <div className="flex flex-wrap items-center gap-2">
        {result.tagUrl !== '' && (
          <button
            type="button"
            onClick={() => {
              openExternal(result.tagUrl)
            }}
            className={buttonClass}
          >
            View {tag} on GitHub
            <ExternalIcon className="size-3.5 text-muted" />
          </button>
        )}
        {result.workflowUrl !== '' && (
          <button
            type="button"
            onClick={() => {
              openExternal(result.workflowUrl)
            }}
            className={buttonClass}
          >
            Watch the release workflow
            <ExternalIcon className="size-3.5 text-muted" />
          </button>
        )}
        {released && (
          <button
            type="button"
            disabled={copy.pending}
            onClick={() => {
              void copyTag()
            }}
            className={buttonClass}
          >
            {copied ? <CheckIcon className="size-3.5" /> : <CopyIcon className="size-3.5" />}
            {copied ? 'Copied' : `Copy ${tag}`}
          </button>
        )}
        {result.commands.length > 0 && (
          <button
            type="button"
            disabled={terminal.pending}
            onClick={() => {
              void terminal.run()
            }}
            className={buttonClass}
          >
            Open in terminal
          </button>
        )}
        <span className="ml-auto flex gap-2">
          {(result.kind === 'failed' || result.kind === 'unverified') && (
            <button type="button" onClick={onTryAgain} className={buttonClass}>
              Try again
            </button>
          )}
          <button type="button" onClick={onDone} className={primaryButtonClass}>
            Done
          </button>
        </span>
      </div>
      <Confetti bursts={bursts} onLand={land} container={layer} />
    </section>
  )
}
