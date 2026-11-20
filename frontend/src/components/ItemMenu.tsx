import { use } from 'react'

import { CardPrefsContext } from '../lib/cardPrefs'
import { joinList } from '../lib/format'
import { nudgeable } from '../lib/nudge'
import { orgOf, ownRule } from '../lib/repoRules'
import { leaving, workWords, type RowWork, type StartWork } from '../lib/rowWork'
import { isPinnedItself, type TriageTab } from '../lib/triage'
import type { InboxItem } from '../platform'
import { AutopilotMenuContext, type AutopilotMenu } from './autopilot/RoutineMenu'
import {
  ArchiveIcon,
  AutopilotIcon,
  ClockIcon,
  CommentIcon,
  DotIcon,
  ExternalIcon,
  EyeIcon,
  EyeOffIcon,
  InboxIcon,
  InfoIcon,
  LinkIcon,
  MoreIcon,
  MuteIcon,
  PinIcon,
  PlayIcon,
  PlusIcon,
  RefreshIcon,
  ResetIcon,
  RingIcon,
  RoutineIcon,
  SlidersIcon,
  SparkIcon,
  UnassignIcon,
} from './icons'
import { MenuButton, type MenuItem } from './Menu'
import { ghostClass, hoverClass, mutedTarget, openOnGitHub, snoozeItems, type TriageActions } from './TriageControls'

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
  // onNudge opens the Nudge dialog for a pull request waiting on its author, where the user
  // sees the comment before it is posted. Without it the menu offers no nudge, and no card
  // offers one as its primary.
  onNudge?: ((item: InboxItem) => void) | undefined
  // onOpen opens the item in its sheet, from the row's tab: the review sheet for a pull
  // request, the issue sheet for an issue. Without it the menu offers neither.
  onOpen?: ((item: InboxItem, tab: TriageTab) => void) | undefined
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

  return `the ${joinList(named)} teams`
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

// markItem is Mark as read on an unread item and Mark as unread on one read, or nothing
// while Settings → Cards' Unread dots is off.
function markItem(item: InboxItem, triage: TriageActions, unreadDots: boolean): MenuItem[] {
  if (!unreadDots) {
    return []
  }

  return [
    item.unread
      ? {
          label: 'Mark as read',
          icon: <RingIcon className={iconClass} />,
          onSelect: () => {
            triage.onMarkRead(item)
          },
        }
      : {
          label: 'Mark as unread',
          icon: <DotIcon className="size-3.5 shrink-0 text-info" />,
          onSelect: () => {
            triage.onMarkUnread(item)
          },
        },
  ]
}

