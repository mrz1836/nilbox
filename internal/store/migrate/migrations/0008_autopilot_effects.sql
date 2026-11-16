-- 0008_autopilot_effects: what Autopilot's local steps did to an item's triage, and which
-- routine the triage on this Mac came from.
--
-- The conventions are 0001_init's: DATETIME stamps the application writes in UTC. The
-- inbox owns the item_local_state columns, and Autopilot the autopilot_effects table.

-- == inbox ===============================================================================

-- set_aside_by is the routine that last set the item's dismissal or snooze, or brought it
-- back from one (a routine's Restore), and pinned_by the routine that pinned it; each is
-- NULL when the user did, or nothing did. They are two because a pin is independent of a
-- snooze or a dismissal on the same row. Every triage path of the user's own clears the
-- one it changes, so the window never says a routine did what the user did last. A
-- routine deleted for good leaves no name behind.
ALTER TABLE item_local_state ADD COLUMN set_aside_by INTEGER REFERENCES autopilot_routines (id) ON DELETE SET NULL;
ALTER TABLE item_local_state ADD COLUMN pinned_by INTEGER REFERENCES autopilot_routines (id) ON DELETE SET NULL;

-- == autopilot ===========================================================================

-- One row per local step a match ran that changed its item's triage: the match and the
-- step, its routine, the item (NULL once it is gone), and the action (pin, snooze,
-- dismiss, or restore, checked in Go). state_before is the item's local state before the
-- step and state_after the state it left, both as the inbox writes them (JSON): what Undo
-- puts back, only while the item is still as the step left it. The row is written in the
-- same transaction as the triage it records, before the triage's own write, so the two
-- are stored together or not at all. undone_at is when Undo put the item back, and
-- reversed_at when the user's own triage took the step back (a restore of what it set
-- aside, an unpin of what it pinned, a snooze over it), which also leaves the routine hands
-- off the item.
CREATE TABLE autopilot_effects (
    id           INTEGER  PRIMARY KEY,
    match_id     INTEGER  NOT NULL REFERENCES autopilot_matches (id) ON DELETE CASCADE,
    step         INTEGER  NOT NULL DEFAULT 0 CHECK (step >= 0),
    routine_id   INTEGER  NOT NULL REFERENCES autopilot_routines (id),
    item_id      INTEGER  REFERENCES items (id) ON DELETE SET NULL,
    action       TEXT     NOT NULL CHECK (action <> ''),
    state_before TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(state_before)),
    state_after  TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(state_after)),
    applied_at   DATETIME NOT NULL,
    undone_at    DATETIME,
    reversed_at  DATETIME,
    UNIQUE (match_id, step)
);

CREATE INDEX autopilot_effects_item ON autopilot_effects (item_id);

CREATE INDEX autopilot_effects_applied ON autopilot_effects (applied_at);
