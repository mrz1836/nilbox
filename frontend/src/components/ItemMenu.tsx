import { orgOf, ownRule } from '../lib/repoRules'
import { leaving, workWords, type RowWork, type StartWork } from '../lib/rowWork'
import { isPinnedItself, type TriageTab } from '../lib/triage'
import { openExternal, type InboxItem } from '../platform'
import {
  ArchiveIcon,
  ClockIcon,
  ExternalIcon,
  EyeIcon,
  EyeOffIcon,
  InboxIcon,
  LinkIcon,
  MoreIcon,
  MuteIcon,
  PinIcon,
  RefreshIcon,
  ResetIcon,
  SlidersIcon,
  UnassignIcon,
} from './icons'
import { MenuButton, type MenuItem } from './Menu'
import { ghostClass, hoverClass, mutedTarget, snoozeItems, type TriageActions } from './TriageControls'

// EditActions are what a row's menu does beyond triage: copy the item's link, and the
// edits it makes on GitHub. The inbox shares one set across every row.
export interface EditActions {
  // onCopyLink puts the item's address on the clipboard.
  onCopyLink: (item: InboxItem) => void
  // onUnassign takes the user off the item's assignees on GitHub, and onRemoveReview off
  // a pull request's requested reviewers. Each settles once the inbox shows the outcome,
  // and reports its own failures, so it never rejects.
  onUnassign: (item: InboxItem) => Promise<void>
  onRemoveReview: (item: InboxItem) => Promise<void>
  // onReview opens a pull request in the review sheet, from the row's tab. Without it the
  // menu offers no review.
  onReview?: ((item: InboxItem, tab: TriageTab) => void) | undefined
}

export interface ItemMenuProps {
  item: InboxItem
  // tab is the list tab the row is in, which decides the inbox actions it offers.
  tab: TriageTab
  triage: TriageActions
  edits: EditActions
  // work is what the row is doing with GitHub, and start begins more (see rowWork).
  work: RowWork
  start: StartWork
}

// iconClass is a menu item's icon.
const iconClass = 'size-3.5 shrink-0 text-muted'

// githubSection captions the edits, which change GitHub where the rest change only the
// inbox on this Mac.
const githubSection = 'On GitHub'

// teamsText names the teams a pull request asks to review: "the @backend team", or "the
// @backend and @infra teams".
export function teamsText(teams: readonly string[]): string {
  const named = teams.map((team) => `@${team}`)

  if (named.length === 0) {
    return 'a team'
  }

  if (named.length === 1) {
    return `the ${named[0] ?? ''} team`
  }

  return `the ${named.slice(0, -1).join(', ')} and ${named.at(-1) ?? ''} teams`
}

// repoRuleItems pin or mute the item's repository, or every repository of its org, or take
// either rule off, each target by its own rule, then open the rules in Settings.
export function repoRuleItems(item: InboxItem, triage: TriageActions): MenuItem[] {
  const repo = item.repo
  const org = orgOf(repo)
  const toggles = (target: string, scope: string, divided: boolean): MenuItem[] => {
    const own = ownRule(triage.repoRules, target)
    const set = (rule: 'pinned' | 'muted') => () => {
      triage.onRepoRule(target, own === rule ? '' : rule)
    }

    return [
      {
        label: `${own === 'pinned' ? 'Unpin' : 'Pin'} ${scope}`,
        icon: <PinIcon filled={own === 'pinned'} className={iconClass} />,
        title:
          own === 'pinned'
            ? 'Its items go back among the rest'
            : 'Keep its items at the top of every list, after the items pinned themselves',
        divided,
        onSelect: set('pinned'),
      },
      {
        label: `${own === 'muted' ? 'Unmute' : 'Mute'} ${scope}`,
        icon: <MuteIcon className={iconClass} />,
        title:
          own === 'muted'
            ? 'Its items go back to the lists'
            : 'Move its items to each list’s Muted tab, out of the counts, the menu bar, and the notifications',
        onSelect: set('muted'),
      },
    ]
  }

  return [
    ...toggles(repo, repo, false),
    ...toggles(org, `everything in ${org}`, true),
    {
      label: 'Pinned and muted…',
      icon: <SlidersIcon className={iconClass} />,
      title: 'See every pinned and muted repository and org in Settings',
      divided: true,
      onSelect: triage.onRepoRuleSettings,
    },
  ]
}

