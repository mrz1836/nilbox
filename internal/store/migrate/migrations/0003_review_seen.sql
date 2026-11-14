-- 0003_review_seen: what the review sheet showed when the user last opened a pull request.
--
-- The conventions are 0001_init's: DATETIME stamps the application writes in UTC.

-- == review ==============================================================================

-- review_diffs' context now holds the pull request's conversation too (its comments, its
-- reviews with their line comments, and its events), its long texts trimmed and marked so
-- one pull request's context stays within 1 MiB. seen_at is GitHub's time of the newest
-- activity in the read the sheet showed when it last opened the pull request, which tells
-- the next visit what is new; NULL before the sheet opened it, or when the read didn't say.
-- Only the sheet's opening moves it: a read leaves it as it was.
ALTER TABLE review_diffs ADD COLUMN seen_at DATETIME;
