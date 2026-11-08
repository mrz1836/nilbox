-- 0002_item_projects: the GitHub projects an issue is in.
--
-- projects is a JSON array of {"title", "url", "status", "statusColor"} objects, one per
-- open project the issue is in, in GitHub's order: status is the issue's option in the
-- project's Status field ('' when it has none) and statusColor that option's color as
-- "#rrggbb" or ''. It stays '[]' for a pull request, and for an issue read while the
-- token lacks the read:project scope. Like the other details, it is replaced whole on
-- every refresh that reads them.
ALTER TABLE item_details
    ADD COLUMN projects TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(projects) AND json_type(projects) = 'array');
