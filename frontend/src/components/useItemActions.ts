import type { ReactNode } from 'react'

import { attempt } from '../lib/call'
import { ownRule } from '../lib/repoRules'
import { endedText, type EndedItem } from '../lib/sheet'
import { formatWhen } from '../lib/time'
import { findItem, isDismissed, type TriageTab } from '../lib/triage'
import type { InboxState } from '../lib/useInbox'
import { useClock } from '../lib/useNow'
import {
  copyText,
  openExternal,
  setRepoRule,
  type InboxItem,
  type RepoRule,
  type RepoRules,
  type Settings,
} from '../platform'
import { teamsText, type EditActions } from './ItemMenu'
import { useNudge } from './NudgeDialog'
import type { ToastMessage } from './Toast'
import type { TriageActions } from './TriageControls'

// ruleChangeWords is what the toast says once a target's rule changed from was to rule,
// and what that does.
function ruleChangeWords(target: string, was: RepoRule, rule: RepoRule): { text: string; detail: string } {
  switch (rule) {
    case 'pinned':
      return { text: `Pinned ${target}`, detail: 'Its items stay at the top of every list.' }
    case 'muted':
      return { text: `Muted ${target}`, detail: 'Its items wait in each list’s Muted tab.' }
    case '':
      return was === 'muted'
        ? { text: `Unmuted ${target}`, detail: 'Its items are back in the lists.' }
        : { text: `Unpinned ${target}`, detail: 'Its items are back among the rest.' }
  }
}

export interface ItemActionsOptions {
  // inbox is the window's shared inbox state (useInbox), whose triage and edits the rows
  // make.
  inbox: InboxState
  // snoozeDays is how long a row's Snooze button hides an item, and onSnoozeSettings opens
  // its setting.
  snoozeDays: number
  onSnoozeSettings: () => void
  // repoRules are the pinned and muted repositories and orgs, from the settings;
  // onSettingsSaved receives the settings as stored after each change, and
  // onRepoRuleSettings opens them in Settings → Repositories.
  repoRules: RepoRules
  onSettingsSaved?: ((settings: Settings) => void) | undefined
  onRepoRuleSettings?: (() => void) | undefined
  // showToast says what an action did, or why it couldn't, and closeToast takes it away.
  showToast: (message: Omit<ToastMessage, 'id'>) => void
  closeToast: () => void
  // onOpen opens an item in its sheet, from a row's menu; without it the menu offers no
  // sheet.
  onOpen?: ((item: InboxItem, tab: TriageTab) => void) | undefined
  // listed reports rows of the inbox's own lists, whose item a refresh that leaves it out
  // of the inbox closed or merged; a routine's Watching list lists items in no list.
  listed?: boolean
  // onManageReplies opens the replies in the Prompts tab, from the Nudge dialog's replies
  // menu; without it the menu has no way there.
  onManageReplies?: (() => void) | undefined
}

// ItemActions are what the rows' controls and menus do, and the triage a sheet's header
// makes, each of which says what it did in a toast.
export interface ItemActions {
  triage: TriageActions
  edits: EditActions
  // dismissIt, snoozeIt, and restoreIt set an item aside or bring it back, and changeRule
  // pins or mutes a repository or an org; each resolves with whether it took the change.
  // snoozeIt's toast leads with lead ("Snoozed" unless given), then when it comes back.
  dismissIt: (item: InboxItem) => Promise<boolean>
  snoozeIt: (item: InboxItem, days: number, lead?: string) => Promise<boolean>
  restoreIt: (item: InboxItem) => Promise<boolean>
  changeRule: (target: string, rule: RepoRule) => Promise<boolean>
  // unassignOn takes the user off the item on GitHub and says so once GitHub made it; it
  // resolves with why it couldn't, or ''.
  unassignOn: (item: InboxItem) => Promise<string>
  // seen marks read the items a sheet showed, as it opens on one, steps away from one, or
  // closes. The user asked for none of it, so a failure goes to the console, the items
  // back as they were.
  seen: (ids: readonly number[]) => void
  // ended says what became of an item its sheet found merged or closed on GitHub since the
  // inbox last read it, as the sheet moves on from it, and offers it on GitHub.
  ended: (item: EndedItem) => void
  // nudgeDialog is the Nudge dialog the rows' edits open (edits.onNudge), while it is open,
  // which the list renders.
  nudgeDialog: ReactNode
}

