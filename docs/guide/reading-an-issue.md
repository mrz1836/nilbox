# Reading an issue

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/issues`](../architecture/issues.md), [`internal/timeline`](../architecture/timeline.md), [`internal/people`](../architecture/people.md), [`internal/assignees`](../architecture/assignees.md)

Clicking an issue's title opens the **issue sheet**, the review sheet's twin for issues, so you
can read an issue, discuss it, hand it to an agent, and close it without leaving Nilbox. Issues
reach the inbox only when they are assigned to you, in **Yours**, so these are your own work
items.

## What it shows

- **The header:** the issue's icon in its state's color (green while open, blue once closed as
  completed, grey as not planned or a duplicate), the title, `owner/repo#n`, who opened it and
  when, with the card's trust tag, a **Locked** tag on a locked conversation, the issue's type,
  and where it sits in its list. **←** and **→** step through the list's cards as the [review
  sheet](reviewing-a-pull-request.md)'s do, pull requests included, and the triage buttons,
  **↗**, **✕**, and **Escape** work as there.
- **The conversation** in the middle, oldest first: the description, rendered from markdown as
  safely as a pull request's, with its reactions; then every comment, with who wrote it and when
  (the full date on hover), **Author**, **You**, and **bot** tags, whether it was edited, **Quote
  reply**, which quotes it into your comment, and **↗**. A comment someone hid is folded, saying
  why, and so is a bot's, to its first line; open either to read it. Events take a line each:
  mentions by other pull requests and issues, linked and unlinked pull requests, closes (as what,
  by what), reopens, assignments, renames, duplicates, and labels, the ones a person changed at
  once on one line. **Show N earlier entries** reads the part of a long conversation before the
  newest hundred entries, 1 GraphQL point per hundred, only when you click it.
- **New since your last visit:** a divider marks what came after the conversation as you last
  opened it, and the sheet scrolls to it once. It goes by GitHub's own times, so your Mac's clock
  can't move it, and your own comments are never new to you.
- **Where it stands,** at the top of the right column, in its color: **Fix merged: #14 into
  release-1.2**, **Fix in progress: #14 · draft · checks failing**, **You replied last**,
  **Assigned to you · opened 3 days ago**, **Locked**, **Closed as not planned by octo-other**,
  or that GitHub can't find it any more. Under it, the issue's worktree and agent session, as
  its card shows them.
- **The details:** **Development** (the pull requests that close it once merged, with their
  state, checks, and review, then the ones that mentioned it), assignees, labels, projects, the
  milestone (its due day, in amber once it has passed, and how far along it is), sub-issues with
  their progress, the issue it is part of, and who took part.
- **Labels and assignees,** with triage access or more to an open issue's repository: **Edit**
  beside **Labels** lists the repository's labels, the first hundred in name order, to filter
  and check, each checked while the issue has it; **Assignees** works as on the review sheet
  (see [Assignees](reviewing-a-pull-request.md)): the same picker, sections, and search, **×** to
  unassign someone, and, with no one assigned, **Assign yourself** and a few others in one
  click. They wait for the sheet's first read, so they start from who the issue is assigned to.
  **Apply** (or **⌘↩**) sends only what differs from the issue, unassigning before it assigns,
  checks every answer, since GitHub ignores a change an account can't make rather than refusing
  it, and reads the issue again once GitHub made any of it; the picker stays open until GitHub
  answers, and a step that failed says why there, while those before it stand. You take
  yourself off with **Unassign me**, in the action menu, never here.
- **The latest agent session** on the issue, as on the review sheet, with **Use as comment**.

## Links between the sheets

A pull request or issue either sheet names, in the details, the conversation's events, or the review
sheet's **Closes** row, opens in its own sheet when it is in your inbox, at its place in its list,
with a **‹ acme-org/api#12** step in the header back to the sheet you came from (while that card is
still in your inbox); following another link replaces that step rather than stacking another.
Anything else opens on GitHub, which the **↗** beside it says.

## Commenting and closing

Write in **Your comment** and press **Comment**, or **⌘↩**. **Replies** above it fills the text in
from one of your [comment replies](prompts.md#review-replies) for issues, filled in for this issue,
with **No reply** to empty it and **Manage replies…** to edit them; none is picked for you, so
**⌘↩** never posts text you didn't write or choose. The button's menu also offers **Close as
completed…** and **Close as not planned…**, which become the button once chosen, and **Unassign
me**. When a fix has merged, the button starts as **Close as completed…**. Every close asks first,
says your text is posted as a comment before it, and can be undone on GitHub by reopening the issue.
The closes wait for the sheet's first read from GitHub, and are left out when GitHub doesn't let
your account close the issue. On a locked conversation you can't write to, **Comment** is held back,
and a close goes only without text.

## Never over a comment you haven't read

A close remembers the comments you saw. Before it sends anything, it reads the issue's state and its
newest ten comments (1 GraphQL point): when someone else commented since you opened it, nothing is
sent, the sheet keeps your draft, says who, and reads the issue again, so you can read it first;
when someone closed it first, it leaves the inbox and the toast says so. Your own comments never
stop it. Once your comment is posted, anything that keeps the close from going says the comment was
posted, so you never post it twice.

While an action runs, the sheet ticks off each step GitHub finishes and takes no input; then it
closes, and a toast says what came of it. **Unassign me** closes the sheet with its toast once
GitHub takes you off, or says in the sheet why it couldn't.

## Closed since the last refresh

An issue someone closed on GitHub after the inbox last read it gives way as a [merged or closed pull
request](reviewing-a-pull-request.md) does: the sheet's read finds it closed, the sheet closes or
steps past it, the toast says **acme-org/api#12 was closed** with **Open on GitHub**, and the card
leaves the inbox. When someone closed it first while your close was on its way, the sheet stays,
keeping your text, until you answer its notice.

**Drafts** are kept per issue for the session, as the review sheet's are: your text and your
chosen action, cleared once the action succeeds, but not when a new comment stopped a close.

## Changed since it was read

When the card shows newer activity or more comments than the sheet's read, which the inbox's refresh
brings at no cost, the sheet says so and offers **Reload**.

## What it costs

Opening costs nothing: it shows what the sheet read last time, or the card. Its refresh costs 1
GraphQL point, and none within 20 seconds of the last unless the card shows newer activity or you
click refresh. Each write costs what the [edits table](item-menu.md) says.

## What it keeps

The sheet keeps what it read of each issue in the app's database on this Mac: at most 1 MiB per
issue (a very long description or comment is cut short, marked, with a link to the rest), 32 MiB and
200 issues in all, the least recently opened dropped first, anything unopened for 14 days dropped,
and an issue's own once it is closed. Nothing leaves your Mac but the requests to GitHub's API.
