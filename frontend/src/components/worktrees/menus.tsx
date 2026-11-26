import { logFailure } from '../../lib/call'
import { counted } from '../../lib/format'
import { showable, tabWords } from '../../lib/sessions'
import { asReadNow, type DetailsRead } from '../../lib/useWorktreeDetails'
import { itemRef, uncommitted } from '../../lib/worktreeParts'
import { removeWhy } from '../../lib/worktrees'
import {
  copyText,
  openExternal,
  type AgentID,
  type AgentSession,
  type Worktree,
  type WorktreeRepo,
} from '../../platform'
import {
  CheckIcon,
  CopyIcon,
  ExternalIcon,
  FolderIcon,
  ScissorsIcon,
  TerminalIcon,
  TrashIcon,
  UnlockIcon,
  WorktreeIcon,
} from '../icons'
import type { MenuItem } from '../Menu'

// The Worktrees page's menus: a row's ⋯, with everything its row and drawer offer and
// Remove… and Force remove… last, and the items of the page's cleanup ⌄. An item that
// can't run now says why under it; the backend decides what may be removed, and these only
// word it.

// icon is a menu item's mark, muted beside its words.
const icon = 'size-3.5 shrink-0 text-muted'

// copy puts text on the clipboard from a menu, which closes as it's chosen, so a copy the
// clipboard refuses is only logged, naming what.
function copy(text: string, what: string) {
  logFailure(copyText(text), `could not copy the ${what}`)
}

// why is a disabled item's reason, as its note under it and its tooltip.
function why(reason: string): Pick<MenuItem, 'note' | 'title'> {
  return reason === '' ? {} : { note: reason, title: reason }
}

// goneWhy says why Open in terminal and Reveal in Finder can't run on a worktree: its
// folder is gone, or git couldn't read it; '' when they can.
export function goneWhy(wt: Worktree): string {
  if (wt.prunable) {
    return 'Its folder is gone'
  }

  return wt.error === '' ? '' : 'git couldn’t read it'
}

// WorktreeRowActions are what a worktree's row, its drawer, and its ⋯ do: open it in the
// terminal or reveal it in Finder, bring forward the terminal tab of the session at work in
// it, and hand the page a change to make to it, or a prune of its clone.
export interface WorktreeRowActions {
  onTerminal: () => void
  onReveal: () => void
  onShowTerminal: () => void
  onCleanUp: (repo: WorktreeRepo, worktree: Worktree) => void
  onUnlock: (repo: WorktreeRepo, worktree: Worktree) => void
  onRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  onForceRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  onPrune: (repo: WorktreeRepo) => void
}

export interface RowMenuOptions extends WorktreeRowActions {
  repo: WorktreeRepo
  worktree: Worktree
  // read is what the window has read of the worktree's details for this listing, or
  // undefined before anything asked: the removals are counted by it, as its drawer's are.
  read: DetailsRead | undefined
  // session is the session the app follows in the worktree (liveSessions), if any, whose
  // terminal tab Show terminal brings forward while it can be shown (showable).
  session: AgentSession | undefined
  // disabled holds every change while one, or a listing, runs.
  disabled: boolean
  agentName: (id: AgentID) => string
}

