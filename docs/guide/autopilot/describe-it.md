# Describe it

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [`internal/describeit`](../../architecture/describeit.md), [`internal/autopilot/kit`](../../architecture/autopilot/kit.md), [`internal/mcpserver`](../../architecture/mcpserver.md)

**Describe it** turns what you say into Autopilot: tell it what you want, in your own words, and
Claude Code proposes the routines, the prompts they run, and the settings and rules they lean
on, as cards you review, test, and apply one part at a time. Nothing changes until you apply a
part, and each applies through the same calls as the builder, the Prompts tab, and Settings,
so a routine from a card is checked, saved, and kept exactly as one you built. It is part of the
Autopilot tab, so it shows while Autopilot (beta) is on, and it talks with Claude Code alone.
While Autopilot is off, Describe it does nothing in the background either: it keeps neither the
snapshot of your setup it hands Claude nor the plugin current, and watches for no proposal, and
it catches up on all of it once you turn Autopilot on.

## Ways in

- While you have no routine, the empty tab asks *Tell Autopilot what you want*, with three
  examples a click puts in the box (*When Dependabot’s security scans fail, fix it in the
  background and tell me how it went*, *Pin my pull requests when their checks fail*, and
  *Snooze passing bot PRs for a few days*). **Send** (⌘↩) opens Describe it with your message
  on its way, and **Open Describe it** goes there without one.
- **Describe it** sits in the tab's list, above **New routine**, with a count while proposals
  from [your own Claude Code](your-own-claude-code.md) wait.
- **Change with Claude…**, beside a routine's name and in its **⋯** menu, opens Describe it with
  the routine attached to your next message, for a change you describe.
- **Describe a routine for this…**, in an item's **⋯** menu (**Autopilot ▸**), attaches the
  item: its address, kind, repository, author, labels, and the names of the checks that failed
  at its head, never its title, text, comments, diff, or code.

## The conversation

The header names the agent, *Claude · default · opus · high* (its profile, model, and effort), with
**Change ▸**, which starts a new conversation with another profile, model, or effort, each
**Inherit** to take [Settings → Autopilot](settings.md)'s choice; then the tokens the conversation
used and what it cost, *about $0.12* (an estimate on a subscription, as for every run); and **New
conversation**, which asks first once you have sent something, and sets the conversation aside with
its cards, while what you applied stays applied. Below, oldest first, come your messages with what
they attach, and what Claude did with each: its replies; a plain line for each of its tools, marked
✓ or ✗ as it comes out (*Read how Autopilot works*, *Read your setup*, *Read about triggers*,
*Checked 2 routines and a settings change: 1 problem*, *Proposed a routine and a settings change*);
and its cards, each after the line that proposed it. A card a later one revises shrinks to one line,
*Revised below: …*, with **Show this card**.

## The composer

Write what you want and send it with **Send** or ⌘↩; what you attach shows as chips above the box,
each with **×**. While a turn runs, **Stop** stops it (*Stopped. Send another message to go on.*).
Under the box a line says what Claude sees: *Claude sees what you type, how Autopilot works, and the
names in your setup and inbox, never your items’ text or code.* While Claude Code is missing, older
than 2.1.328, or not signed in on its profile, the box is off and says why and what to do, with
**Check again**.

## What Claude hears

Each message tells Claude first what you did with its cards since the last one, which your message
shows quietly (*Claude heard: Added “Fix failing checks”, switched off; discarded the settings
change.*), so it builds on what you applied rather than propose it again.

## One conversation

There is one conversation at a time. It stays as you switch tabs and restart, and is removed, with
its cards, 30 days after it started; a [reset](../starting-over.md) removes it too. Quitting stops a
turn that runs, and the conversation goes on from it. Claude Code keeps its own transcript of each
conversation in the profile's folder, as it does every session's, and Nilbox never removes it.

## A card

Each proposal is a card headed *Claude says*, with Claude's summary of it, then its parts in the
order they apply: new prompts, routines, a settings change, and a rules change. Each part has its
own buttons, and once you act its outcome takes their place (*✓ Added, switched off · Show it*,
*Changes saved*, *Added to your prompts*, *Applied*, *Opened in the builder*, *Discarded*). From
then on the part shows what it did, never checked again against what it changed: a routine by its
name, a change by its sentence now, and settings and rules by what they set (*Daily cap → $5*). A
card of several parts has **Apply all**, which applies the parts left in that order and stops at the
first that fails, saying which and why; what it applied before stays applied. A part with problems
can't be applied (*It can’t be applied like this: ask Claude to fix it, or discard it.*), and while
one has them, **Apply all** is off.

