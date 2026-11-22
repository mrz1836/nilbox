import { Fragment, useId, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'

import { agentName } from '../../lib/launch'
import {
  ageTitle,
  canMark,
  checkedAgo,
  checkedTitle,
  cloneNotes,
  legendOf,
  markLabel,
  markTitle,
  modulesText,
  modulesTitle,
  staleTone,
  toolsText,
  type DepsGroup,
  type DepsNote,
} from '../../lib/manifests'
import { cardSession, sinceWhen, stateLabel, stateText, stateTint } from '../../lib/sessions'
import { formatAge, formatAgo } from '../../lib/time'
import { useNowAs } from '../../lib/useNow'
import type { Tone } from '../../lib/worktreeParts'
import type { AgentInfo, AgentSession, ManifestsClone } from '../../platform'
import { AlertIcon, ChevronRightIcon, CircleCheckIcon, PinIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { SessionDot } from '../SessionDot'
import { toolButtonClass } from '../worktrees/layout'
import { DepsDrawer } from './DepsDrawer'

// The Deps page's rows: one line per clone, in columns its whole group shares, and under
// an unfolded one its drawer (DepsDrawer). The list is the grid and each row a subgrid of
// it, so the columns line up down the group, and the stalest manifest takes the room the
// rest leave. In a list under 56rem that manifest and the notes move to a second line,
// under the repository, and in one under 36rem the modules and the tools join them there,
// the age staying beside the repository. The list's width is what counts, not the
// window's: the list is the container the rows ask, as on the Release page.

// depsListClass is a group's list: the container its rows' layout follows, and the grid
// of their nine columns: the staleness edge, the repository, its modules, its tools, its
// stalest manifest, the notes, the age, Upgrade…, and the chevron. The edge's column is
// 0.4375rem, not the edge's own 0.1875rem, as on a Release row: a subgrid adds its padding
// to the items in its outer columns as margin, so the row's 0.25rem of left padding takes
// the rest, leaving the edge its width, inset from the row's rounded corner.
export const depsListClass =
  '@container grid grid-cols-[0.4375rem_fit-content(22rem)_auto_auto_minmax(0,1fr)_fit-content(16rem)_auto_auto_auto] gap-x-3'

// rowClass is a row: a subgrid of the list's columns, every cell placed in it by its own
// classes, and in a narrow list a grid of its own, five columns wide.
const rowClass =
  'group/row relative col-span-full grid grid-cols-subgrid items-center rounded-md py-1 pr-1 pl-1 hover:bg-raised/60 focus-within:bg-raised/60 data-open:bg-raised/75 @max-4xl:py-1.5 @max-xl:grid-cols-[0.1875rem_minmax(0,1fr)_auto_auto_auto] @max-xl:gap-x-2'

// metaClass holds the modules, the tools, the stalest manifest, and the notes: no box of
// its own while they sit in the row's columns, and in a narrow list the second line they
// share.
const metaClass =
  'contents @max-xl:col-[2/-1] @max-xl:row-start-2 @max-xl:flex @max-xl:min-w-0 @max-xl:items-center @max-xl:gap-2'

// pathClass is the stalest manifest's cell, which a clone that couldn't be read gives its
// error.
const pathClass =
  'col-start-5 row-start-1 flex min-w-0 items-center gap-1.5 text-xs @max-4xl:col-[2/6] @max-4xl:row-start-2 @max-xl:flex-1'

// rowButtonClass is a row's Upgrade…: quiet muted words until the row is hovered or holds
// the focus, when it is drawn as a button, as a Release row's Release… is, so a column of
// them stays calm and each is still found where it always is.
const rowButtonClass =
  'inline-flex items-center rounded-md border border-transparent px-2 py-0.5 text-xs font-medium whitespace-nowrap text-muted transition-colors hover:bg-raised group-hover/row:border-border group-hover/row:bg-surface group-hover/row:text-text group-focus-within/row:border-border group-focus-within/row:bg-surface group-focus-within/row:text-text focus-visible:outline-2 focus-visible:outline-accent'

// markButtonClass is a row's mark: a quiet icon, as a Worktrees row's tools are, which
// turns green while the clone is marked up to date, so the marked rows show it down a
// column.
const markButtonClass = `${toolButtonClass} data-checked:text-success`

// toneEdges color a row's edge, and the dots of its group's legend, by the tone its age
// takes (staleTone): the tone's own fill, and a faint one for an age that isn't known, as
// a Release row without checks has. Each class is spelled out in full so Tailwind finds
// it.
const toneEdges: Readonly<Record<Tone, string>> = {
  info: 'bg-info',
  warning: 'bg-warning',
  caution: 'bg-caution',
  success: 'bg-success',
  danger: 'bg-danger',
  muted: 'bg-muted/30',
}

// selectingText reports a click that ends a drag across text, which selects the words
// rather than acting on the row they're in, as a Release row reads it.
function selectingText(): boolean {
  return (window.getSelection()?.toString() ?? '') !== ''
}

// controls are what a row's click leaves to themselves: its buttons and links, and the
// cells of Upgrade… and the chevron. A row renders nothing out of its place, so every
// click that reaches it lands inside it.
const controls = 'button, a, input, label, select, textarea, [data-row-controls]'

// onAControl reports a click on one of a row's controls.
function onAControl(e: MouseEvent<HTMLElement>): boolean {
  return (e.target as Element).closest(controls) !== null
}

// neighbor is the chevron of the row step rows after the one whose chevron from is, across
// every group on the page, or undefined past either end.
function neighbor(from: HTMLElement, step: number): HTMLElement | undefined {
  const toggles = Array.from(document.querySelectorAll<HTMLElement>('[data-deps-toggle]'))

  return toggles[toggles.indexOf(from) + step]
}

// repoParts splits "owner/name" after its first slash, the owner keeping the slash; a
// repository without one is all name.
function repoParts(repo: string): { owner: string; name: string } {
  const cut = repo.indexOf('/') + 1

  return { owner: repo.slice(0, cut), name: repo.slice(cut) }
}

// RepoCell is a row's repository: a muted owner and the name in medium weight, cut short
// with the whole and the clone's folder on hover, and the pin after it when the user
// pinned it, so the names still line up.
function RepoCell({ clone }: { clone: ManifestsClone }) {
  const { owner, name } = repoParts(clone.repo)

  return (
    <span className="col-start-2 row-start-1 flex min-w-0 items-center gap-1.5 text-sm">
      <span className="truncate" title={`${clone.repo}\n${clone.dir}`}>
        <span className="text-muted">{owner}</span>
        <span className="font-medium">{name}</span>
      </span>
      {clone.pinned && (
        <span
          role="img"
          aria-label="Pinned"
          title="Pinned: unpin it in Settings → Repositories"
          className="inline-flex shrink-0 text-accent"
        >
          <PinIcon filled className="size-3.5" />
        </span>
      )}
    </span>
  )
}

// PathCell is a row's stalest manifest, the counted one whose dependencies changed longest
// ago, none when none counts; or, for a clone that couldn't be read, why, in amber, cut
// short with the whole on hover.
function PathCell({ clone }: { clone: ManifestsClone }) {
  if (clone.error !== '') {
    return (
      <span title={clone.error} className={`${pathClass} font-medium text-warning`}>
        <AlertIcon className="size-3.5 shrink-0" />
        <span className="truncate">{clone.error}</span>
      </span>
    )
  }

  // stalest is the counted module that changed longest ago, which the modules list first;
  // with none counted, the row names none.
  const stalest = clone.counted > 0 ? clone.modules[0] : undefined

  if (stalest === undefined) {
    return null
  }

  return (
    <span
      title={clone.counted === 1 ? stalest.path : `${stalest.path}, the one that changed longest ago`}
      className={`${pathClass} text-muted`}
    >
      <span className="truncate font-mono">{stalest.path}</span>
    </span>
  )
}

// UpgradeTag follows an upgrade of the repository, as an inbox card's session tag does
// (SessionTag), in words alone: a status dot and "Claude · working · 4m", on a faint
// backing while it works, waits on the user, or failed. Its tooltip names the prompt, and
// the worktree it works in.
function UpgradeTag({ session, agents }: { session: AgentSession; agents: readonly AgentInfo[] }) {
  const name = agentName(agents, session.agent)
  const label = stateLabel(session.state)
  const since = sinceWhen(session)
  const what = session.detail === '' ? `${name} ${label}` : `${name} ${label}: ${session.detail}`

  return (
    <span
      title={`${session.title}: ${what}\nIn ${session.dir}`}
      className={`inline-flex max-w-56 min-w-0 shrink-0 cursor-help items-center gap-1.5 rounded-full px-2 py-px text-caption leading-4 font-medium ${stateText[session.state]} ${stateTint[session.state]}`}
    >
      <SessionDot state={session.state} />
      <span className="sr-only">Upgrade: </span>
      <span className="truncate">
        {name} · {label}
        {since !== '' && (
          <>
            {' · '}
            <RelativeTime iso={since} show={formatAge} />
          </>
        )}
      </span>
    </span>
  )
}

// NoteMark is one note about how a clone was read, muted, its sentence in its tooltip.
function NoteMark({ note }: { note: DepsNote }) {
  return (
    <span title={note.title} className="inline-flex min-w-0 cursor-help items-center text-muted">
      <span className="truncate">{note.text}</span>
    </span>
  )
}

// CheckedMark says when the user marked the clone up to date, "checked 3d ago", muted, as
// of the clock, its tooltip saying when in full and what the mark does now (checkedTitle).
function CheckedMark({ clone }: { clone: ManifestsClone }) {
  return (
    <span title={checkedTitle(clone)} className="inline-flex min-w-0 cursor-help items-center text-muted">
      <RelativeTime iso={clone.checkedAt} show={checkedAgo} className="truncate" />
    </span>
  )
}

interface NotesCellProps {
  clone: ManifestsClone
  session: AgentSession | undefined
  agents: readonly AgentInfo[]
  notes: readonly DepsNote[]
}

// NotesCell is what to know about a clone: the upgrade an agent works on in it, when the
// user marked it up to date, then how it was read (cloneNotes). A comma between them, for a
// screen reader, keeps them apart when read as text; the gap does on screen.
function NotesCell({ clone, session, agents, notes }: NotesCellProps) {
  const parts: { key: string; node: ReactNode }[] = [
    ...(session === undefined ? [] : [{ key: 'session', node: <UpgradeTag session={session} agents={agents} /> }]),
    ...(clone.checkedAt === '' ? [] : [{ key: 'checked', node: <CheckedMark clone={clone} /> }]),
    ...notes.map((note) => ({ key: note.kind, node: <NoteMark note={note} /> })),
  ]

  if (parts.length === 0) {
    return null
  }

  return (
    <span className="col-start-6 row-start-1 flex min-w-0 items-center gap-2.5 overflow-hidden text-xs whitespace-nowrap @max-4xl:col-[6/9] @max-4xl:row-start-2 @max-4xl:justify-end @max-xl:shrink-0 @max-xl:justify-start">
      {parts.map(({ key, node }, i) => (
        <Fragment key={key}>
          {i > 0 && <span className="sr-only">, </span>}
          {node}
        </Fragment>
      ))}
    </span>
  )
}

// AgeCell is how long ago a clone's stalest counted module changed, a mark up to date
// notwithstanding, which its note says; its tooltip says when in full, that change's
// subject, when it was marked, and what of the clone was read, as of the clock (ageTitle).
function AgeCell({ clone }: { clone: ManifestsClone }) {
  const title = useNowAs((now) => ageTitle(now, clone))

  return (
    <span
      title={title}
      className="col-start-7 row-start-1 cursor-help text-right text-xs whitespace-nowrap text-muted tabular-nums @max-xl:col-start-3"
    >
      <RelativeTime iso={clone.oldest} show={formatAgo} />
    </span>
  )
}

export interface DepsRowProps {
  clone: ManifestsClone
  // open reports the row's drawer unfolded, and onToggle unfolds or folds it: the page
  // keeps which are open, several at once, across reads.
  open: boolean
  onToggle: () => void
  // sessions are the repository's upgrade sessions, newest first (upgradeSessions), and
  // agents the coding agents, which name them; none by default.
  sessions?: readonly AgentSession[] | undefined
  agents?: readonly AgentInfo[]
  // onUpgrade opens the clone's upgrade sheet.
  onUpgrade: () => void
  // onMark marks the clone's repository up to date (checked) or unmarks it, and marking
  // reports a mark under way, which holds every row's back; without onMark the row offers
  // none.
  onMark?: ((checked: boolean) => void) | undefined
  marking?: boolean
}

// noAgents names no agent, until the agents are listed: each goes by its ID.
const noAgents: readonly AgentInfo[] = []

// DepsRow is one clone on one line of its group's grid: an edge colored by how long ago
// its dependencies last changed (staleTone), its repository and pin, how many modules of
// each kind it holds, the tools it is set up for, its stalest manifest (or why it couldn't
// be read), what to know about it (an upgrade an agent works on there, when the user marked
// it up to date, then how it was read), that manifest's age, its mark up to date (a quiet
// icon, green while it is marked), Upgrade…, quiet until the row is hovered, and the
// chevron that unfolds its drawer. A click on the row's empty space unfolds it too, a
// pointer's shortcut for the chevron, which the keyboard reaches: → unfolds, ← folds, and ↑
// and ↓ move to the rows around it, across groups; a drag that selects the row's words
// leaves it as it was, so they can still be copied. The edge, the mark's note, and the
// upgrade read the clock, and the row renders again only when what they say changes.
export function DepsRow({
  clone,
  open,
  onToggle,
  sessions,
  agents = noAgents,
  onUpgrade,
  onMark,
  marking = false,
}: DepsRowProps) {
  const drawerId = useId()
  const tone = useNowAs((now) => staleTone(now, clone))
  // shown is the upgrade the row follows: the newest still running, else the newest that
  // ended in the last day, as a card's session tag picks it.
  const shownId = useNowAs((now) => cardSession(sessions, now)?.id ?? null)
  const session = sessions?.find((s) => s.id === shownId)

  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    const unfold = e.key === 'ArrowRight' && !open
    const fold = e.key === 'ArrowLeft' && open

    if (unfold || fold) {
      e.preventDefault()
      onToggle()

      return
    }

    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      const next = neighbor(e.currentTarget, e.key === 'ArrowDown' ? 1 : -1)

      if (next !== undefined) {
        e.preventDefault()
        next.focus()
      }
    }
  }

  return (
    <>
      {/* eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions -- the row's click is a pointer's shortcut for its chevron, which the keyboard and a screen reader reach */}
      <li
        data-open={open || undefined}
        onClick={(e) => {
          if (!selectingText() && !onAControl(e)) {
            onToggle()
          }
        }}
        className={rowClass}
      >
        <span
          aria-hidden="true"
          data-tone={tone}
          className={`col-start-1 row-start-1 my-0.5 self-stretch rounded-full @max-4xl:row-end-3 ${toneEdges[tone]}`}
        />
        <RepoCell clone={clone} />
        <div className={metaClass}>
          <span
            title={modulesTitle(clone.modules)}
            className="col-start-3 row-start-1 text-xs whitespace-nowrap tabular-nums"
          >
            {modulesText(clone.modules)}
          </span>
          <span className="col-start-4 row-start-1 text-xs whitespace-nowrap text-muted">{toolsText(clone.tools)}</span>
          <PathCell clone={clone} />
          <NotesCell clone={clone} session={session} agents={agents} notes={cloneNotes(clone)} />
        </div>
        {clone.oldest !== '' && <AgeCell clone={clone} />}
        <span
          data-row-controls
          className="col-start-8 row-start-1 flex items-center gap-1 justify-self-end @max-xl:col-start-4"
        >
          {onMark !== undefined && canMark(clone) && (
            <button
              type="button"
              data-checked={clone.checkedAt !== '' || undefined}
              aria-label={markLabel(clone)}
              title={markTitle(clone)}
              disabled={marking}
              onClick={() => {
                onMark(clone.checkedAt === '')
              }}
              className={markButtonClass}
            >
              <CircleCheckIcon className="size-3.5" />
            </button>
          )}
          <button
            type="button"
            aria-label={`Upgrade ${clone.repo}’s dependencies…`}
            onClick={onUpgrade}
            className={rowButtonClass}
          >
            Upgrade…
          </button>
        </span>
        <span data-row-controls className="col-start-9 row-start-1 flex @max-xl:col-start-5">
          <button
            type="button"
            data-deps-toggle
            aria-expanded={open}
            aria-controls={open ? drawerId : undefined}
            aria-label={`${open ? 'Fold' : 'Unfold'} ${clone.repo}`}
            onClick={onToggle}
            onKeyDown={onKeyDown}
            className={toolButtonClass}
          >
            <ChevronRightIcon className={`size-3.5 motion-safe:transition-transform ${open ? 'rotate-90' : ''}`} />
          </button>
        </span>
      </li>
      {open && <DepsDrawer id={drawerId} clone={clone} onUpgrade={onUpgrade} onMark={onMark} marking={marking} />}
    </>
  )
}

