# Needs you

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Agents in the terminal, readiness, and Needs your OK](../../architecture/autopilot/agents-in-the-terminal.md)

Activity's **Needs you**, above the history, holds what a routine can't do without you:

- **Waiting for your OK:** an Ask first routine's match, a catch-up after a long gap, a run that
  was interrupted, and what a launch held back. Each card says what would run and why it waits
  (*Held: it changes agent configuration (.claude/settings.json)*, *pr-88 has 3 uncommitted
  files*, *No local clone of acme-org/sdk*), with **Run**, **Skip this commit**, and **Open**. A
  hold's card shows what the hold dialog does, and its **Run on 4f2a9c1** confirms that commit
  only: a push since holds it again. A missing clone's offers **Clone and run**. A
  [GitHub action](changes-on-github.md)'s card says what it would send next
  (*Next on GitHub: Ask Dependabot to rebase*), and its button says what you let go: **Send
  it** (Ask first, a catch-up, or an outside contributor's item), **Send anyway** for one its
  sheet offers with warnings, which the card shows and the button accepts exactly, or **Send
  it again** for one the app quit while sending. A
  [Merge it](merge-and-post-review.md)'s card shows the verdict on the checks and
  what it would do (*Next on GitHub: Merges it (squash).*), with **Merge**; a Post the agent's
  review's shows the agent's review (its verdict, its text, and how many line comments), with
  **Post approval** or **Post request for changes** for its verdict, **Post as comment**, and
  **Edit in the sheet**, which skips it and opens the review sheet, where the agent's notes
  start your own review.
- **Matched at once:** a routine's first pass that matched more than it takes on by itself, as
  one card naming the first few items, with **Run on them**, which lets every one go at
  Autopilot's pace, and **Skip them**, which skips every match the card shows at once: one
  that came since the card was read still waits, and the toast says when some had moved on.
- **Can't start yet:** something this Mac lacks, a tool the repository needs, or a prompt gone
  from your library, naming the fix, with **Check again**. Once fixed, an Autopilot routine's
  runs start by themselves, and an Ask first routine's after your OK, as they would have anyway.
- **Queued** follows: what starts by itself, each saying what it waits on.

Each match that comes to wait for you is told once as a **Needs your OK** notification; its
banner opens its card.

## Run a routine

An item's **⋯** menu has **Autopilot ▸**: **Run a routine ▸** lists your routines that act on its
kind, and runs one on it now, once, needing no OK and counting toward no cap or try (every other
limit still holds it). A routine whose conditions don't match the item says why, and offers **Run
anyway**. **Why didn't a routine run?** shows a routine's decision on the item now, from what is
stored, asking GitHub nothing:

- what of the routine itself holds it back: *It's switched off*, *Autopilot is paused*, *It's in
  Dry run*, *It belongs to @other-login*, and what this Mac lacks for it, as This Mac says it;
- when the item isn't in what it watches, why, a line for each source: *It isn't asking for your
  review: it's in your inbox, opened by you.*, *Not in its search: its author is octo-other,
  not Dependabot.*, *it's in acme-labs/web, not acme-org*, or, for an item its search should
  find, that the next refresh brings it;
- each condition with the value it compared, and what its history says (*2 of 2 tries used.*, and
  its record for the occurrence now, as *Its record for this: …*);
- and, folded, **Its ledger for this item**: every match the routine recorded on it, newest
  first, a Dry run's marked.

**Make a routine from this…**, beside them, opens the builder filled in from the item, as
[Templates](templates-and-quick-setup.md) says, and **Describe a routine for this…** opens
[Describe it](describe-it.md) with the item attached.