| Part | Shows | Buttons |
|---|---|---|
| A new routine | **New**, the name it takes here (*Fix failing checks 2* when you have one of that name), its sentence, the mode it starts in (the one new routines start in, with Claude's suggestion beside it as a hint, as a template's is), its problems and warnings as the builder reads them, [This Mac](this-mac.md), what it matches in your inbox now (*Matches 3 pull requests in your inbox now.*, from what Nilbox has stored; for a routine that watches a search, *It watches a search, which Test reads.*), and its risks | **Add**, which saves it switched off; **Edit in builder**, which opens the builder filled in, to finish there; **Test**; **Discard** |
| A change to a routine | **Changes “<name>”**, *changed since Claude read it* when it did, its sentence **Now** and **With the change**, that it keeps its own mode and switch, and the rest as above | **Save changes**, which asks first, as the builder's save does, when the routine is on and the change reaches what it watches, fires on, checks, or fires once per; **Open the routine**; **Test**; **Discard** |
| A new prompt | Its nickname, title, the kind of item it is for, and what it is (*a coding agent runs it*, *a reply that comments*), with **Show the prompt** for its text | **Add**, with your library's defaults for the models, effort, plan mode, worktree, and flags, which a proposal can't set; **Discard** |
| A settings change | Each setting it changes, *Agents at once 2 → 1* | **Apply**, each through the setting's own control in Settings; **Discard** |
| A rules change | Each rule it changes, *Trust renovate[bot]*, *Mute acme-org/legacy*, *Ignore Deploy preview* | **Apply**; **Discard** |

## Risks

Under each part, before you apply it, a ⚠ line names each thing it would do that deserves a look,
and a quiet one what loosens nothing. Nilbox works them out from the part before and after, never
from Claude's words, and what a routine did before and does still is no new risk: a routine that is
on, outside Dry run, whose watch, trigger, conditions, or once per change; an agent that may push,
run `gh` on the item, or run with the profile's own permissions, or that gets a bigger budget, more
time, or commands beyond its preset; a merge, a review posted in your name, as its verdict, or
allowed to approve; each comment, label, rebase, and branch update it sends GitHub; a script it
runs; a new prompt an agent will run or a routine will comment; a routine's limits, active hours, or
daily budget loosened; new routines starting in Autopilot, agents starting through an outage, a
shorter history or run log, a daily cap raised or taken away, and a longer gap caught up on without
asking; a bot newly trusted, a repository or org muted, a workflow's held runs ignored; and,
quietly, a routine that changed after Claude read it, or a name another routine has.

## What a card can't change

A proposal can add and change routines, add prompts, change Autopilot's settings, and change the
trusted bots, the pinned and muted repositories and orgs, and the ignored workflows. It can never
switch a routine on or off or change its mode, switch Autopilot off, or pause it: a proposal has no
place for them, and one that names them is refused before you see it. A new routine always arrives
switched off.

## How Claude works here

Each message is one turn of Claude Code, run headless on its profile's own sign-in (with an API key
from your shell only while **Use an API key for Claude Code when one is set** is on), in an empty
folder of Nilbox's own, with none of Claude Code's own tools, none of your or a folder's settings,
hooks, MCP servers, or `CLAUDE.md`, and no tool but Nilbox's: its tool server, the app's own binary
in a tool mode, which reads how Autopilot works and the snapshot of your setup Nilbox writes, checks
a proposal, and writes it for the tab. The tool server reads nothing but that snapshot and the
proposals' own folders, runs nothing, reaches no network, and asks GitHub nothing; only a card's
**Test** reads GitHub, on your click. A turn whose own setup isn't that (Nilbox's tools didn't
start, or another tool showed up) stops before Claude goes on, and says so. Each turn may spend $2,
as Claude Code counts it, and stops there (*The turn reached its budget of $2 before it finished.*);
turns count toward none of Autopilot's limits, its daily cap included. A turn running shows in
Claude Code's own list of sessions as *Nilbox: Describe it*, and no session pill follows it. What
Claude reads is generated from this version of Nilbox's own catalog, so an update teaches it what
Autopilot can do from the next turn on.
