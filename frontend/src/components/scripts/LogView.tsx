import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState, type UIEvent } from 'react'

import { lineCount, linesFrom, type AnsiLine, type AnsiSpan, type AnsiTone } from '../../lib/ansi'
import { attempt, cancel, start } from '../../lib/call'
import {
  appended,
  chunkBytes,
  earlierRead,
  emptyTail,
  followed,
  fromTail,
  hasEarlier,
  pinnedAt,
  revealed,
  shownFrom,
  withEarlier,
} from '../../lib/logTail'
import { remToPx } from '../../lib/rem'
import { scriptRunOutput, type ScriptRunChunk } from '../../platform'
import { quietButtonClass } from '../controls'
import { ErrorText } from '../settings/rows'

// pollMs is how long the view waits, once it has read all of a live run's log, before it
// reads again for new output.
export const pollMs = 750

// retryMs is how long it waits after a read of a live run's log failed.
export const retryMs = 3000

// blockLines groups the lines in blocks the browser skips while off screen
// (content-visibility), as the inbox skips its rows; a block's place holds as many lines
// until it has been drawn once.
const blockLines = 50

// lineRem is a line's height, in rem; the view follows the output while its end is
// within one line of view.
const lineRem = 1.35

// toneClasses draw each tone in the theme's color.
const toneClasses: Readonly<Record<AnsiTone, string>> = {
  muted: 'text-muted',
  danger: 'text-danger',
  success: 'text-success',
  warning: 'text-warning',
  info: 'text-info',
  accent: 'text-accent',
  text: 'text-text',
}

// spanClass is a span's color and weight, or undefined for plain text.
function spanClass({ tone, bold }: AnsiSpan): string | undefined {
  const classes = [tone === null ? '' : toneClasses[tone], bold ? 'font-bold' : ''].filter((c) => c !== '')

  return classes.length === 0 ? undefined : classes.join(' ')
}

// LogLine is one line of the log, as text. An empty line keeps a line's height.
function LogLine({ line }: { line: AnsiLine }) {
  return (
    <div className="min-h-[1.35rem] break-all whitespace-pre-wrap">
      {line.map((span, i) => (
        <span key={i} className={spanClass(span)}>
          {span.text}
        </span>
      ))}
    </div>
  )
}

export interface LogViewProps {
  // runId is the run whose log shows.
  runId: number
  // live reports a run still going. The view reads its log for new output until the log
  // says it is done, whether or not the run is live; a live run's failed read is tried
  // again.
  live: boolean
}