// inboxActions are the triage actions the row's quiet controls offer, gathered in the
// menu as well: in the inbox tab, Refresh, Pin, Snooze, Dismiss, and the repository's own
// pin or mute; in the Snoozed and Dismissed tabs, Unsnooze or Restore, Pin, the
// repository's pin or mute, and Refresh; in the Muted tab, Unmute, Pin, and Refresh. After
// Pin in each, Mark as read or Mark as unread, while the cards show unread dots.
function inboxActions(
  item: InboxItem,
  tab: TriageTab,
  triage: TriageActions,
  work: RowWork,
  start: StartWork,
  unreadDots: boolean,
) {
  const pinned = isPinnedItself(item)
  const mark = markItem(item, triage, unreadDots)
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
      ...mark,
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
      ...mark,
      repository,
      { ...refresh, divided: true },
    ]
  }

  return [
    refresh,
    pin,
    ...mark,
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

// autopilotItems are what Autopilot offers for the item: Run a routine, each of the user's
// routines that are on and act on its kind, which runs it now; Make a routine from this…,
// which opens the builder filled in from the item; Describe a routine for this…, which
// opens Describe it with the item attached; and Why didn't a routine run?, which shows
// what each decides about it.
function autopilotItems(item: InboxItem, autopilot: AutopilotMenu): MenuItem[] {
  const routines = autopilot.runnable(item)
  const asks = autopilot.askable(item)

  return [
    routines.length === 0
      ? {
          label: 'Run a routine',
          icon: <PlayIcon className={iconClass} />,
          title:
            asks.length === 0
              ? `No routine of yours acts on ${item.kind === 'pr' ? 'pull requests' : 'issues'}`
              : 'Switch a routine on to run it here',
          disabled: true,
        }
      : {
          label: 'Run a routine',
          icon: <PlayIcon className={iconClass} />,
          items: routines.map((r) => ({
            label: r.name,
            icon: <RoutineIcon className={iconClass} />,
            title: r.sentence === '' ? 'Run it on this item now' : r.sentence,
            onSelect: () => {
              autopilot.onRun(r, item)
            },
          })),
        },
    {
      label: 'Make a routine from this…',
      icon: <PlusIcon className={iconClass} />,
      title: 'Open the builder filled in from this item: its author, its owner, and what is happening to it',
      onSelect: () => {
        autopilot.onMake(item)
      },
    },
    {
      label: 'Describe a routine for this…',
      icon: <SparkIcon className={iconClass} />,
      title:
        'Tell Claude Code what a routine should do with items like this. It sees the item’s address, kind, repository, author, labels, and failed checks’ names, never its text or code.',
      onSelect: () => {
        autopilot.onDescribe(item)
      },
    },
    {
      label: 'Why didn’t a routine run?',
      icon: <InfoIcon className={iconClass} />,
      title: 'See what a routine decides about this item, condition by condition, and what its history says',
      onSelect: () => {
        autopilot.onWhy(item)
      },
    },
  ]
}

// githubEdits are the edits the user can make to the item on GitHub: a nudge of the
// author of a pull request waiting on them, in any tab and however long it has been quiet;
// off its assignees when it is assigned to them; and off its requested reviewers when it
// asks for their review. A pull request that asks only a team cannot take one member off,
// so its item says why instead. Each spends one REST request of the API budget.
function githubEdits(item: InboxItem, edits: EditActions, work: RowWork, start: StartWork): MenuItem[] {
  const out: MenuItem[] = []
  const busy = work !== null
  const onNudge = edits.onNudge

  if (onNudge !== undefined && nudgeable(item)) {
    out.push({
      label: `Nudge @${item.author}…`,
      icon: <CommentIcon className={iconClass} />,
      title: 'Post a comment asking the author about the changes you requested. Uses one REST request.',
      disabled: busy,
      onSelect: () => {
        onNudge(item)
      },
    })
  }

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
// is a click away even without the pointer on the row; Autopilot's, where the window
// provides them (AutopilotMenuContext); and, when they apply, the edits on GitHub: a
// nudge of a quiet author, and those that take the user off the item. Its button shows
// with the row's quiet
// controls, and stays in sight, turning to a spinner, while the row is busy with GitHub.
// Its items are built only as it opens.
export function ItemMenu({ item, tab, triage, edits, work, start }: ItemMenuProps) {
  const autopilot = use(AutopilotMenuContext)
  const { unread: unreadDots } = use(CardPrefsContext)
  const onOpen = edits.onOpen
  const review: MenuItem[] =
    onOpen === undefined
      ? []
      : [
          {
            label: item.kind === 'pr' ? 'Review…' : 'View issue…',
            icon: <EyeIcon className={iconClass} />,
            title:
              item.kind === 'pr'
                ? 'See its changes and approve, comment, or merge it here'
                : 'Read its conversation, and comment on it or close it here',
            onSelect: () => {
              onOpen(item, tab)
            },
          },
        ]
  const items = (): MenuItem[] => [
    ...review,
    {
      label: 'Open on GitHub',
      icon: <ExternalIcon className={iconClass} />,
      onSelect: () => {
        openOnGitHub(item, triage)
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
      items: () => inboxActions(item, tab, triage, work, start, unreadDots),
    },
    ...(autopilot === null
      ? []
      : [
          {
            label: 'Autopilot',
            icon: <AutopilotIcon className={iconClass} />,
            title: 'Run a routine on it now, or see why one didn’t run',
            items: () => autopilotItems(item, autopilot),
          },
        ]),
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
