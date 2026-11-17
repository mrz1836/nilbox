import { useId, useState } from 'react'

import { canonical, testedLabel, testHeadline } from '../../../lib/routineBuilder'
import { useQuery } from '../../../lib/useQuery'
import {
  previewAutopilotActions,
  previewRoutineRun,
  type AutopilotDraft,
  type AutopilotItemRef,
  type AutopilotMode,
  type AutopilotResult,
  type AutopilotTestItem,
  type AutopilotTrace,
  type AutopilotTestResult,
  type ItemKind,
} from '../../../platform'
import { PlannedLaunch } from '../../LaunchPreviewSection'
import { buttonClass, captionClass, quietButtonClass } from '../../controls'
import { ChevronDownIcon, ChevronRightIcon, RefreshIcon } from '../../icons'
import { Spinner } from '../../Spinner'
import { ResultMark, TraceList } from '../Trace'
import { RunOnMatches } from './RunOnMatches'

export interface TestPanelProps {
  // result is the last test of the draft, undefined before one answered; stale reports one
  // of an earlier draft, whose watch, trigger, conditions, or Once per changed since.
  result: AutopilotTestResult | undefined
  stale: boolean
  // testing reports a test under way, and error why the last one failed.
  testing: boolean
  error: string
  // ready reports a draft that can be tried, with no problem; why says what to fix first.
  ready: boolean
  // kinds are the kinds of item the routine acts on, which its headline names.
  kinds: readonly ItemKind[]
  // mode is the routine's mode: in Dry run, a match is what it would do.
  mode: AutopilotMode
  // draft is the routine as edited, whose agent step Preview plans on an item it tried.
  draft: AutopilotDraft
  onTest: () => void
  // preview is the draft, when it sends GitHub anything: a matching item's Preview says what
  // it would send it, built and not sent.
  preview?: AutopilotDraft | undefined
  // pinned is the item a routine was made from, with what the draft decides about it from
  // what is stored: it shows at the top before a test, and first in one.
  pinned?: Pinned | undefined
  // saved reports the draft is the routine as saved, which Run on the matches now runs.
  saved?: boolean
}

// Pinned is an item Test shows at the top, with what the draft decides about it now.
export interface Pinned {
  item: AutopilotItemRef
  match: AutopilotResult
  trace: AutopilotTrace[]
}

// pinnedFirst puts the item pinned names first among items, else leaves them as they are.
export function pinnedFirst(items: readonly AutopilotTestItem[], pinned: Pinned | undefined): AutopilotTestItem[] {
  if (pinned === undefined) {
    return [...items]
  }

  return [...items.filter((it) => it.id === pinned.item.id), ...items.filter((it) => it.id !== pinned.item.id)]
}

// previewed is the item a test tried that Preview plans the agent step on: the first that
// matches, else the first.
function previewed(result: AutopilotTestResult | undefined): AutopilotTestItem | undefined {
  return result?.items.find((it) => it.match === 'yes') ?? result?.items[0]
}

interface RunPreviewProps {
  draft: AutopilotDraft
  item: AutopilotTestItem
}

