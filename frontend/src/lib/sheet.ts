// What the sheets over the inbox share: the props both take, where the card they show sits
// among the cards they step through, the openings their hosts hear, their keys, and the
// steps of an action under way.

import { useEffect, useEffectEvent, useRef, useState, type ReactNode } from 'react'

import {
  on,
  type AgentSession,
  type ConversationOrder,
  type InboxItem,
  type ItemResolution,
  type Worktree,
} from '../platform'
import { refText } from './format'
import { isTyping } from './keys'
import type { LaunchOptions } from './launch'
import { isTopmost } from './useModalDialog'

// Place is where a card sits among the cards a sheet steps through: the cards before and
// after it, if any, and its position, as in "3 of 8 in Assigned", or '' when the list
// doesn't hold it.
export interface Place {
  prev: InboxItem | undefined
  next: InboxItem | undefined
  position: string
}

// placeIn finds the card with id among siblings, the cards of the list titled listTitle in
// the order it shows them.
export function placeIn(siblings: readonly InboxItem[], id: number, listTitle: string): Place {
  const at = siblings.findIndex((s) => s.id === id)

  return {
    prev: at > 0 ? siblings[at - 1] : undefined,
    next: at >= 0 ? siblings[at + 1] : undefined,
    position: at < 0 ? '' : `${String(at + 1)} of ${String(siblings.length)} in ${listTitle}`,
  }
}

// movesOnTo is the card a sheet moves on to as an action takes the card with id out of the
// cards it steps through, along with every card leaves says goes too: the nearest after it
// that stays, else the nearest before it, or undefined when none stays, which closes the
// sheet. Work it out before the action runs, which changes the cards.
export function movesOnTo(
  siblings: readonly InboxItem[],
  id: number,
  leaves: (card: InboxItem) => boolean,
): InboxItem | undefined {
  const at = siblings.findIndex((s) => s.id === id)

  if (at < 0) {
    return undefined
  }

  const stays = (card: InboxItem) => card.id !== id && !leaves(card)

  return siblings.slice(at + 1).find(stays) ?? siblings.slice(0, at).findLast(stays)
}

// StepWay is the way a sheet stepped to the card it shows: to the card after the one it
// showed, or before it.
export type StepWay = 'next' | 'prev'

// Came is how a sheet came to the card it shows by a step through its cards: the way it
// went, and the cards it stepped through then, by ID, in the order the list showed them.
// A card found merged or closed as it shows is stepped past the same way (stepPast).
export interface Came {
  way: StepWay
  cards: readonly number[]
}

// cameBy is the step from the card with from to the card with to, among siblings.
export function cameBy(siblings: readonly InboxItem[], from: number, to: number): Came {
  const cards = siblings.map((s) => s.id)

  return { way: cards.indexOf(to) < cards.indexOf(from) ? 'prev' : 'next', cards }
}

// Past is a step past a card a sheet found merged or closed: the card it steps to, and how
// it came to that one, the same way, through the same cards but the one stepped past.
export interface Past {
  to: number
  came: Came
}

// stepPast is the step past the card with id, which a sheet came to by came and found
// merged or closed: to the nearest card the way it came that the list still holds (held),
// else the nearest the other way; undefined when the list holds no other, or the sheet
// opened on the card (came is null), either of which closes the sheet. The read that found
// it took it out of the list already, so came's cards, not the list's, say where it was.
// The step leaves it out of the cards it came through, so a list that still holds it, not
// yet read again, never has the sheet step back to it, nor between two such cards for good.
export function stepPast(came: Came | null, id: number, held: (id: number) => boolean): Past | undefined {
  const at = came === null ? -1 : came.cards.indexOf(id)

  if (came === null || at < 0) {
    return undefined
  }

  const stays = (card: number) => card !== id && held(card)
  const after = came.cards.slice(at + 1).find(stays)
  const before = came.cards.slice(0, at).findLast(stays)
  const to = came.way === 'next' ? (after ?? before) : (before ?? after)

  return to === undefined ? undefined : { to, came: { way: came.way, cards: came.cards.filter((c) => c !== id) } }
}

// EndedState is what became of an item a sheet found no longer open as it read it, since
// the inbox last did: merged, or closed.
export type EndedState = 'merged' | 'closed'

// EndedItem is an item a sheet found merged or closed on GitHub, as its read names it. That
// read took it out of the inbox's lists already, as any read of a single item does.
export interface EndedItem {
  id: number
  repo: string
  number: number
  title: string
  url: string
  state: EndedState
}

