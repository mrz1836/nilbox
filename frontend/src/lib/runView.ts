// What the run view shows of an agent's background run, worked out from the event model
// every agent's stream is read into (runevent), so nothing here branches on the agent:
// the timeline's rows, the plan's checklist, the routine's steps, and the header's words.

import type {
  AutopilotMatch,
  AutopilotRun,
  AutopilotRunEnding,
  AutopilotRunEvent,
  AutopilotRunProgress,
} from '../platform'
import { counted } from './format'

// presetLabels name the tools presets, as the fence names them.
export const presetLabels: Readonly<Record<string, string>> = {
  readOnly: 'Read only',
  githubTasks: 'GitHub tasks only',
  editTest: 'Edit and test, no push',
  editTestPush: 'Edit, test, and push',
  profile: 'The profile’s own permissions',
}

// eventKey names an event: its line's offset in the run's log and which of that line's
// events it is, which a page read back from the log and the events followed from memory
// share.
export function eventKey(e: Pick<AutopilotRunEvent, 'offset' | 'part'>): string {
  return `${String(e.offset)}:${String(e.part)}`
}

// joinEvents puts an earlier page before the events shown, each event once, oldest first.
export function joinEvents(
  earlier: readonly AutopilotRunEvent[],
  shown: readonly AutopilotRunEvent[],
): AutopilotRunEvent[] {
  const seen = new Set(shown.map(eventKey))

  return [...earlier.filter((e) => !seen.has(eventKey(e))), ...shown].sort(
    (a, b) => a.offset - b.offset || a.part - b.part,
  )
}

// appendEvents adds the events followed since to those shown, each once.
export function appendEvents(
  shown: readonly AutopilotRunEvent[],
  more: readonly AutopilotRunEvent[],
): AutopilotRunEvent[] {
  return joinEvents(shown, more)
}

// RowKind is what a row of the timeline shows.
export type RowKind =
  'start' | 'say' | 'command' | 'edit' | 'reads' | 'subagent' | 'tool' | 'denied' | 'note' | 'result'

// RowState is how a command or a tool came out: it worked, it failed, or it hasn't
// answered yet.
export type RowState = 'ok' | 'failed' | 'pending' | ''

// TimelineRow is one row of the run view's timeline.
export interface TimelineRow {
  key: string
  at: string
  kind: RowKind
  text: string
  state: RowState
  // error marks a row that says something went wrong: a refusal, a limit, a failed end.
  error: boolean
}

// toolRow is the row of a tool's event, or null for one the timeline leaves to the
// checklist (the todo list) or shows nothing of.
function toolRow(e: AutopilotRunEvent): TimelineRow | null {
  const base = { key: eventKey(e), at: e.at, state: 'pending' as RowState, error: false }

  switch (e.category) {
    case 'command':
      return { ...base, kind: 'command', text: e.text }
    case 'edit':
      return { ...base, kind: 'edit', text: `Edited ${e.text}` }
    case 'read':
    case 'search':
      return { ...base, kind: 'reads', text: e.text }
    case 'subagent':
      return { ...base, kind: 'subagent', text: `Subagent: ${e.text === '' ? e.tool : e.text}` }
    case 'web':
      return { ...base, kind: 'tool', text: `Read ${e.text}` }
    case 'todo':
      return null
    case 'report':
      return { ...base, kind: 'note', text: 'Wrote its report', state: '' }
    case 'other':
      return { ...base, kind: 'tool', text: `Used ${e.tool}` }
  }
}

// noteRow is the row of an event that isn't a tool's.
function noteRow(e: AutopilotRunEvent): TimelineRow | null {
  const base = { key: eventKey(e), at: e.at, state: '' as RowState, error: e.error }

  switch (e.kind) {
    case 'started':
      return { ...base, kind: 'start', text: e.text === '' ? 'Started' : `Started: ${e.text}` }
    case 'text':
      return { ...base, kind: 'say', text: e.text }
    case 'denied':
      return {
        ...base,
        kind: 'denied',
        text: `Not allowed: ${e.tool}${e.text === '' ? '' : `. ${e.text}`}`,
        error: true,
      }
    case 'retry':
    case 'task':
    case 'limit':
      return { ...base, kind: 'note', text: e.text }
    case 'result':
      return { ...base, kind: 'result', text: e.text }
    default:
      return null
  }
}

