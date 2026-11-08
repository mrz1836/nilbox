-- 0004_item_edits: the edits the user made on GitHub from the inbox, and who each pull
-- request asks to review.
--
-- assign_left_at and review_left_at are set when the user takes themselves off an item's
-- assignees, or off a pull request's requested reviewers, from the inbox: each holds the
-- item's updated_at on GitHub as GitHub answered the edit. GitHub's search index can trail
-- an edit for a while, so a refresh whose search still lists the item there, with no
-- activity since (item_updated_at not after the stamp), keeps it out of that list; once
-- the item moves past the stamp, the search is believed again and the stamp is cleared.
-- Both are NULL otherwise.
ALTER TABLE items ADD COLUMN assign_left_at DATETIME;

ALTER TABLE items ADD COLUMN review_left_at DATETIME;

-- review_users and review_teams are who a pull request asks to review: JSON arrays of the
-- logins of the people (and bots) and the slugs of the teams, in GitHub's order. Both stay
-- '[]' for an issue. Like the other details, they are replaced whole on every refresh that
-- reads them, and an edit to the review requests rewrites them with GitHub's answer.
ALTER TABLE item_details
    ADD COLUMN review_users TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(review_users) AND json_type(review_users) = 'array');

ALTER TABLE item_details
    ADD COLUMN review_teams TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(review_teams) AND json_type(review_teams) = 'array');
