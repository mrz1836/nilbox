import { describe, expect, it } from 'vitest'

import type { AutopilotRunEvent } from '../platform'
import { agentMatch, agentRun, runEnding, runEvent } from '../test/fixtures'
import {
  agentReport,
  costText,
  diffLines,
  eventKey,
  followedText,
  joinEvents,
  sheetLine,
  statusText,
  stepMarks,
  timelineRows,
  todoMark,
  usageText,
} from './runView'

// tool is a tool's event at line offset, its part 0.
function tool(offset: number, category: AutopilotRunEvent['category'], text: string, name = 'Bash') {
  return runEvent({ seq: offset, offset, kind: 'tool', tool: name, category, text })
}

// result is a tool's result at line offset.
function result(offset: number, error = false) {
  return runEvent({ seq: offset, offset, kind: 'toolResult', text: 'ok', error })
}

describe('timelineRows', () => {
  it('marks each command with how it came out, and one still running while the run goes', () => {
    const events = [
      runEvent({ offset: 0, kind: 'started', text: 'claude-opus-5-5 · Claude Code 2.1.325', at: '' }),
      tool(10, 'command', '$ gh pr checks 88'),
      result(20),
      tool(30, 'command', '$ pnpm test'),
      result(40, true),
      tool(50, 'command', '$ magex lint'),
    ]

    const rows = timelineRows(events, true)

    expect(rows.map((r) => [r.kind, r.text, r.state])).toEqual([
      ['start', 'Started: claude-opus-5-5 · Claude Code 2.1.325', ''],
      ['command', '$ gh pr checks 88', 'ok'],
      ['command', '$ pnpm test', 'failed'],
      ['command', '$ magex lint', 'pending'],
    ])
    // A run that ended has nothing still running.
    expect(timelineRows(events, false).at(-1)?.state).toBe('')
  })

  it('collapses the reads and searches in a row, and leaves the todo list and thinking to the checklist', () => {
    const rows = timelineRows(
      [
        tool(0, 'read', 'package.json', 'Read'),
        result(1),
        tool(2, 'search', 'esbuild', 'Grep'),
        result(3),
        tool(4, 'read', 'pnpm-lock.yaml', 'Read'),
        result(5),
        tool(6, 'read', 'go.mod', 'Read'),
        runEvent({ offset: 7, kind: 'thinking', text: '' }),
        tool(8, 'todo', '1 of 3 done', 'TodoWrite'),
        runEvent({ offset: 9, kind: 'text', text: 'OSV flags esbuild before 0.25.1.' }),
        tool(10, 'edit', 'package.json', 'Edit'),
        result(11),
      ],
      false,
    )

    expect(rows.map((r) => [r.kind, r.text])).toEqual([
      ['reads', 'Looked at package.json, esbuild and 2 more'],
      ['say', 'OSV flags esbuild before 0.25.1.'],
      ['edit', 'Edited package.json'],
    ])
  })

  it('says what the fence refused, the agent’s retries and limits, and its end', () => {
    const rows = timelineRows(
      [
        runEvent({ offset: 0, kind: 'denied', tool: 'Bash', text: 'git push isn’t allowed', error: true }),
        runEvent({ offset: 1, kind: 'limit', text: 'allowed_warning · five_hour' }),
        runEvent({ offset: 2, kind: 'result', text: 'Finished' }),
      ],
      false,
    )

    expect(rows.map((r) => [r.kind, r.text, r.error])).toEqual([
      ['denied', 'Not allowed: Bash. git push isn’t allowed', true],
      ['note', 'allowed_warning · five_hour', false],
      ['result', 'Finished', false],
    ])
  })

  it('shows nothing for a result that answers no tool on the page, as one at its start', () => {
    expect(timelineRows([result(0), tool(1, 'command', '$ ls')], true)).toHaveLength(1)
  })

  it('names every other tool by what it did', () => {
    const rows = timelineRows(
      [
        tool(0, 'subagent', 'Find the advisory', 'Task'),
        tool(1, 'subagent', '', 'Task'),
        tool(2, 'web', 'https://osv.dev/GHSA-xxxx', 'WebFetch'),
        tool(3, 'report', '', 'StructuredOutput'),
        tool(4, 'other', '', 'NotebookRead'),
        tool(5, 'read', 'a.go', 'Read'),
        tool(6, 'read', 'b.go', 'Read'),
        runEvent({ offset: 7, kind: 'started', text: '' }),
        runEvent({ offset: 8, kind: 'denied', tool: 'WebFetch', text: '' }),
      ],
      true,
    )

    expect(rows.map((r) => [r.kind, r.text, r.state])).toEqual([
      ['subagent', 'Subagent: Find the advisory', 'pending'],
      ['subagent', 'Subagent: Task', 'pending'],
      ['tool', 'Read https://osv.dev/GHSA-xxxx', 'pending'],
      ['note', 'Wrote its report', ''],
      ['tool', 'Used NotebookRead', 'pending'],
      ['reads', 'Looked at a.go, b.go', ''],
      ['start', 'Started', ''],
      ['denied', 'Not allowed: WebFetch', ''],
    ])
  })

  it('names a lone read or search by what it looked at', () => {
    expect(timelineRows([tool(0, 'read', 'go.mod', 'Read')], false)[0]?.text).toBe('Read go.mod')
    expect(timelineRows([tool(0, 'search', 'esbuild src', 'Grep')], false)[0]?.text).toBe('Searched for esbuild src')
  })
})