// Look is one read or search of a run of them: what it looked at, and whether it searched.
interface Look {
  text: string
  search: boolean
}

// readsText says what a run of reads and searches looked at: one by its file or pattern,
// several by the first two and a count.
function readsText(looks: readonly Look[]): string {
  const [first, second] = looks

  if (looks.length === 1 && first !== undefined) {
    return first.search ? `Searched for ${first.text}` : `Read ${first.text}`
  }

  const more = looks.length - 2

  return `Looked at ${first?.text ?? ''}, ${second?.text ?? ''}${more > 0 ? ` and ${counted(more, 'more', 'more')}` : ''}`
}

// timelineRows turns a run's events into its timeline's rows, oldest first: a command
// marked with how it came out once its result comes (pending while the run goes, and
// unanswered once it ended), the reads and searches in a row collapsed into one, what the
// agent said, what the fence refused, and its end. Thinking and the todo list, which the
// checklist shows, have no row; a result that answers no tool shown, as one at a page's
// start, has none either.
export function timelineRows(events: readonly AutopilotRunEvent[], live: boolean): TimelineRow[] {
  const rows: TimelineRow[] = []
  const waiting: TimelineRow[] = []
  let reads: Look[] = []

  const closeReads = () => {
    const last = rows.at(-1)

    if (last?.kind === 'reads' && reads.length > 0) {
      last.text = readsText(reads)
    }

    reads = []
  }

  for (const e of events) {
    if (e.kind === 'toolResult') {
      const answered = waiting.shift()

      if (answered !== undefined && answered.kind !== 'reads') {
        answered.state = e.error ? 'failed' : 'ok'
      }

      continue
    }

    const row = e.kind === 'tool' ? toolRow(e) : noteRow(e)

    if (row === null) {
      continue
    }

    if (e.kind === 'tool') {
      waiting.push(row)
    }

    const look = { text: row.text, search: e.category === 'search' }

    if (row.kind === 'reads' && rows.at(-1)?.kind === 'reads') {
      reads.push(look)

      continue
    }

    closeReads()

    if (row.kind === 'reads') {
      reads = [look]
    }

    rows.push(row)
  }

  closeReads()

  if (!live) {
    for (const row of rows) {
      if (row.state === 'pending') {
        row.state = ''
      }
    }
  }

  for (const row of rows) {
    if (row.kind === 'reads') {
      row.state = ''
    }
  }

  return rows
}

// todoMark is a todo's mark on the plan's checklist: ✓ done, ◐ under way, ○ to do.
export function todoMark(status: string): string {
  switch (status) {
    case 'completed':
      return '✓'
    case 'in_progress':
      return '◐'
    default:
      return '○'
  }
}

// StepMark is one of the routine's steps as the header numbers them, and where the match
// stands on it.
export interface StepMark {
  label: string
  current: boolean
  done: boolean
}

// stepMarks are the routine's steps as the run view's header lists them: "1 Agent · 2 Wait
// for the checks · 3 Merge it", the one the match is at marked while it is open.
export function stepMarks(m: AutopilotMatch): StepMark[] {
  const open = ['waiting', 'blocked', 'queued', 'running', 'verifying'].includes(m.state)

  return m.steps.map((s, i) => ({
    label: `${String(i + 1)} ${s.label}`,
    current: open && i === m.step,
    done: s.status === 'done',
  }))
}

// money writes dollars as the header does: "$0.61".
function money(n: number): string {
  return `$${n.toFixed(2)}`
}

// costText is the run's cost against its budget, as the agent reports it: "≈ $0.61 of
// $3.00" for an estimate on the user's plan, the budget alone while the agent hasn't said,
// or its tokens against a budget in tokens.
export function costText(run: AutopilotRun, tokens: number): string {
  if (run.unit === 'tokens' && run.budget > 0) {
    return `${counted(tokens, 'token', 'tokens')} of ${String(run.budget)}`
  }

  const budget = run.unit === 'usd' && run.budget > 0 ? money(run.budget) : ''

  if (run.cost !== null) {
    const spent = `${run.costEstimated ? '≈ ' : ''}${money(run.cost)}`

    return budget === '' ? spent : `${spent} of ${budget}`
  }

  return budget === '' ? '' : `budget ${budget}`
}

