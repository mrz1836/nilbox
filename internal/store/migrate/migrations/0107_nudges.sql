-- 0107_nudges: when the user last nudged the author of a pull request they asked for
-- changes on.
--
-- The conventions are 0100_settings's. The inbox owns every column here.

-- == items ===============================================================================

-- A pull request whose author the user asked for changes, and that has gone quiet since,
-- offers to nudge its author: one comment on its conversation, posted from the inbox. Its
-- card measures how long it has been quiet from the later of its last activity and the
-- nudge, so a nudge just posted is never offered again until the pull request goes quiet
-- once more.
--
-- nudged_at is when GitHub says the user's last nudge from the inbox was posted, NULL
-- before one was. Only the inbox's Nudge writes it, and nothing clears it: the snapshot
-- shows it only while it is later than the user's standing verdict (viewer_review_at), so
-- a new round of review starts with none.
ALTER TABLE items ADD COLUMN nudged_at DATETIME;
