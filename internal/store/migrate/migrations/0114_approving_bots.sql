-- 0114_approving_bots: the bots whose latest review approves a pull request.
--
-- The conventions are 0100_settings's. The inbox owns every column here.

-- == item_details ========================================================================

-- A pull request's card says when an auto-merge bot the user listed approved it, so it
-- merges by itself once its checks pass. The details read asks for each latest review's
-- author with its type at no extra point, and the match against the user's list happens as
-- the card is drawn, so a change to the list asks GitHub nothing.
--
-- approving_bots is a JSON array of the logins of the bots (GitHub Apps) whose latest
-- review approves the pull request, lower-cased, each once, in GitHub's order, such as
-- ["acme-org-automerge"]. It stays '[]' for an issue, a pull request no bot approved, and a
-- row stored before this until the next read of its details, which a whole refresh makes of
-- every listed item.
ALTER TABLE item_details ADD COLUMN approving_bots TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(approving_bots) AND json_type(approving_bots) = 'array');