// rowMenuItems are a row's ⋯: Show terminal while a session the app follows runs there in
// a tab that can be shown, Open in terminal, Reveal in Finder, Copy path, Copy branch, Open
// the item on GitHub, Unlock for a lock an ended session left, Prune for a folder that's
// gone, then Clean up… for a finished worktree, Remove…, and Force remove… for one with
// uncommitted files, in red. The removals count its files as its details last did
// (asReadNow), as its drawer does, so a read can hold one back but never allow one the
// backend refused.
export function rowMenuItems(o: RowMenuOptions): MenuItem[] {
  const { repo, worktree: wt, session, disabled } = o
  const gone = goneWhy(wt)
  const items: MenuItem[] = [
    ...(showable(session)
      ? [
          {
            label: 'Show terminal',
            icon: <TerminalIcon className={icon} />,
            title: `Bring forward ${tabWords(session)} ${o.agentName(session.agent)} runs in`,
            onSelect: o.onShowTerminal,
          },
        ]
      : []),
    {
      label: 'Open in terminal',
      icon: <TerminalIcon className={icon} />,
      disabled: gone !== '',
      ...why(gone),
      onSelect: o.onTerminal,
    },
    {
      label: 'Reveal in Finder',
      icon: <FolderIcon className={icon} />,
      disabled: gone !== '',
      ...why(gone),
      onSelect: o.onReveal,
    },
    {
      label: 'Copy path',
      icon: <CopyIcon className={icon} />,
      onSelect: () => {
        copy(wt.path, 'worktree path')
      },
    },
  ]

  if (!wt.detached && wt.branch !== '') {
    items.push({
      label: 'Copy branch',
      icon: <WorktreeIcon className={icon} />,
      onSelect: () => {
        copy(wt.branch, 'branch name')
      },
    })
  }

  const { item } = wt

  if (item !== null) {
    items.push({
      label: `Open ${itemRef(item)} on GitHub`,
      icon: <ExternalIcon className={icon} />,
      onSelect: () => {
        openExternal(item.url)
      },
    })
  }

  if (wt.canUnlock) {
    items.push({
      label: 'Unlock',
      icon: <UnlockIcon className={icon} />,
      disabled,
      note: 'Releases the lock of the session that ended here',
      onSelect: () => {
        o.onUnlock(repo, wt)
      },
    })
  }

  if (wt.prunable) {
    items.push({
      label: `Prune ${repo.name}`,
      icon: <ScissorsIcon className={icon} />,
      disabled,
      title:
        'Clears git’s entries for worktrees whose folders are gone; touches no worktree that exists, and no branch',
      onSelect: () => {
        o.onPrune(repo)
      },
    })

    return items
  }

  const fresh = asReadNow(wt, o.read)
  const removeReason = removeWhy(fresh, false, o.agentName)

  items.push(
    ...(wt.canCleanUp
      ? [
          {
            label: 'Clean up…',
            icon: <CheckIcon className="size-3.5 shrink-0 text-success" />,
            disabled,
            divided: true,
            onSelect: () => {
              o.onCleanUp(repo, wt)
            },
          },
        ]
      : []),
    {
      label: 'Remove…',
      icon: <TrashIcon className={icon} />,
      disabled: disabled || removeReason !== '',
      divided: !wt.canCleanUp,
      ...why(removeReason),
      onSelect: () => {
        o.onRemove(repo, wt)
      },
    },
  )

  if (uncommitted(fresh) > 0) {
    const forceReason = removeWhy(fresh, true, o.agentName)

    items.push({
      label: 'Force remove…',
      icon: <TrashIcon className="size-3.5 shrink-0" />,
      danger: true,
      disabled: disabled || forceReason !== '',
      ...why(forceReason),
      onSelect: () => {
        o.onForceRemove(repo, wt)
      },
    })
  }

  return items
}

export interface CleanupMenuOptions {
  // finishedCount and cleanCount are how many worktrees Clean up finished and Remove clean
  // would take (the listing's counts).
  finishedCount: number
  cleanCount: number
  // disabled holds every change while one, or a listing, runs.
  disabled: boolean
  onRemoveAllClean: () => void
  onCleanUpMerged: () => void
}

// cleanupMenuItems are the page's cleanup ⌄, beside Clean up N finished, which runs that
// cleanup itself: Clean up finished… only while none is finished, to say why, Remove the
// clean worktrees…, and Clean up merged branches…, each saying what it takes.
export function cleanupMenuItems(o: CleanupMenuOptions): MenuItem[] {
  const items: MenuItem[] = []

  if (o.finishedCount === 0) {
    items.push({
      label: 'Clean up finished…',
      icon: <CheckIcon className={icon} />,
      disabled: true,
      note: 'None is finished: no worktree whose pull request merged or issue closed has all its work on GitHub',
    })
  }

  items.push(
    o.cleanCount > 0
      ? {
          label: `Remove ${counted(o.cleanCount, 'clean worktree', 'clean worktrees')}…`,
          icon: <TrashIcon className={icon} />,
          disabled: o.disabled,
          note: 'Each is unlocked and holds nothing uncommitted to lose; you choose which, and their branches',
          onSelect: o.onRemoveAllClean,
        }
      : {
          label: 'Remove clean worktrees…',
          icon: <TrashIcon className={icon} />,
          disabled: true,
          note: 'None is clean and unlocked',
        },
    {
      label: 'Clean up merged branches…',
      icon: <WorktreeIcon className={icon} />,
      disabled: o.disabled,
      note: 'The local branches whose pull requests merged, in every clone',
      onSelect: o.onCleanUpMerged,
    },
  )

  return items
}
