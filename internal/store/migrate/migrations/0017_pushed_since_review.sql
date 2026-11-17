-- 0017_pushed_since_review: pull requests pushed to since the user requested changes.
--
-- The conventions are 0001_init's. The inbox owns every column here.

-- == items ===============================================================================

-- Submitting a review takes the user's review request off a pull request, so one its author
-- pushed to without asking again left the inbox for good. The inbox's fourth search reads
-- the open pull requests the user reviewed and didn't open, each with the user's own standing
-- verdict (the latest review that approved or requested changes) and its head commit.
--
-- reason_reviewed is the raw match of that search, which lists nothing by itself, and
-- reason_pushed one listed in Review requested because the user's verdict requested changes
-- at a commit other than its head, both written by the refresh's upsert. A review the user
-- leaves from the inbox takes reason_pushed off as it takes reason_review_requested off,
-- stamped, until the pull request moves.
--
-- viewer_review_state is the user's verdict, APPROVED or CHANGES_REQUESTED, and
-- viewer_review_commit the commit it was left on, '' with none; the card says a pull request
-- was pushed to since from them, whichever list it is in.
ALTER TABLE items ADD COLUMN reason_reviewed BOOLEAN NOT NULL DEFAULT 0 CHECK (reason_reviewed IN (0, 1));
ALTER TABLE items ADD COLUMN reason_pushed BOOLEAN NOT NULL DEFAULT 0 CHECK (reason_pushed IN (0, 1));
ALTER TABLE items ADD COLUMN viewer_review_state TEXT NOT NULL DEFAULT ''
    CHECK (viewer_review_state IN ('', 'APPROVED', 'CHANGES_REQUESTED'));
ALTER TABLE items ADD COLUMN viewer_review_commit TEXT NOT NULL DEFAULT '';
