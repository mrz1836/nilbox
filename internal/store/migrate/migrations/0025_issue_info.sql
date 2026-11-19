-- 0025_issue_info: how an issue is planned, beyond the projects it is in.
--
-- The conventions are 0001_init's. The inbox owns every column here.

-- == item_details ========================================================================

-- An issue card's details show what GitHub's sidebar shows of how the issue is planned:
-- its type, its values in its organization's issue fields, its milestone, the issue it is
-- a sub-issue of, and how many open issues block it and it blocks. The details read asks
-- for them at no extra point.
--
-- issue_info is a JSON object of them:
--   "type" and "typeColor": the type's name, such as "Task", and its color as "#rrggbb",
--     both '' when the issue has no type;
--   "fields": an array of {"name", "value", "color"}, one per issue field with a value, in
--     GitHub's order, color a single-select option's as "#rrggbb" and '' otherwise;
--   "milestone": {"title", "url", "dueOn" (its due day as "2006-01-02", '' when it has
--     none), "closed", "progress" (the percentage of its items closed)}, or null;
--   "parent": {"repo" ("owner/name"), "number", "title", "url"}, or null;
--   "blockedBy" and "blocking": the counts of open issues.
-- It stays '{}' for a pull request, and for a row stored before this until the next read
-- of its details, which a whole refresh makes of every listed item.
ALTER TABLE item_details ADD COLUMN issue_info TEXT NOT NULL DEFAULT '{}'
    CHECK (json_valid(issue_info) AND json_type(issue_info) = 'object');