// endedState is what became of an item GitHub says is in state (OPEN, MERGED, or CLOSED),
// or '' while it is open.
export function endedState(state: string): EndedState | '' {
  switch (state) {
    case 'MERGED':
      return 'merged'
    case 'CLOSED':
      return 'closed'
    default:
      return ''
  }
}

// endedText says what became of an ended item, as "acme-org/api#12 was merged".
export function endedText(ended: EndedItem): string {
  return `${refText(ended)} was ${ended.state}`
}

// Shown is what a sheet shows of its item, as its latest read found it: enough to name it
// and say whether it is still open.
interface Shown {
  id: number
  repo: string
  number: number
  title: string
  url: string
  state: string
}

// useEnded hands onEnded the item a sheet shows once a read of it finds it merged or
// closed on GitHub: for a card the inbox lists, which holds open items alone, since its
// last refresh. It waits while hold is set: an action under way, whose outcome the sheet
// reports itself, or what one left for the user to read (its notice). It tells once per
// item and state, so a host that leaves the sheet where it is is never told again.
export function useEnded(shown: Shown | null, hold: boolean, onEnded: ((ended: EndedItem) => void) | undefined): void {
  const state = shown === null || hold ? '' : endedState(shown.state)
  const id = shown?.id ?? 0
  const tell = useEffectEvent((as: EndedState) => {
    if (shown !== null) {
      onEnded?.({ id: shown.id, repo: shown.repo, number: shown.number, title: shown.title, url: shown.url, state: as })
    }
  })

  useEffect(() => {
    if (state !== '') {
      tell(state)
    }
  }, [state, id])
}

// SheetStep is a step to the card before or after the one a sheet shows: the title of the
// card it steps to, and the step.
export interface SheetStep {
  title: string
  onStep: () => void
}

// stepTo is the step to the card to, which onNavigate takes, or null when there is none.
export function stepTo(to: InboxItem | undefined, onNavigate: (id: number) => void): SheetStep | null {
  if (to === undefined) {
    return null
  }

  return {
    title: to.title,
    onStep: () => {
      onNavigate(to.id)
    },
  }
}

// SheetBack is the way back to the sheet a link was followed from: its words, as in
// "acme-org/api#12", the title of what it shows, and the step back.
export interface SheetBack {
  label: string
  title: string
  onBack: () => void
}

// SheetCommonProps are what both sheets over the inbox take alike (ReviewSheetProps and
// IssueSheetProps extend them), which a host builds once and hands to either.
export interface SheetCommonProps {
  // id is the item the sheet shows, and item its inbox card.
  id: number
  item: InboxItem | undefined
  // siblings are the cards of the list and tab it was opened from, in the order the list
  // shows them, for stepping through them; listTitle names that list.
  siblings: readonly InboxItem[]
  listTitle: string
  // order is the order the conversation is listed in, and onOrder hears the user choose
  // another, which the window remembers.
  order?: ConversationOrder
  onOrder?: ((order: ConversationOrder) => void) | undefined
  launch: LaunchOptions | null
  resolution: ItemResolution | null
  sessions: readonly AgentSession[] | undefined
  // worktree is the item's worktree, which the sheet's work line shows, if it has one.
  worktree?: Worktree | undefined
  onNavigate: (id: number) => void
  onClose: () => void
  // onEnded hears a read find the item merged or closed on GitHub since the inbox last
  // read it (useEnded), for the host to move the sheet on from it; without it, the sheet
  // shows it as read.
  onEnded?: ((ended: EndedItem) => void) | undefined
  // onManageReplies opens the replies in the Prompts tab.
  onManageReplies?: (() => void) | undefined
  // initialFocus selects the control focus starts on as the sheet opens (see Sheet), and
  // refocus moves it there again whenever it changes.
  initialFocus?: string | undefined
  refocus?: number | undefined
  // back returns to the sheet a link was followed from, or is null.
  back?: SheetBack | null
  // triage is the card's triage, in the header, and toast the host's toast, which shows in
  // the sheet while it is open, since the page behind it can't be reached.
  triage?: ReactNode
  toast?: ReactNode
}

// useSheetVisits hears a host's sheet move from the item it showed to the one it shows,
// viewingId, either 0 for none: seen marks both read, the one left catching what the sheet
// read while open and what the user did in it, and onOpened hears the one opened. Each
// opening is heard once, however it was opened: the page shown again (Activity), or an
// effect run twice, opens nothing anew, and never undoes a Mark unread made since.
export function useSheetVisits(
  viewingId: number,
  seen: (ids: readonly number[]) => void,
  onOpened: ((itemId: number) => void) | undefined,
): void {
  // opened is the item last opened in the sheet, or 0 while none is.
  const opened = useRef(0)
  const moved = useEffectEvent((left: number, now: number) => {
    seen([left, now].filter((id) => id > 0))

    if (now > 0) {
      onOpened?.(now)
    }
  })

  useEffect(() => {
    const left = opened.current

    if (viewingId === left) {
      return
    }

    opened.current = viewingId
    moved(left, viewingId)
  }, [viewingId])
}

