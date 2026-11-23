# Autopilot's notifications and the menu bar

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Notices, the menu bar, and the window's close](../../architecture/autopilot/notices.md)

Autopilot has five kinds of notification, each with a banner switch of its own in **Settings →
Notifications**, all on by default, and the **Autopilot** type on the
[Notifications page](../notifications-page.md), both shown while Autopilot (beta) is on:

| Kind | Switch | When |
|---|---|---|
| Autopilot alert | **Routine alerts** | A routine's **Notify me** or **Tell me** step runs; headed by the routine's name. A schedule's digest is one alert for what it matched at its time, about no one item, which opens the routine |
| Autopilot problem | **Autopilot problems** | A routine can't run: its search fails, it won't load, or this Mac lacks something it needs. You hear of each problem once, as it starts |
| Needs your OK | **Needs your OK** | A routine's match waits for you: Ask first, a hold, no clone, a dirty worktree, a catch-up, a GitHub action's warnings, an outside contributor's item, a first pass's backlog; its banner opens the match's card |
| Agent finished | **Agents finishing** | A routine's coding agent in the background ends, done or failed, with what it cost; never one you stopped |
| Checks after a fix | **Checks after a fix** | A **Wait for the checks** step comes to its verdict on an agent's push: fixed, still failing, unknown, or overtaken ([closing the loop](wait-for-the-checks.md)) |

A routine can turn its own banners on or off, whatever the switches say. More than three at once
arrive as one banner that sums them up (*4 new from Autopilot*). Clicking an alert about an item
opens the item, as an inbox arrival does, and the entry is marked read when the item's sheet
opens; clicking one about no item, as a problem is, opens the Autopilot tab at its routine. When
what it names is gone, as after **Clear history**, a toast says so.

While Autopilot (beta) is on and a routine exists, the [menu bar](../menu-bar.md) menu has an
**Autopilot** group after your lists: each agent working, *1 needs your OK* (*3 need your OK*),
which opens Activity, and **Pause Autopilot** as a checkbox row, the same setting as the tab's
button and Settings'. The number by the icon stays the inbox's. While a routine is on (and Autopilot
is) or an agent works, closing the window only hides it, even with the menu bar icon off, so
Autopilot carries on; the Dock icon brings it back.