// thousands writes a count of tokens short: "640", "12.3k", "120k".
export function thousands(n: number): string {
  if (n < 1000) {
    return String(n)
  }

  return `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1).replace(/\.0$/, '')}k`
}

// usageText says a run's tokens: what went in and came out, with what its provider read
// from its cache apart, as the header counts them.
export function usageText(p: Pick<AutopilotRunProgress, 'tokensIn' | 'tokensOut' | 'cacheRead'>): string {
  if (p.tokensIn + p.tokensOut + p.cacheRead === 0) {
    return ''
  }

  const parts = [`${thousands(p.tokensIn)} in`, `${thousands(p.tokensOut)} out`]

  if (p.cacheRead > 0) {
    parts.push(`${thousands(p.cacheRead)} from cache`)
  }

  return `${parts.join(' · ')} tokens`
}

// statusText is where a run stands, as the header's first word says it: working, starting,
// or stopping while it goes; how it came out once it ended.
export function statusText(run: AutopilotRun, live: boolean, ending: AutopilotRunEnding | null): string {
  if (live) {
    switch (run.status) {
      case 'starting':
        return 'Starting'
      case 'stopping':
        return 'Stopping'
      default:
        return 'Working'
    }
  }

  if (ending !== null && ending.summary !== '') {
    return ending.summary
  }

  return run.summary === '' ? 'Ended' : run.summary
}

// followedText says where a run that isn't an agent's in the background is followed, as
// the run view says in place of its timeline: a session in the terminal on its item's row,
// a script's in Scripts → Runs, and a GitHub action's in Activity.
export function followedText(runner: string): string {
  switch (runner) {
    case 'terminal':
      return 'It runs in your terminal, not in the background: its session’s panel, on the item’s row, follows it.'
    case 'script':
      return 'It runs as a script: Scripts → Runs shows its log.'
    default:
      return 'It sends GitHub actions: Activity says how each went.'
  }
}

// agentReport is what the agent said of its own run, as plain text: its report's summary,
// else its final message, else why it gave no report.
export function agentReport(ending: AutopilotRunEnding): string {
  if (ending.reported && ending.report !== '') {
    return ending.report
  }

  return ending.message === '' ? ending.noReport : ending.message
}

// shortCommit is a commit's first seven characters.
export function shortCommit(oid: string): string {
  return oid.slice(0, 7)
}

// DiffLine is a line of a patch, with how the run view colors it.
export interface DiffLine {
  text: string
  tone: 'add' | 'remove' | 'hunk' | 'file' | 'plain'
}

// diffLines splits a unified diff into its lines, each with its tone.
export function diffLines(patch: string): DiffLine[] {
  if (patch === '') {
    return []
  }

  return patch
    .replace(/\n$/, '')
    .split('\n')
    .map((text) => {
      if (text.startsWith('diff --git') || text.startsWith('+++') || text.startsWith('---')) {
        return { text, tone: 'file' as const }
      }

      if (text.startsWith('@@')) {
        return { text, tone: 'hunk' as const }
      }

      if (text.startsWith('+')) {
        return { text, tone: 'add' as const }
      }

      if (text.startsWith('-')) {
        return { text, tone: 'remove' as const }
      }

      return { text, tone: 'plain' as const }
    })
}

// agentRunOf is the background agent's run of a match that the run view shows, the newest
// first: the run an item's sheet links to.
export function agentRunOf(m: AutopilotMatch): AutopilotRun | undefined {
  return m.runs.filter((r) => r.runner === 'background').at(-1)
}

// sheetLine is the one line a sheet says, under its work line, of the newest background run
// a routine made on its item: "⚡ Fix failing checks is working on this", "⚡ Fix failing
// checks pushed 4f2a9c1 · checks pass", or how it came out. A match with no such run says
// nothing: its chip does.
export function sheetLine(m: AutopilotMatch): string {
  const run = agentRunOf(m)

  if (run === undefined) {
    return ''
  }

  const verdict = run.verdict === '' ? '' : ` · ${run.verdict}`

  if (run.status !== 'ended') {
    return `${m.routine} is working on this`
  }

  if (m.reason === 'taken_over') {
    return `${m.routine}: you took it over`
  }

  if (run.commit !== '') {
    return `${m.routine} pushed ${shortCommit(run.commit)}${verdict}`
  }

  return run.summary === '' ? `${m.routine} ran${verdict}` : `${m.routine}: ${run.summary}${verdict}`
}
