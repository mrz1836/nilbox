-- 0115_review_texts: the latest reviews of a pull request that say something.
--
-- The conventions are 0100_settings's. The inbox owns every column here.

-- == item_details ========================================================================

-- A card's comment count counts what people wrote, as the review sheet's conversation does:
-- GitHub's count of a pull request's comments (comments) leaves out a review's own text,
-- such as a bot's note as it approves. The details read asks for each latest review's text
-- at no extra point and counts the ones that say something.
--
-- review_texts is how many of a pull request's latest reviews, one per reviewer, have text,
-- a pending review left out. The card adds it to comments as the snapshot is built, so
-- comments stays GitHub's own count, which a snooze's wake compares. It stays 0 for an issue,
-- a pull request no review says anything on, and a row stored before this until the next
-- read of its details, which a whole refresh makes of every listed item.
ALTER TABLE item_details ADD COLUMN review_texts INTEGER NOT NULL DEFAULT 0 CHECK (review_texts >= 0);