// RunPreview is Preview <repo> #<n> ▸: on request, exactly the launch the draft's agent
// step would make on the item, built and not started (what it resolves to, the prompt, the
// worktree and folder, the command, its environment and permissions, and the terminal),
// read again as the draft changes while it is open.
function RunPreview({ draft, item }: RunPreviewProps) {
  const id = useId()
  const [open, setOpen] = useState(false)
  const key = canonical(draft)
  const plan = useQuery(() => previewRoutineRun(draft, item.id), [key, item.id], { enabled: open })
  const name = `${item.repo} #${String(item.number)}`
  const p = plan.data

  return (
    <div className="flex flex-col gap-2">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={`${id}-plan`}
        onClick={() => {
          setOpen(!open)
        }}
        className={`${quietButtonClass} self-start`}
      >
        {open ? <ChevronDownIcon className="size-3" /> : <ChevronRightIcon className="size-3" />}
        Preview {name}
      </button>
      {open && (
        <div id={`${id}-plan`} className="flex flex-col gap-2 rounded-md border border-border p-2.5">
          {plan.error !== '' ? (
            <p role="alert" className="text-xs text-danger">
              {plan.error}
            </p>
          ) : p === undefined || plan.pending ? (
            <p className="text-xs text-muted">Planning the run…</p>
          ) : (
            <>
              <p className="text-xs text-text">
                {[p.agent, p.profile, p.model === '' ? 'its default model' : p.model, p.effort, p.promptName]
                  .filter((part) => part !== '')
                  .join(' · ')}
              </p>
              <PlannedLaunch plan={p} />
              {p.warnings.length > 0 && (
                <ul aria-label="What it skipped" className="flex flex-col gap-0.5">
                  {p.warnings.map((w) => (
                    <li key={w} className="text-xs text-warning">
                      {w}
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}

// needWords say what a read would have to bring for an unknown to decide.
const needWords: Readonly<Record<string, string>> = {
  details: 'its details',
  checks: 'the check names',
  files: 'its changed files',
}

// rowState says where an item stands with the routine: what it would do, what its history
// holds of it, what it waits to know, or that it doesn't match.
function rowState(it: AutopilotTestItem, mode: AutopilotMode): string {
  if (it.leftAlone) {
    return 'left alone: you took back what it did'
  }

  if (it.match === 'yes') {
    return it.record !== '' ? it.record : mode === 'dryRun' ? 'would run, in Dry run' : 'would run'
  }

  if (it.match === 'unknown') {
    const needs = it.needs.map((n) => needWords[n] ?? n).filter((w) => w !== '')

    return needs.length > 0 ? `waiting for data: ${needs.join(', ')}` : 'waiting for data'
  }

  return 'doesn’t match'
}

// TestPanel is the draft tried on its items now, before it is saved: "3 of 7 open pull
// requests match now", each item with whether it matches, where it stands with the routine,
// and Why ▸, every step of its decision with the value it compared, and, on a match of a
// draft that sends GitHub anything, Preview ▸, what it would send; ↻ Test, which runs it
// only when asked, since its searches cost points, and says what the last test spent; and
// the agent step's Preview of the run it would start.
export function TestPanel({
  result,
  stale,
  testing,
  error,
  ready,
  kinds,
  mode,
  draft,
  onTest,
  preview,
  pinned,
  saved = false,
}: TestPanelProps) {
  const agent = draft.spec.steps.some((st) => st.agent != null)
  const sample = previewed(result)
  const headline = (() => {
    if (testing) {
      return 'Trying it on your items…'
    }

    if (result !== undefined) {
      return testHeadline(result, kinds)
    }

    return ready
      ? 'Not tried yet: Test reads its searches from GitHub, which costs a point or a few.'
      : 'Fix the problems above to try it on your items.'
  })()

  return (
    <section aria-label="Test" className="flex flex-col gap-2 border-t border-border py-3">
      <div className="flex flex-wrap items-center gap-3">
        <h3 className={captionClass}>Test</h3>
        <p role="status" className="min-w-0 flex-1 text-sm">
          {headline}
        </p>
        {testing && <Spinner />}
        <button
          type="button"
          disabled={!ready || testing}
          title={
            result?.cached === true
              ? 'Its searches were read within the minute, so they cost nothing again.'
              : undefined
          }
          onClick={onTest}
          className={buttonClass}
        >
          <RefreshIcon className="size-3.5 text-muted" />
          {testedLabel(result)}
        </button>
      </div>
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
      {stale && !testing && (
        <p className="text-xs text-muted">
          What it watches, fires on, checks, or fires once per changed since this test: Test again to see what it
          matches now.
        </p>
      )}
      {result?.lowBudget === true && (
        <p className="text-xs text-warning">
          Your GitHub API budget is low, so this test read nothing new; it tried what is stored.
        </p>
      )}
      {result === undefined && pinned !== undefined && (
        <ul aria-label="The item it was made from" className="flex flex-col">
          <PinnedRow pinned={pinned} />
        </ul>
      )}
      {result !== undefined && result.items.length > 0 && (
        <ul aria-label="Items it watches" className="flex flex-col">
          {pinnedFirst(result.items, pinned).map((it) => (
            <TestRow key={it.id} item={it} mode={mode} preview={it.match === 'yes' ? preview : undefined} />
          ))}
        </ul>
      )}
      {agent && ready && sample !== undefined && <RunPreview key={sample.id} draft={draft} item={sample} />}
      <RunOnMatches result={result} draft={draft} saved={saved} stale={stale || testing} mode={mode} />
    </section>
  )
}

interface TestRowProps {
  item: AutopilotTestItem
  mode: AutopilotMode
  // preview is the draft whose GitHub actions Preview ▸ shows for the item, when it matches.
  preview: AutopilotDraft | undefined
}

// TestRow is one item a test tried: ✓ or ✗, the item, where it stands, Why ▸, and, for a
// match of a draft that sends GitHub anything, Preview ▸.
function TestRow({ item: it, mode, preview }: TestRowProps) {
  const [open, setOpen] = useState(false)
  const [previewing, setPreviewing] = useState(false)
  const name = `${it.repo} #${String(it.number)}`

  return (
    <li className="flex flex-col border-b border-border/60 py-1 last:border-b-0">
      <div className="flex items-center gap-2 text-sm">
        <ResultMark result={it.match} />
        <span className="w-44 shrink-0 truncate font-mono text-[0.75rem] text-muted" title={name}>
          {name}
        </span>
        <span className="min-w-0 flex-1 truncate" title={it.title}>
          {it.title}
        </span>
        <span className={`shrink-0 text-xs ${it.match === 'yes' ? 'text-text' : 'text-muted'}`}>
          {rowState(it, mode)}
        </span>
        <button
          type="button"
          aria-expanded={open}
          aria-label={`Why, for ${name}`}
          onClick={() => {
            setOpen(!open)
          }}
          className={quietButtonClass}
        >
          {open ? <ChevronDownIcon className="size-3" /> : <ChevronRightIcon className="size-3" />}
          Why
        </button>
        {preview !== undefined && (
          <button
            type="button"
            aria-expanded={previewing}
            aria-label={`Preview, for ${name}`}
            onClick={() => {
              setPreviewing(!previewing)
            }}
            className={quietButtonClass}
          >
            {previewing ? <ChevronDownIcon className="size-3" /> : <ChevronRightIcon className="size-3" />}
            Preview
          </button>
        )}
      </div>
      {previewing && preview !== undefined && <ActionPreviews draft={preview} itemId={it.id} name={name} />}
      {open && <TraceList label={`Why, for ${name}`} trace={it.trace} className="ml-6 py-1" />}
    </li>
  )
}

interface ActionPreviewsProps {
  draft: AutopilotDraft
  itemId: number
  name: string
}

// ActionPreviews shows what the draft's GitHub actions would send the item, built and not
// sent: each action with the exact comment or what it does to the labels, or why it can't
// be built for the item.
function ActionPreviews({ draft, itemId, name }: ActionPreviewsProps) {
  const previews = useQuery(() => previewAutopilotActions(draft, itemId), [canonical(draft), itemId])

  if (previews.data === undefined) {
    return previews.error === '' ? (
      <p role="status" className="ml-6 py-1 text-xs text-muted">
        Building what it would send…
      </p>
    ) : (
      <p role="alert" className="ml-6 py-1 text-xs text-danger">
        {previews.error}
      </p>
    )
  }

  return (
    <ul aria-label={`What it would send ${name}`} className="ml-6 flex flex-col gap-1 py-1">
      {previews.data.map((p) => (
        <li key={`${String(p.step)}:${p.action}`} className="flex flex-col gap-0.5 text-xs">
          <span className="font-medium text-text">{p.label}</span>
          {p.problem === '' ? (
            <span className="rounded border border-border bg-raised px-2 py-1 font-mono whitespace-pre-wrap text-text">
              {p.sends}
            </span>
          ) : (
            <span className="text-danger">{p.problem}</span>
          )}
        </li>
      ))}
    </ul>
  )
}

// pinnedWords say what the draft decides about the item it was made from, before a test.
const pinnedWords: Readonly<Record<string, string>> = {
  yes: 'matches now, from what is stored',
  no: 'doesn’t match now: Why ▸ says which part',
  unknown: 'can’t tell yet: Test reads what it waits on',
}

// PinnedRow is the item a routine was made from, at the top of Test until a test reads
// more: whether the draft matches it now, and Why ▸.
function PinnedRow({ pinned }: { pinned: Pinned }) {
  const [open, setOpen] = useState(false)
  const name = `${pinned.item.repo} #${String(pinned.item.number)}`

  return (
    <li className="flex flex-col py-1">
      <div className="flex items-center gap-2 text-sm">
        <ResultMark result={pinned.match} />
        <span className="w-44 shrink-0 truncate font-mono text-[0.75rem] text-muted" title={name}>
          {name}
        </span>
        <span className="min-w-0 flex-1 truncate" title={pinned.item.title}>
          {pinned.item.title}
        </span>
        <span className={`shrink-0 text-xs ${pinned.match === 'yes' ? 'text-text' : 'text-muted'}`}>
          {pinnedWords[pinned.match] ?? pinned.match}
        </span>
        <button
          type="button"
          aria-expanded={open}
          aria-label={`Why, for ${name}`}
          onClick={() => {
            setOpen(!open)
          }}
          className={quietButtonClass}
        >
          {open ? <ChevronDownIcon className="size-3" /> : <ChevronRightIcon className="size-3" />}
          Why
        </button>
      </div>
      {open && <TraceList label={`Why, for ${name}`} trace={pinned.trace} className="ml-6 py-1" />}
    </li>
  )
}