// LogView shows a run's log: its text in the theme's colors, never markup, in lines a
// terminal would show (lib/ansi). It reads the log's tail first and shows the newest lines
// (lib/logTail), with Show earlier output to read back from there. While more may come it
// reads on from where it left off, and follows the new output down until the user scrolls
// up, then offers Jump to latest. A log cut down as its run ended is read again from its
// tail. The status of the run is the panel's to announce, not the log's lines.
export function LogView({ runId, live }: LogViewProps) {
  const [log, setLog] = useState(emptyTail)
  const [error, setError] = useState('')
  const [reading, setReading] = useState(false)
  // reloads counts the times the log was found cut down by a read for earlier output,
  // each of which reads it again from its tail.
  const [reloads, setReloads] = useState(0)
  const scroller = useRef<HTMLDivElement>(null)
  const isLive = useEffectEvent(() => live)
  const following = log.pinned === null

  // Read the tail, then on from where each read left off, at once while more is there,
  // after pollMs once the read caught up, until the log is done.
  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout> | undefined
    // current holds the read under way, which is cancelled as the view goes.
    const current: PromiseLike<ScriptRunChunk>[] = []
    // next is where the next read starts, or null to read the tail, and size the log's size
    // as the last read found it.
    let next: number | null = null
    let size = 0

    const read = () => {
      const from = next
      const call = start(() => scriptRunOutput(runId, from ?? -1, chunkBytes))

      current.splice(0, current.length, call)
      void attempt(call).then((result) => {
        if (stopped) {
          return
        }

        if (!result.ok) {
          setError(result.error)

          if (isLive()) {
            wait(retryMs)
          }

          return
        }

        const chunk = result.data

        setError('')

        // A log read past its end, or smaller than it was, was cut down as its run ended:
        // it is read again from its tail.
        if (chunk.restart || chunk.size < size) {
          next = null
          size = 0
          wait(0)

          return
        }

        size = chunk.size
        setLog((was) => (from === null ? fromTail(chunk, was) : appended(was, chunk)))
        next = chunk.next

        if (!chunk.done) {
          wait(chunk.next < chunk.size ? 0 : pollMs)
        }
      })
    }

    const wait = (ms: number) => {
      timer = setTimeout(read, ms)
    }

    read()

    return () => {
      stopped = true
      clearTimeout(timer)
      current.forEach(cancel)
    }
  }, [runId, reloads])

  // While the view follows the output, its end stays in view as lines arrive.
  useLayoutEffect(() => {
    const el = scroller.current

    if (following && el !== null) {
      el.scrollTop = el.scrollHeight
    }
  }, [following, log])

  // A scroll away from the end holds the window where it is; one back to the end follows
  // the output again.
  const onScroll = (e: UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight <= remToPx(lineRem)

    if (atEnd !== following) {
      setLog(atEnd ? followed : pinnedAt)
    }
  }

  // showEarlier shows the lines held above the window, or reads the part of the log
  // before them. What it read joins the log only if the log is still the one it was read
  // for.
  const showEarlier = async () => {
    const want = earlierRead(log)

    if (want === null) {
      setLog(revealed)

      return
    }

    setReading(true)

    const result = await attempt(scriptRunOutput(runId, want.offset, want.limit))

    setReading(false)

    if (!result.ok) {
      setError(result.error)
    } else if (result.data.restart || result.data.size < log.size) {
      // The log was cut down since it was read: it is read again from its tail.
      setReloads((n) => n + 1)
    } else {
      const chunk = result.data

      setError('')
      setLog((was) => (was.reads === log.reads && was.start === log.start ? withEarlier(was, chunk) : was))
    }
  }

  const total = lineCount(log.ansi)
  const first = shownFrom(log)
  const shown = linesFrom(log.ansi, first)
  // blocks hold the lines shown, cut at every blockLines-th line of the log, so a block
  // keeps its lines as the window moves.
  const blocks: { at: number; lines: readonly AnsiLine[] }[] = []

  for (let at = first - (first % blockLines); at < total; at += blockLines) {
    const from = Math.max(at, first)

    blocks.push({ at: from, lines: shown.slice(from - first, at + blockLines - first) })
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-1">
      <div className="relative flex min-h-0 flex-1 flex-col">
        <div
          ref={scroller}
          role="region"
          aria-label="Output"
          // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- the log scrolls, and a scrolling region must take focus so the keyboard can scroll it (WCAG 2.1.1)
          tabIndex={0}
          onScroll={onScroll}
          className="min-h-0 flex-1 overflow-y-auto rounded-md border border-border bg-background px-3 py-2 font-mono text-xs leading-[1.35rem] text-text"
        >
          {hasEarlier(log) && (
            <button
              type="button"
              disabled={reading}
              onClick={() => {
                void showEarlier()
              }}
              className={`${quietButtonClass} mb-1 font-sans`}
            >
              {reading ? 'Reading earlier output…' : 'Show earlier output'}
            </button>
          )}
          {log.reads === 0
            ? error === '' && <p className="font-sans text-muted">Reading the output…</p>
            : total === 0 && <p className="font-sans text-muted">{live ? 'No output yet.' : 'No output.'}</p>}
          {blocks.map((block) => (
            <div key={block.at} className="[contain-intrinsic-size:auto_67.5rem] [content-visibility:auto]">
              {block.lines.map((line, i) => (
                <LogLine key={block.at + i} line={line} />
              ))}
            </div>
          ))}
        </div>
        {!following && (
          <button
            type="button"
            onClick={() => {
              setLog(followed)
            }}
            className="absolute right-4 bottom-3 rounded-full border border-border bg-surface px-3 py-1 text-xs font-medium text-text shadow-md transition-colors hover:bg-raised"
          >
            Jump to latest
          </button>
        )}
      </div>
      <ErrorText error={error} />
    </div>
  )
}