// inboxActions are the triage actions the row's quiet controls offer, gathered in the
// menu as well: in the inbox tab, Refresh, Pin, Snooze, Dismiss, and the repository's own
// pin or mute; in the Snoozed and Dismissed tabs, Unsnooze or Restore, Pin, the
// repository's pin or mute, and Refresh; in the Muted tab, Unmute, Pin, and Refresh.
function inboxActions(item: InboxItem, tab: TriageTab, triage: TriageActions, work: RowWork, start: StartWork) {
  const pinned = isPinnedItself(item)
  const refresh: MenuItem = {
    label: 'Refresh from GitHub',
    icon: <RefreshIcon className={iconClass} />,
    title: 'Read this item from GitHub again, without refreshing the rest of the inbox',
    disabled: work !== null,
    onSelect: () => {
      start('refresh', () => triage.onRefresh(item))
    },
  }
  const pin: MenuItem = {
    label: pinned ? 'Unpin' : 'Pin to top',
    icon: <PinIcon filled={pinned} className={iconClass} />,
    onSelect: () => {
      if (pinned) {
        triage.onUnpin(item)
      } else {
        triage.onPin(item)
      }
    },
  }

  const repository: MenuItem = {
    label: 'Pin or mute repository',
    icon: <MuteIcon className={iconClass} />,
    items: () => repoRuleItems(item, triage),
  }

  if (tab === 'muted') {
    const target = mutedTarget(item, triage.repoRules)

    return [
      {
        label: `Unmute ${target}`,
        icon: <MuteIcon className={iconClass} />,
        title: 'Its items go back to the lists',
        onSelect: () => {
          triage.onRepoRule(target, '')
        },
      },
      pin,
      { ...refresh, divided: true },
    ]
  }

  if (tab !== 'inbox') {
    const snoozed = tab === 'snoozed'

    return [
      {
        label: snoozed ? 'Unsnooze' : 'Restore',
        icon: <ResetIcon className={iconClass} />,
        title: snoozed ? 'End the snooze and put it back in the list now' : 'Put it back in the list',
        onSelect: () => {
          triage.onRestore(item)
        },
      },
      pin,
      repository,
      { ...refresh, divided: true },
    ]
  }

  return [
    refresh,
    pin,
    repository,
    {
      label: 'Snooze',
      icon: <ClockIcon className={iconClass} />,
      divided: true,
      items: () => snoozeItems(item, triage),
    },
    {
      label: 'Dismiss',
      icon: <ArchiveIcon className={iconClass} />,
      title: 'Hide it until you restore it',
      onSelect: () => {
        triage.onDismiss(item)
      },
    },
  ]
}

// githubEdits are the edits the user can make to the item on GitHub: off its assignees
// when it is assigned to them, and off its requested reviewers when it asks for their
// review. A pull request that asks only a team cannot take one member off, so its item
// says why instead. Each spends one REST request of the API budget.
function githubEdits(item: InboxItem, edits: EditActions, work: RowWork, start: StartWork): MenuItem[] {
  const out: MenuItem[] = []
  const busy = work !== null

  if (item.assigned) {
    out.push({
      label: 'Unassign me',
      icon: <UnassignIcon className={iconClass} />,
      title: 'Take yourself off its assignees on GitHub. Uses one REST request.',
      disabled: busy,
      onSelect: () => {
        start('unassign', () => edits.onUnassign(item))
      },
    })
  }

  if (item.reviewRequest === 'team') {
    out.push({
      label: 'Remove me as reviewer',
      icon: <EyeOffIcon className={iconClass} />,
      title: `GitHub asks ${teamsText(item.reviewTeams)} to review this, not you by name, and takes a team’s request off for every member or none. Dismiss it to hide it here.`,
      disabled: true,
    })
  } else if (item.reviewRequest === 'you') {
    out.push({
      label: 'Remove me as reviewer',
      icon: <EyeOffIcon className={iconClass} />,
      title: 'Take yourself off its requested reviewers on GitHub. Uses one REST request.',
      disabled: busy,
      onSelect: () => {
        start('review', () => edits.onRemoveReview(item))
      },
    })
  }

  const [first] = out

  if (first !== undefined) {
    out[0] = { ...first, divided: true, section: githubSection }
  }

  return out
}

// ItemMenu is a row's menu of everything it can do with its item: open it on GitHub or
// copy its link; the inbox actions of its quiet controls, gathered in a submenu, so each
// is a click away even without the pointer on the row; and, when they apply, the edits
// that take the user off the item on GitHub. Its button shows with the row's quiet
// controls, and stays in sight, turning to a spinner, while the row is busy with GitHub.
// Its items are built only as it opens.
export function ItemMenu({ item, tab, triage, edits, work, start }: ItemMenuProps) {
  const onReview = edits.onReview
  const review: MenuItem[] =
    item.kind === 'pr' && onReview !== undefined
      ? [
          {
            label: 'Review…',
            icon: <EyeIcon className={iconClass} />,
            title: 'See its changes and approve, comment, or merge it here',
            onSelect: () => {
              onReview(item, tab)
            },
          },
        ]
      : []
  const items = (): MenuItem[] => [
    ...review,
    {
      label: 'Open on GitHub',
      icon: <ExternalIcon className={iconClass} />,
      onSelect: () => {
        openExternal(item.url)
      },
    },
    {
      label: 'Copy link',
      icon: <LinkIcon className={iconClass} />,
      onSelect: () => {
        edits.onCopyLink(item)
      },
    },
    {
      label: 'Inbox actions',
      icon: <InboxIcon className={iconClass} />,
      title: 'Snooze, dismiss, pin, mute, or refresh it. These stay on this Mac.',
      divided: true,
      items: () => inboxActions(item, tab, triage, work, start),
    },
    ...githubEdits(item, edits, work, start),
  ]
  const editing = leaving(work)

  return (
    <div className={hoverClass(work)}>
      <MenuButton
        label="More actions"
        title={work === null ? 'More actions' : workWords[work]}
        items={items}
        className={`${ghostClass} px-1`}
      >
        {editing ? (
          <span
            aria-hidden="true"
            className="size-3.5 rounded-full border-[1.5px] border-current border-t-transparent motion-safe:animate-spin"
          />
        ) : (
          <MoreIcon className="size-4" />
        )}
      </MenuButton>
    </div>
  )
}
