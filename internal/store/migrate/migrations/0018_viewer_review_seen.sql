-- 0018_viewer_review_seen: when the user's verdict was submitted, and the newest one seen.
--
-- The conventions are 0001_init's. The inbox owns every column here.

-- == items ===============================================================================

-- A read of one pull request (a card's refresh, the read after an agent session stops
-- working, the review sheet's) now reads the user's own verdict too, so a review an agent
-- left, or one left in a browser, reaches the card before the next whole refresh, and the
-- inbox tells it once as the user's review.
--
-- viewer_review_at is when GitHub says the verdict in viewer_review_state was submitted,
-- NULL with none. It moves with viewer_review_state and viewer_review_commit, and is cleared
-- with them, so a read can tell the verdict it holds from a newer one, and an older read
-- never writes over a newer verdict.
--
-- viewer_review_seen_at is when the newest verdict the inbox has seen of the user's on the
-- pull request was submitted, NULL before any. It only ever rises, and nothing clears it, so
-- a review is told at most once, whichever read sees it first.
ALTER TABLE items ADD COLUMN viewer_review_at DATETIME;
ALTER TABLE items ADD COLUMN viewer_review_seen_at DATETIME;