// DepsLegend explains the colors of a group's edges, for its heading's note: a dot of each
// color before the ages it stands for (legendOf), at the heading's end. The entries sit on
// one line, and one that doesn't fit wraps onto a hidden second line, so it drops whole
// rather than being cut. A colon and a comma, for a screen reader, keep the heading's name
// readable: "Stale · 41: over 3 months, age unknown". The space after each is plain text
// beside it, not inside it: a name drops the spaces an element's own text starts or ends
// with, and the layout draws no space between its boxes.
export function DepsLegend({ group, rows }: { group: DepsGroup; rows: readonly ManifestsClone[] }) {
  return (
    <>
      <span className="sr-only">:</span>{' '}
      <span className="flex h-4 min-w-0 flex-wrap items-center justify-end gap-x-3 gap-y-1 overflow-hidden">
        {legendOf(group, rows).map(({ tone, words }, i) => (
          <Fragment key={tone}>
            {i > 0 && (
              <>
                <span className="sr-only">,</span>{' '}
              </>
            )}
            <span className="inline-flex shrink-0 items-center gap-1">
              <span aria-hidden="true" className={`size-1.5 rounded-full ${toneEdges[tone]}`} />
              {words}
            </span>
          </Fragment>
        ))}
      </span>
    </>
  )
}