describe('joinEvents', () => {
  it('puts an earlier page first, each event once, by its place in the log', () => {
    const a = runEvent({ seq: 0, offset: 0, part: 0, text: 'a' })
    const b = runEvent({ seq: 0, offset: 0, part: 1, text: 'b' })
    const c = runEvent({ seq: 3, offset: 90, text: 'c' })

    expect(joinEvents([a, b, c], [c]).map((e) => e.text)).toEqual(['a', 'b', 'c'])
    expect(eventKey(b)).toBe('0:1')
  })
})

describe('the header', () => {
  it('numbers the routine’s steps, marking where the match stands', () => {
    expect(stepMarks(agentMatch())).toEqual([
      { label: '1 Agent', current: true, done: false },
      { label: '2 Wait for the checks', current: false, done: false },
    ])
    expect(stepMarks(agentMatch({ state: 'done' })).some((m) => m.current)).toBe(false)
  })

  it('puts the cost against the budget, an estimate marked, or the budget alone while unknown', () => {
    expect(costText(agentRun(), 0)).toBe('budget $3.00')
    expect(costText(agentRun({ cost: 0.61, costEstimated: true }), 0)).toBe('≈ $0.61 of $3.00')
    expect(costText(agentRun({ cost: 0.61 }), 0)).toBe('$0.61 of $3.00')
    expect(costText(agentRun({ unit: 'tokens', budget: 200000 }), 12345)).toBe('12,345 tokens of 200000')
    expect(costText(agentRun({ budget: 0, unit: '' }), 0)).toBe('')
    expect(costText(agentRun({ budget: 0, unit: '', cost: 0.12 }), 0)).toBe('$0.12')
  })

  it('counts the tokens in and out, with the cache’s apart', () => {
    expect(usageText({ tokensIn: 12345, tokensOut: 640, cacheRead: 0 })).toBe('12.3k in · 640 out tokens')
    expect(usageText({ tokensIn: 1, tokensOut: 2, cacheRead: 150000 })).toBe('1 in · 2 out · 150k from cache tokens')
    expect(usageText({ tokensIn: 0, tokensOut: 0, cacheRead: 0 })).toBe('')
  })

  it('says where the run stands, or how it came out', () => {
    expect(statusText(agentRun(), true, null)).toBe('Working')
    expect(statusText(agentRun({ status: 'starting' }), true, null)).toBe('Starting')
    expect(statusText(agentRun({ status: 'ended' }), false, null)).toBe('Ended')
    expect(statusText(agentRun({ status: 'stopping' }), true, null)).toBe('Stopping')
    expect(statusText(agentRun({ status: 'ended' }), false, runEnding())).toBe('Finished: fixed.')
    expect(statusText(agentRun({ status: 'ended', summary: 'Stopped by you.' }), false, null)).toBe('Stopped by you.')
  })
})

describe('agentReport', () => {
  it('is the agent’s own report, else its final message, else why it gave none', () => {
    expect(agentReport(runEnding())).toBe('Moved esbuild to 0.25.1, which fixes GHSA-xxxx.')
    expect(agentReport(runEnding({ reported: false, report: '' }))).toBe(
      'Moved esbuild to 0.25.1; the checks pass here.',
    )
    expect(agentReport(runEnding({ reported: false, report: '', message: '', noReport: 'It was stopped.' }))).toBe(
      'It was stopped.',
    )
  })
})

describe('followedText', () => {
  it('says where a run not in the background is followed', () => {
    expect(followedText('terminal')).toContain('its session’s panel')
    expect(followedText('script')).toContain('Scripts → Runs')
    expect(followedText('github')).toContain('Activity says how each went')
  })
})

describe('todoMark', () => {
  it('marks done, under way, and to do', () => {
    expect([todoMark('completed'), todoMark('in_progress'), todoMark('pending')]).toEqual(['✓', '◐', '○'])
  })
})

describe('diffLines', () => {
  it('tones a patch’s lines', () => {
    expect(diffLines('diff --git a/x b/x\n@@ -1 +1 @@\n-old\n+new\n same\n').map((l) => l.tone)).toEqual([
      'file',
      'hunk',
      'remove',
      'add',
      'plain',
    ])
    expect(diffLines('')).toEqual([])
  })
})

describe('sheetLine', () => {
  it('says what the newest background run made of the item', () => {
    expect(sheetLine(agentMatch())).toBe('Fix failing checks is working on this')

    const pushed = agentRun({ status: 'ended', outcome: 'done', commit: '4f2a9c1abcdef', verdict: 'checks pass' })
    expect(sheetLine(agentMatch({ state: 'verifying', runs: [pushed] }))).toBe(
      'Fix failing checks pushed 4f2a9c1 · checks pass',
    )

    const taken = agentRun({ status: 'ended', outcome: 'stopped' })
    expect(sheetLine(agentMatch({ state: 'done', reason: 'taken_over', runs: [taken] }))).toBe(
      'Fix failing checks: you took it over',
    )

    // A match with no background run leaves it to its chip.
    expect(sheetLine(agentMatch({ runs: [] }))).toBe('')

    const ran = agentRun({ status: 'ended', outcome: 'done' })
    expect(sheetLine(agentMatch({ state: 'done', reason: 'ran', runs: [ran] }))).toBe('Fix failing checks ran')

    const notes = agentRun({ status: 'ended', outcome: 'done', summary: 'Finished: looks good.' })
    expect(sheetLine(agentMatch({ state: 'done', reason: 'ran', runs: [notes] }))).toBe(
      'Fix failing checks: Finished: looks good.',
    )
  })
})
