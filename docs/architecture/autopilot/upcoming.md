# What runs next (`upcoming.go`)

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/autopilot`](../../../internal/autopilot) · Guide: [Autopilot](../../guide/autopilot/README.md)

As a cycle decides each routine on each item it watches (`evaluateItem`), a decision whose trigger
is pending goes to `noteUpcoming`: the routine acts (not Dry run), has no record of the pending
`FireKey` but one that reopens (`reopensRow`), so a commit it ran on whose checks run again says
nothing, and its `Plan.ConditionsWhenDue` is a sure yes; the item is one it doesn't leave alone,
which `evaluateItem` checked first. Each becomes an `Upcoming` (the routine, its mode and whether it
waits for an OK, `waitsForOK`: Ask first with a step `asksFirst` names, its trigger, `When` and
`At`, and its steps as a match's show before they start, `plannedSteps`, once a cycle). A pass that
decided every routine replaces the engine's map by item whole (`keepUpcoming`, under `Engine.mu`);
turning Autopilot off empties it. So `Service.Upcoming` reads Autopilot's settings, then looks in a
map: no other read, no GitHub, and the `autopilot:updated` every cycle sends keeps the window's copy
current. `LeaveAlone(routine, item)` inserts the routine's hands-off row (`whyLeftAlone`), which
Activity's Left alone line and `LetActAgain` already handle, forgets the item's entry at once
(`dropUpcoming`), and wakes the engine.
