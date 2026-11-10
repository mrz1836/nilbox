// A run's log as the log view holds it: the part read so far, from some point in the log
// to where the next read starts, read into lines (lib/ansi), and which of them show.
//
// The view shows a tail window: the newest tailLines lines, sliding along as new output
// arrives, while it follows the output. Once the user scrolls away, or asks for earlier
// output, the window's first line is pinned, so nothing they are reading moves; new lines
// still join its end. Show earlier output shows the lines held above the window, and
// once they all show, reads the part of the log before them.
//
// Offsets are the log's, in bytes: where the text held starts (start) and where the next
// read starts (next). Reading earlier output asks for the bytes just before start. A log
// only grows while its run goes; one that reads smaller than before (size) was cut down
// as its run ended, and is read again from its tail.

import { dropLines, emptyAnsi, feedAnsi, lineCount, type AnsiState } from './ansi'

// tailLines is how many of the newest lines show at first, and how many more each Show
// earlier output adds.
export const tailLines = 2000

// chunkBytes is how much of the log one read asks for: the most the backend sends.
export const chunkBytes = 256 * 1024

// keepChars is how much of the log is held: past it, the oldest lines are let go, down to
// about half of it, so a run that prints for hours costs no more than one that doesn't.
// While the window is pinned, only the lines above it go, until the log holds twice as
// much; then the oldest go whatever shows, as a terminal's scrollback does. Show earlier
// output reads what went again.
export const keepChars = 4 * 1024 * 1024

// Chunk is a part of the log as the backend sends it, with the log's size as it read it.
export interface Chunk {
  text: string
  offset: number
  next: number
  size: number
}

// LogTail is the log as held: its text from start up to next, read into lines; the log's
// size as the last read found it; the first line shown, or null while the newest
// tailLines show; and how many times the log was read from its tail, which a read made
// for an older one is checked against.
export interface LogTail {
  readonly raw: string
  readonly start: number
  readonly next: number
  readonly size: number
  readonly ansi: AnsiState
  readonly pinned: number | null
  readonly reads: number
}

// emptyTail is a log before its first read.
export const emptyTail: LogTail = { raw: '', start: 0, next: 0, size: 0, ansi: emptyAnsi, pinned: null, reads: 0 }

// fromTail is the log as a read of its tail found it, following its output.
export function fromTail(chunk: Chunk, was: LogTail): LogTail {
  return {
    raw: chunk.text,
    start: chunk.offset,
    next: chunk.next,
    size: chunk.size,
    ansi: feedAnsi(emptyAnsi, chunk.text),
    pinned: null,
    reads: was.reads + 1,
  }
}

// lineEnds finds where the first lines of raw end: as many as end before limit, up to
// most. It returns how many, and where the line after them starts.
function lineEnds(raw: string, limit: number, most: number): { lines: number; at: number } {
  let lines = 0
  let at = 0

  for (let end = raw.indexOf('\n'); lines < most && end !== -1 && end < limit; end = raw.indexOf('\n', at)) {
    lines++
    at = end + 1
  }

  return { lines, at }
}

// trimmed is log with its oldest lines let go once it holds more than keep (keepChars),
// as far as the window allows: see keepChars. Each finished line is one line feed of raw,
// so the lines let go are counted off raw, and the style in force carries on.
function trimmed(log: LogTail, keep: number): LogTail {
  const { raw, pinned } = log
  // most is how many lines may go: any while the window follows, or the log holds twice
  // keep; only those above the window while it is pinned.
  const most = pinned === null || raw.length > keep * 2 ? Infinity : pinned

  if (raw.length <= keep || most === 0) {
    return log
  }

  const { lines, at } = lineEnds(raw, raw.length - Math.floor(keep / 2), most)

  // A line longer than half of keep has no end to cut at yet.
  if (lines === 0) {
    return log
  }

  return {
    ...log,
    raw: raw.slice(at),
    start: log.start + new TextEncoder().encode(raw.slice(0, at)).length,
    ansi: dropLines(log.ansi, lines),
    pinned: pinned === null ? null : Math.max(0, pinned - lines),
  }
}

// appended is log with chunk, read from where it left off, joined at its end, and its
// oldest lines let go once it holds too much (keepChars). It costs what chunk holds, and,
// once in a while, what is let go.
export function appended(log: LogTail, chunk: Chunk, keep: number = keepChars): LogTail {
  const grown: LogTail = {
    ...log,
    raw: log.raw + chunk.text,
    next: chunk.next,
    size: chunk.size,
    ansi: feedAnsi(log.ansi, chunk.text),
  }

  return trimmed(grown, keep)
}

// shownFrom is the first line shown.
export function shownFrom(log: LogTail): number {
  return log.pinned ?? Math.max(0, lineCount(log.ansi) - tailLines)
}

// hasEarlier reports output above what shows: lines held, or a part of the log not read.
export function hasEarlier(log: LogTail): boolean {
  return shownFrom(log) > 0 || log.start > 0
}

// earlierRead is the read Show earlier output needs: the part of the log just before
// what is held, as an offset and a byte count, or null when the lines it shows are held.
export function earlierRead(log: LogTail): { offset: number; limit: number } | null {
  if (shownFrom(log) > 0) {
    return null
  }

  const offset = Math.max(0, log.start - chunkBytes)

  return { offset, limit: log.start - offset }
}

// revealed is log with tailLines more of the lines held above the window showing.
export function revealed(log: LogTail): LogTail {
  return { ...log, pinned: Math.max(0, shownFrom(log) - tailLines) }
}

// withEarlier is log with chunk, the part of the log just before it, joined at its start,
// and tailLines more lines showing. The backend starts a read at a character's start, and
// reads at least a whole character, so a read of a log's first bytes can run on into what
// is held: that part is cut off at start, a character's start too.
export function withEarlier(log: LogTail, chunk: Chunk): LogTail {
  const text =
    chunk.next > log.start
      ? new TextDecoder().decode(new TextEncoder().encode(chunk.text).subarray(0, log.start - chunk.offset))
      : chunk.text
  const raw = text + log.raw
  const ansi = feedAnsi(emptyAnsi, raw)
  // The lines read join above the window: its first line moves down by as many.
  const added = lineCount(ansi) - lineCount(log.ansi)

  return { ...log, raw, start: chunk.offset, ansi, pinned: Math.max(0, shownFrom(log) + added - tailLines) }
}

// pinnedAt is log with the window held where it is, as the user scrolls away from the
// newest output.
export function pinnedAt(log: LogTail): LogTail {
  return { ...log, pinned: shownFrom(log) }
}

// followed is log following its output again: the newest tailLines show.
export function followed(log: LogTail): LogTail {
  return { ...log, pinned: null }
}