// useItemActions builds what the rows of a list of items do with them: the inbox's own
// triage (each confirmed by a toast with an Undo), the pinned and muted rules, and the
// edits on GitHub, which show once GitHub has made them, a nudge of a quiet author through
// its dialog (useNudge). The inbox's lists and a routine's Watching list share it.
export function useItemActions({
  inbox,
  snoozeDays,
  onSnoozeSettings,
  repoRules,
  onSettingsSaved,
  onRepoRuleSettings,
  showToast,
  closeToast,
  onOpen,
  listed = true,
  onManageReplies,
}: ItemActionsOptions): ItemActions {
  const clock = useClock()
  const { dismiss, snooze, restore, pin, unpin, refreshItem, unassign, removeReviewRequest, markRead, markUnread } =
    inbox

  // failed reports a triage action the backend refused; the item is back as it was.
  const failed = (what: string, item: InboxItem, error: string) => {
    showToast({ text: `Could not ${what} “${item.title}”`, detail: error, tone: 'error' })
  }

  // mark marks an item read or unread, as the item's dot shows at once, and says why it
  // couldn't, when the dot goes back as it was.
  const mark = (item: InboxItem, unread: boolean) => {
    void (unread ? markUnread(item.id) : markRead([item.id])).then((result) => {
      if (!result.ok) {
        showToast({
          text: `Could not mark “${item.title}” ${unread ? 'unread' : 'read'}`,
          detail: result.error,
          tone: 'error',
        })
      }
    })
  }

  // undo puts an item back the way it was before it was snoozed or dismissed.
  const undo = (item: InboxItem) => () => {
    closeToast()
    void restore(item.id).then((result) => {
      if (!result.ok) {
        failed('undo the change to', item, result.error)
      }
    })
  }

  // setRule sets the rule of a target and records the settings as stored, or says why it
  // couldn't. It resolves with whether it did.
  const setRule = async (target: string, rule: RepoRule): Promise<boolean> => {
    const result = await attempt(setRepoRule(target, rule))

    if (!result.ok) {
      showToast({ text: `Could not change the rule for ${target}`, detail: result.error, tone: 'error' })

      return false
    }

    onSettingsSaved?.(result.data)

    return true
  }

  // changeRule pins, mutes, or takes the rule off a repository or an org, as a card asked,
  // and says so in a toast whose Undo puts back the rule the target had. The inbox shows the
  // change once the backend has rebuilt it under the new rules ("inbox:updated").
  const changeRule = async (target: string, rule: RepoRule): Promise<boolean> => {
    const was = ownRule(repoRules, target)
    const done = await setRule(target, rule)

    if (done) {
      showToast({
        ...ruleChangeWords(target, was, rule),
        undo: () => {
          closeToast()
          void setRule(target, was)
        },
      })
    }

    return done
  }

  const dismissIt = async (item: InboxItem): Promise<boolean> => {
    const result = await dismiss(item.id)

    if (result.ok) {
      showToast({ text: 'Dismissed', detail: item.title, undo: undo(item) })
    } else {
      failed('dismiss', item, result.error)
    }

    return result.ok
  }

  const snoozeIt = async (item: InboxItem, days: number, lead = 'Snoozed'): Promise<boolean> => {
    const result = await snooze(item.id, days)

    if (!result.ok) {
      failed('snooze', item, result.error)

      return false
    }

    const until = findItem(result.data, item.id)?.snoozedUntil ?? ''
    const text = until === '' ? lead : `${lead} until ${formatWhen(clock.getSnapshot(), new Date(until))}`

    showToast({ text, detail: item.title, undo: undo(item) })

    return true
  }

  const restoreIt = async (item: InboxItem): Promise<boolean> => {
    const result = await restore(item.id)

    if (result.ok) {
      showToast({ text: isDismissed(item) ? 'Restored' : 'Unsnoozed', detail: item.title })
    } else {
      failed('restore', item, result.error)
    }

    return result.ok
  }

  // triage is what every row's triage buttons do. It reads no time while rendering (the
  // toast after a snooze reads the clock when it shows), so the rows keep the same
  // actions from one tick to the next.
  const triage: TriageActions = {
    snoozeDays,
    onDismiss: (item) => {
      void dismissIt(item)
    },
    onSnooze: (item, days) => {
      void snoozeIt(item, days)
    },
    onRestore: (item) => {
      void restoreIt(item)
    },
    // A pin shows at once, as the item moves to the top, so it needs no toast.
    onPin: (item) => {
      void pin(item.id).then((result) => {
        if (!result.ok) {
          failed('pin', item, result.error)
        }
      })
    },
    onUnpin: (item) => {
      void unpin(item.id).then((result) => {
        if (!result.ok) {
          failed('unpin', item, result.error)
        }
      })
    },
    // A refreshed item that is still open updates in place; one that is not leaves the
    // inbox, which the toast explains.
    onRefresh: async (item) => {
      const result = await refreshItem(item.id)

      if (!result.ok) {
        failed('refresh', item, result.error)
      } else if (listed && findItem(result.data, item.id) === undefined) {
        showToast({ text: 'No longer open on GitHub', detail: item.title })
      }
    },
    onChangeDefault: onSnoozeSettings,
    repoRules,
    onRepoRule: (target, rule) => {
      void changeRule(target, rule)
    },
    onRepoRuleSettings: onRepoRuleSettings ?? (() => undefined),
    onMarkRead: (item) => {
      mark(item, false)
    },
    onMarkUnread: (item) => {
      mark(item, true)
    },
  }

  // unassignOn takes the user off the item on GitHub, from its card's menu or its sheet, and
  // says so once GitHub made it; it resolves with why it couldn't, or ''.
  const unassignOn = async (item: InboxItem): Promise<string> => {
    const result = await unassign(item.id)

    if (!result.ok) {
      return result.error
    }

    showToast({ text: 'Unassigned you on GitHub', detail: item.title })

    return ''
  }

  // nudging is the Nudge dialog the rows open. The comment it posts is the user's own
  // activity, so the item is marked read once GitHub posted it.
  const nudging = useNudge({
    nudge: inbox.nudge,
    snoozeDays,
    snoozeIt,
    markRead: (item) => {
      mark(item, false)
    },
    showToast,
    closeToast,
    onManageReplies,
  })

  // edits is what every row's menu does beyond triage. An edit shows once GitHub has
  // made it, as the item leaves the list, which the toast confirms; a pull request that
  // still asks a team for a review stays, and the toast says which. The activity an edit
  // made is the user's own, so the item is marked read once GitHub made it.
  const edits: EditActions = {
    onCopyLink: (item) => {
      void attempt(copyText(item.url)).then((result) => {
        showToast(
          result.ok
            ? { text: 'Link copied', detail: item.url }
            : { text: 'Could not copy the link', detail: result.error, tone: 'error' },
        )
      })
    },
    onUnassign: async (item) => {
      const error = await unassignOn(item)

      if (error !== '') {
        failed('unassign you from', item, error)

        return
      }

      mark(item, false)
    },
    onRemoveReview: async (item) => {
      const result = await removeReviewRequest(item.id)

      if (!result.ok) {
        failed('remove you as a reviewer of', item, result.error)

        return
      }

      const after = findItem(result.data, item.id)

      mark(item, false)
      showToast({
        text: 'Removed you as a reviewer on GitHub',
        detail: after?.reviewRequest === 'team' ? `It still asks ${teamsText(after.reviewTeams)}` : item.title,
      })
    },
    onNudge: nudging.onNudge,
    onOpen,
  }

  const seen = (ids: readonly number[]) => {
    void markRead(ids).then((result) => {
      if (!result.ok) {
        console.error('could not mark the items read:', result.error)
      }
    })
  }

  const ended = (item: EndedItem) => {
    showToast({
      text: endedText(item),
      detail: item.title,
      action: {
        label: 'Open on GitHub',
        onClick: () => {
          closeToast()
          openExternal(item.url)
        },
      },
    })
  }

  return {
    triage,
    edits,
    dismissIt,
    snoozeIt,
    restoreIt,
    changeRule,
    unassignOn,
    seen,
    ended,
    nudgeDialog: nudging.dialog,
  }
}
