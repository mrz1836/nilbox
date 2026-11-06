-- 0007_item_triage: snooze and dismiss.
--
-- An item can be dismissed (hidden from every list until it is restored) or snoozed
-- (hidden until snoozed_until passes, then back on its own), never both. The state stays
-- on this machine; nothing is written to GitHub. 0001 reserved done_at for a done marker
-- that nothing ever wrote, so it becomes dismissed_at. A row whose snooze has passed is
-- kept, so the inbox can say the item is back from a snooze; restoring an item deletes
-- its row.
ALTER TABLE item_local_state RENAME COLUMN done_at TO dismissed_at;

ALTER TABLE item_local_state ADD COLUMN snoozed_until DATETIME
    CHECK (snoozed_until IS NULL OR dismissed_at IS NULL);
