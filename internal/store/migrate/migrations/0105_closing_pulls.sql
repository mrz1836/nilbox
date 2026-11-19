-- 0105_closing_pulls: the pull requests that close an issue once they merge.
--
-- The conventions are 0100_settings's. The inbox owns every column here.

-- == item_details ========================================================================

-- An issue card says when a pull request is on its way to closing it, and a pull request's
-- card names the listed issues it closes, from GitHub's closing references: a pull request
-- whose description says it closes the issue, or one someone linked to it by hand. The
-- details read asks for them on the issue's side at no extra point.
--
-- closing_pulls is a JSON array of the first five, open or merged, in GitHub's order, each
-- {"repo" ("owner/name"), "number", "title", "url", "merged", "draft", "authorLogin",
-- "authorType" (User, Bot, ...), "authorAssociation" (OWNER, MEMBER, ...), "fork" (its
-- branch is in a fork)}: who opened it decides, as the card is drawn, whether the card
-- trusts it. It stays '[]' for a pull request, an issue no pull request closes, and a row
-- stored before this until the next read of its details, which a whole refresh makes of
-- every listed item.
ALTER TABLE item_details ADD COLUMN closing_pulls TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(closing_pulls) AND json_type(closing_pulls) = 'array');