// SheetKeys is what a sheet's keys do.
export interface SheetKeys {
  // working holds every key back while an action runs, so its outcome is never lost.
  working: boolean
  // run runs the sheet's button, as ⌘↩ asks; it checks for itself whether the button's
  // action is held back.
  run: () => void
  // prev and next step to the card before and after, or are null at the ends.
  prev: SheetStep | null
  next: SheetStep | null
}

// inPopover reports a key pressed in an open menu or popover, which takes its own arrows:
// a submenu's open and close with them, and the choices it was built with are another
// card's once the sheet steps. A popover's panel names itself a dialog; the sheet, a native
// <dialog>, carries no role of its own to match. A picker that opens its own modal <dialog>
// over the sheet (the issue sheet's) leaves the sheet no longer topmost instead.
function inPopover(e: KeyboardEvent): boolean {
  return e.target instanceof Element && e.target.closest('[role="menu"], [role="dialog"]') !== null
}

// onRadio reports a key pressed on a radio button, whose arrows move between its choices.
function onRadio(e: KeyboardEvent): boolean {
  return e.target instanceof HTMLInputElement && e.target.type === 'radio'
}

// useSheetKeys gives the sheet whose title has titleId its keys while it is the dialog the
// user is in and no menu or picker in it holds focus: ⌘↩ runs its button, and ← and → step
// through its list while the user isn't typing, with no modifier held, unless a control in
// the sheet took the key first.
export function useSheetKeys(titleId: string, keys: SheetKeys): void {
  const onKey = useEffectEvent((e: KeyboardEvent) => {
    const dialog = document.querySelector(`dialog[aria-labelledby="${titleId}"]`)

    // A key pressed in a menu or a picker is its own, ⌘↩ included, which applies a picker.
    if (!(dialog instanceof HTMLDialogElement) || !isTopmost(dialog) || keys.working || inPopover(e)) {
      return
    }

    if (e.key === 'Enter' && e.metaKey) {
      e.preventDefault()
      keys.run()

      return
    }

    // A key a control in the sheet already took, as the tabs take an arrow to move between
    // them, or one a radio button takes to move between its choices, steps nothing.
    if (isTyping(e) || e.metaKey || e.altKey || e.ctrlKey || e.defaultPrevented || onRadio(e)) {
      return
    }

    const step = e.key === 'ArrowLeft' ? keys.prev : e.key === 'ArrowRight' ? keys.next : null

    if (step !== null) {
      e.preventDefault()
      step.onStep()
    }
  })

  useEffect(() => {
    const listener = (e: KeyboardEvent) => {
      onKey(e)
    }

    document.addEventListener('keydown', listener)

    return () => {
      document.removeEventListener('keydown', listener)
    }
  }, [])
}

// Progress is an action under way: what it is, the item it acts on, the steps the backend
// reported so far, and whether the sheet stays open once it is done.
export interface Progress {
  title: string
  target: string
  steps: readonly string[]
  staysOpen?: boolean | undefined
}

// withStep adds a step the backend reported to progress, once: a step repeated, as the
// wait for GitHub to work out a merge can be, shows as one.
export function withStep(progress: Progress | null, step: string): Progress | null {
  if (progress === null || progress.steps.at(-1) === step) {
    return progress
  }

  return { ...progress, steps: [...progress.steps, step] }
}

// ProgressEvent names an event that reports the steps of a sheet's action.
export type ProgressEvent = 'review:progress' | 'issue:progress'

// useActionProgress follows an action on the item with id as the backend reports its
// steps on event, from the moment the sheet sets it under way until the sheet sets it to
// null. A step about another item, from before the sheet stepped to this one, is never
// shown.
export function useActionProgress(
  event: ProgressEvent,
  id: number,
): [Progress | null, (progress: Progress | null) => void] {
  const [progress, setProgress] = useState<Progress | null>(null)

  const heard = useEffectEvent((itemId: number, step: string) => {
    if (itemId === id) {
      setProgress((p) => withStep(p, step))
    }
  })

  useEffect(
    () =>
      on(event, (data) => {
        heard(data.itemId, data.step)
      }),
    [event],
  )

  return [progress, setProgress]
}
