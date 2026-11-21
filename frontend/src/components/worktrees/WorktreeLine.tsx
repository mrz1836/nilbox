import { useEffect, useEffectEvent, useId, useRef, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'

import { useMutation } from '../../lib/useMutation'
import { readable, type WorktreeDetailsState } from '../../lib/useWorktreeDetails'
import type { RowWork } from '../../lib/useWorktrees'
import type { ItemFacts } from '../../lib/worktreeParts'
import { verbOf } from '../../lib/worktrees'
import {
  openWorktreeTerminal,
  revealWorktree,
  type AgentID,
  type AgentSession,
  type LinkedItem,
  type Worktree,
  type WorktreeRepo,
} from '../../platform'
import { ChevronRightIcon, MoreIcon, TerminalIcon } from '../icons'
import type { LaunchPlace } from '../LaunchControls'
import { MenuButton } from '../Menu'
import { Drawer } from './Drawer'
import {
  chevronCellClass,
  cleanUpButtonClass,
  dimClass,
  iconButtonClass,
  launchPlace,
  metaClass,
  noteClass,
  rowClass,
  toolsCellClass,
  verbButtonClass,
  verbCellClass,
} from './layout'
import { rowMenuItems, type WorktreeRowActions } from './menus'
import {
  AgeCell,
  AgentCell,
  BranchCell,
  detachedOf,
  Edge,
  ItemCell,
  nameOf,
  StateCell,
  WorkCell,
  WorkMark,
} from './RowCells'

// selectingText reports a click that ends a drag across text, which selects the words
// rather than acting on the row they're in, as a Release row reads it.
function selectingText(): boolean {
  return (window.getSelection()?.toString() ?? '') !== ''
}

// controls are what a row's click leaves to themselves: its buttons and links, a menu, a
// dialog, and the cells of its verb, its tools, and its chevron.
const controls = 'button, a, input, label, select, textarea, dialog, [role="menu"], [data-row-controls]'

// onAControl reports a click on one of a row's controls, or on something rendered out of
// the row's place (a launch's dialog), whose clicks still bubble through React to the row.
function onAControl(e: MouseEvent<HTMLElement>): boolean {
  const target = e.target as Element

  return !e.currentTarget.contains(target) || target.closest(controls) !== null
}

// menuOpen reports a menu open anywhere in the window, which a press outside it closes: the
// click that press ends only closes the menu, as on the Mac, and never folds or unfolds the
// row it lands on.
function menuOpen(): boolean {
  return document.querySelector('[role="menu"]') !== null
}

// neighbor is the chevron of the row step rows after the one whose chevron from is, across
// every clone on the page, or undefined past either end.
function neighbor(from: HTMLElement, step: number): HTMLElement | undefined {
  const toggles = Array.from(document.querySelectorAll<HTMLElement>('[data-worktree-toggle]'))

  return toggles[toggles.indexOf(from) + step]
}

// WorktreeLineProps are a row's, which opens Terminal and Finder on its worktree itself.
export interface WorktreeLineProps extends Omit<WorktreeRowActions, 'onTerminal' | 'onReveal'> {
  // repo is the clone the worktree belongs to.
  repo: WorktreeRepo
  worktree: Worktree
  // open reports the row's drawer unfolded, and onToggle unfolds or folds it: the page
  // keeps which are open, several at once, across listings.
  open: boolean
  onToggle: () => void
  // details are the worktrees' details read on demand (useWorktreeDetails), and listing
  // numbers the listing the row is from (useWorktrees' listing): an open drawer reads its
  // worktree's details as it opens, and again after each new listing.
  details: WorktreeDetailsState
  listing: number
  // disabled holds every change while one, or a listing, runs.
  disabled: boolean
  // error is the failure of a change made to the worktree, or '' (the default).
  error?: string
  // work is what the worktree shows of a change taking it, or null (the default) when
  // none does.
  work?: RowWork | null
  // facts are what the inbox read of the worktree's item while it lists it (factsOf), and
  // session the session the app follows in it (liveSessions); none by default.
  facts?: ItemFacts | undefined
  session?: AgentSession | undefined
  // agentName names a coding agent for people, as in "Codex"; by default its ID.
  agentName?: ((id: AgentID) => string) | undefined
  // launchFor is the launch control for an item the inbox lists, placed in the row's verb
  // column (LaunchControls' row variant): the row's verb when that's what fits.
  launchFor?: ((item: LinkedItem, place: LaunchPlace) => ReactNode) | undefined
}

// WorktreeLine is one worktree on one line of the page's grid: its lane's edge, its state,
// branch, item, work, agent, and age, then the one verb that fits it (Clean up, Unlock, a
// launch on its item, or none), ›_ to open it in Terminal (its place kept empty where it
// can't), ⋯ with everything else, and the chevron that unfolds its drawer. A click on the
// row's empty space unfolds it too, a pointer's shortcut for the chevron, which the keyboard
// reaches: → unfolds, ← folds, and ↑ and ↓ move to the rows around it; the click that
// closes an open menu doesn't. Under the row, a line says why a change left it in place or
// failed, or, until the next listing, why Terminal or Finder couldn't open; under that,
// while open, is its drawer, which reads the worktree's
// details as it opens and again after each listing. While a change takes the worktree, the
// row is dimmed and says what is happening in place of its verb and tools.
export function WorktreeLine({
  repo,
  worktree,
  open,
  onToggle,
  details,
  listing,
  disabled,
  error = '',
  work = null,
  facts,
  session,
  agentName = (id) => id,
  launchFor,
  onCleanUp,
  onUnlock,
  onRemove,
  onForceRemove,
  onPrune,
}: WorktreeLineProps) {
  const drawerId = useId()
  const opener = useMutation((where: 'terminal' | 'finder') =>
    where === 'terminal' ? openWorktreeTerminal(repo.path, worktree.path) : revealWorktree(repo.path, worktree.path),
  )
  const verb = verbOf(worktree, facts !== undefined)
  const name = nameOf(worktree)
  const rowName = detachedOf(worktree) ? `detached ${name}` : name
  const reason = work?.reason ?? ''

  // An open drawer reads the worktree's details as it opens and after each new listing;
  // the reads are kept per listing, so asking again in one costs nothing.
  const readDetails = useEffectEvent(() => {
    if (readable(worktree)) {
      details.read(repo.path, worktree)
    }
  })

  useEffect(() => {
    if (open) {
      readDetails()
    }
  }, [open, listing])

  // Why Terminal or Finder couldn't open is about the worktree as one listing found it, so
  // the next listing puts it away.
  const clearOpenError = useEffectEvent(() => {
    if (opener.error !== '') {
      opener.reset()
    }
  })

  useEffect(() => {
    clearOpenError()
  }, [listing])

  // pressedOverMenu records that the row was pressed while a menu was open, which the press
  // closes; the click it ends then leaves the row as it was.
  const pressedOverMenu = useRef(false)

  const onTerminal = () => {
    void opener.run('terminal')
  }

  const onReveal = () => {
    void opener.run('finder')
  }

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

  const { item } = worktree
  let verbCell: ReactNode = null

  if (verb?.act === 'launch' && item !== null) {
    verbCell = launchFor?.(item, launchPlace)
  } else if (verb !== null && verb.act !== 'launch') {
    verbCell = (
      <span data-row-controls className={verbCellClass}>
        <button
          type="button"
          disabled={disabled}
          title={verb.title}
          onClick={() => {
            if (verb.act === 'cleanUp') {
              onCleanUp(repo, worktree)
            } else {
              onUnlock(repo, worktree)
            }
          }}
          className={verb.act === 'cleanUp' ? cleanUpButtonClass : verbButtonClass}
        >
          {verb.label}
        </button>
      </span>
    )
  }

  return (
    <>
      {/* eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions -- the row's click is a pointer's shortcut for its chevron, which the keyboard and a screen reader reach */}
      <li
        data-open={open || undefined}
        aria-busy={work?.state === 'doing' ? true : undefined}
        onMouseDown={() => {
          pressedOverMenu.current = menuOpen()
        }}
        onClick={(e) => {
          const closingMenu = pressedOverMenu.current

          pressedOverMenu.current = false

          if (!closingMenu && !selectingText() && !onAControl(e)) {
            onToggle()
          }
        }}
        className={work === null || work.state === 'left' ? rowClass : `${rowClass} ${dimClass}`}
      >
        <Edge worktree={worktree} />
        <StateCell worktree={worktree} />
        <BranchCell worktree={worktree} />
        <div className={metaClass}>
          <ItemCell worktree={worktree} facts={facts} />
          <WorkCell worktree={worktree} />
          <AgentCell worktree={worktree} session={session} agentName={agentName} />
          <AgeCell worktree={worktree} />
        </div>
        {work === null ? (
          <>
            {verbCell}
            <span data-row-controls className={toolsCellClass}>
              {readable(worktree) ? (
                <button
                  type="button"
                  title="Open in Terminal"
                  aria-label={`Open ${name} in Terminal`}
                  disabled={opener.pending}
                  onClick={onTerminal}
                  className={iconButtonClass}
                >
                  <TerminalIcon className="size-3.5" />
                </button>
              ) : (
                // ›_'s place, kept empty, so ⋯ stays in line with every other row's.
                <span aria-hidden="true" data-tool-gap className="size-6 shrink-0" />
              )}
              <MenuButton
                label={`More for ${name}`}
                title="More"
                items={() =>
                  rowMenuItems({
                    repo,
                    worktree,
                    read: details.of(worktree.path),
                    disabled,
                    agentName,
                    onTerminal,
                    onReveal,
                    onCleanUp,
                    onUnlock,
                    onRemove,
                    onForceRemove,
                    onPrune,
                  })
                }
                className={iconButtonClass}
                panelClassName="min-w-56 max-w-80"
              >
                <MoreIcon className="size-3.5" />
              </MenuButton>
            </span>
          </>
        ) : (
          <WorkMark work={work} />
        )}
        <span data-row-controls className={chevronCellClass}>
          <button
            type="button"
            data-worktree-toggle
            aria-expanded={open}
            aria-controls={open ? drawerId : undefined}
            aria-label={`${open ? 'Fold' : 'Unfold'} ${rowName}`}
            onClick={onToggle}
            onKeyDown={onKeyDown}
            className={iconButtonClass}
          >
            <ChevronRightIcon className={`size-3.5 motion-safe:transition-transform ${open ? 'rotate-90' : ''}`} />
          </button>
        </span>
      </li>
      {(reason !== '' || error !== '' || opener.error !== '') && (
        <li className={noteClass}>
          {reason !== '' && (
            <p role="alert" className="text-warning">
              {reason}
            </p>
          )}
          {error !== '' && (
            <p role="alert" className="text-danger">
              {error}
            </p>
          )}
          {opener.error !== '' && (
            <p role="alert" className="text-danger">
              {opener.error}
            </p>
          )}
        </li>
      )}
      {open && (
        <Drawer
          id={drawerId}
          repo={repo}
          worktree={worktree}
          read={details.of(worktree.path)}
          session={session}
          facts={facts}
          agentName={agentName}
          disabled={disabled}
          working={work !== null}
          opening={opener.pending}
          onTerminal={onTerminal}
          onReveal={onReveal}
          onCleanUp={onCleanUp}
          onUnlock={onUnlock}
          onRemove={onRemove}
          onForceRemove={onForceRemove}
          onPrune={onPrune}
        />
      )}
    </>
  )
}
