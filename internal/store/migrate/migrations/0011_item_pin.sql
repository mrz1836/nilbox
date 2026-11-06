-- 0011_item_pin: pinned items.
--
-- A pinned item stays at the top of every list it is in, the most recently pinned first,
-- until it is unpinned. A pin is kept apart from a snooze or a dismissal: snoozing or
-- dismissing a pinned item sets it aside as usual, and it comes back still pinned, so
-- undoing either leaves the pin as it was. Restoring an item keeps its row while it is
-- pinned. The state stays on this machine; nothing is written to GitHub.
ALTER TABLE item_local_state ADD COLUMN pinned_at DATETIME;
