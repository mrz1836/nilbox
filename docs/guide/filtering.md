# Filtering

The toolbar's field filters the page you're on: **Filter inbox**, **Filter notifications**,
**Filter worktrees**, or **Find a setting**. Prompts keeps the search in its own sidebar. Everything it searches is
already in the window, so a filter is instant and asks GitHub nothing. Each page keeps its
own query while you look at another, until the app quits; a filter never comes back after a
relaunch, where it would make items look lost.

**Keys.**

| Key | What it does |
|---|---|
| **⌘F**, or **/** while you're not typing | Focuses the field, with its text selected. On Prompts, ⌘F focuses the prompt search. ⌃⌘F still toggles full screen. |
| **↓** and **↑** | Choose among the suggestions, or open them over plain words. |
| **Enter** or **Tab** | Inserts the chosen suggestion. |
| **Escape** | Closes the suggestions, then clears the query, then leaves the field. Once the field is empty and left, Escape closes Settings as usual. |

**Suggestions.** Focus the empty field for what the page can be narrowed by, each with how
many it would match: pull requests, issues, drafts, pinned items, a repository, a label, an
author, and a project status or failing checks while some item has one. Type a key such as
`label:` or `repo:ac` and the field lists the values that fit, each counted among what the
rest of your query already matches. A value with spaces is inserted in quotes. Keys the page
understands are tinted as you type, and the field shows **5 matches** or **No matches** (just
the number when it's narrow), with **✕** to clear it.

**The query.** Words AND together: each must appear, ignoring case and accents, in what the
item says. `"a quoted phrase"` matches as a phrase, and `-word` leaves out what has it.
`key:value` filters by a key the page knows; any other `word:` is just text, so a title such
as `fix: x` still matches. `-key:value` leaves out what matches, and `key:a,b` means either.
Repeating a key means either for something an item has one of (`repo:a repo:b`) and both for
something it can have several of (`label:bug label:ui`). A value matches as part of one while
you type, and exactly once it names one an item has, as a picked suggestion does. `#123`
matches item 123, and a bare `123` matches it too.

| Page | Keys |
|---|---|
| Inbox | `is:pr`, `is:issue`, `is:draft`, `is:pinned` (pinned itself, or with its repository or org), `is:conflict`, `is:pushed` (pushed since you requested changes) (`is:open` matches everything); `repo:`, `org:`, `author:` (with `@me` for you), `label:`, `status:` (project status), `project:`; `checks:failing`, `checks:awaiting` (workflow runs held for a maintainer's approval), `checks:passing`, `checks:pending`; `review:approved`, `review:changes_requested`, `review:required` |
| Notifications | `repo:` (`owner/name`), `type:` (on the Nilbox tab `review`, `assigned`, `snooze`, `release`, `run`; on the GitHub tab `pr`, `issue`, `release`, `actions`, `other`), `is:unread`, `is:read`; on the GitHub tab also `is:open`, `is:merged`, `is:closed` (the item's state), and `reason:` (GitHub's reason, such as `mention` or `review_requested`) |
| Worktrees | `repo:`, `branch:`, `agent:`; `is:dirty`, `is:unpushed` (commits not on GitHub), `is:locked`, `is:in-use`, `is:finished`, `is:review` (needs a look), `is:pr`, `is:issue`, `is:open`, `is:merged`, `is:closed` |
| Settings | Words only |

**What the inbox searches.** An item's title, its repository as `owner/name`, its number, its
author, its branch, its labels, its projects and their statuses, and the teams asked to review
it, in every list and in Snoozed, Dismissed, and Muted too. `checks:`, `review:`, and `status:` read
an item the way its card shows it. An item whose details haven't arrived yet matches no key
that needs them, such as `label:`. Descriptions and comments aren't kept on your Mac, so the
filter can't search them.

**The inbox while filtered.** Each list keeps its order (pinned items first, then the status
sort) and shows only its matches, and its tab says how many, as **5 of 38**, in a dashed pill
of its own. Snoozed, Dismissed, and Muted count their matches too, and a tab with none points to the
one that has some, as **No matches here · 2 in Snoozed**. A list with no match anywhere hides
and comes back as you left it, and a collapsed list shows open while you filter without
forgetting that it was collapsed. The words you typed are marked in each title. When nothing
matches, the top of the page says so and offers **Clear filter**. The **Inbox** tab's count
stays the whole inbox's.

**Click to filter.** A card's repository, author, and labels are buttons: click one to add it
to the filter, and click it again to take it out.

**Showing an item.** When the menu bar or a notification shows an item, the inbox's filter is
cleared first, so the item is always found.

**The Notifications page while filtered.** The field filters the tab that shows, within its
**Show** and **Type**, and counts what the list then shows: on the GitHub tab, each item's line
once, however many of its notifications match. Words match an entry's title, its repository,
its number, and what happened (the kind, such as **Back from snooze**, or GitHub's reason, such
as **Mentioned**), and an entry's detail. The suggestions are the tab's own. When nothing
matches, the list says so and offers **Clear the filter**. **Cleared** isn't filtered.

**The Worktrees view while filtered.** A clone with no matching worktree hides. A clone whose
name matches shows all its worktrees; any other shows only the matching ones, and its heading
says **3 of 7** where it counts its lanes, and stays unfolded. A clone or worktree showing an
error stays in sight whatever the filter. When nothing matches, the top of the view says so and
offers **Clear the filter**. A [chip](worktrees-view.md) writes its own `is:` filter. The
cleanups and **Recently cleaned up** aren't filtered: their confirmation lists everything they
would take.

**Find a setting.** The sidebar narrows to the sections with a matching setting, each with how
many match, and turns to the first of them when the open section has none. The matching
settings are marked, and the first scrolls into view. Words match each setting's name and the
words it's known by, such as "dark mode" for the theme or "font" for the text size. The search
is cleared whenever you leave Settings.
