-- 0008_item_auto_merge: a pull request's auto-merge, as an inbox card shows it.
--
-- auto_merge_method is the method GitHub merges the pull request with once its
-- requirements are met ('MERGE', 'SQUASH', or 'REBASE'), and '' while auto-merge is off;
-- auto_merge_at is when it was turned on, NULL while it is off. Both stay '' and NULL for
-- an issue. Like the other details, they are replaced whole on every refresh that reads
-- them.
ALTER TABLE item_details ADD COLUMN auto_merge_method TEXT NOT NULL DEFAULT '';

ALTER TABLE item_details ADD COLUMN auto_merge_at DATETIME;
