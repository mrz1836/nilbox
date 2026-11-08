-- 0003_item_sub_issues: how many sub-issues an issue has, and how many are done.
--
-- sub_issues_total counts an issue's sub-issues and sub_issues_completed those closed as
-- done, never more than the total. Both stay 0 for a pull request and for an issue without
-- sub-issues. Like the other details, they are replaced whole on every refresh that reads
-- them. (An issue's values in its projects' other fields, such as Priority or Size, are kept
-- in the projects column, as a "fields" array on each project.)
ALTER TABLE item_details
    ADD COLUMN sub_issues_total INTEGER NOT NULL DEFAULT 0 CHECK (sub_issues_total >= 0);

ALTER TABLE item_details
    ADD COLUMN sub_issues_completed INTEGER NOT NULL DEFAULT 0
        CHECK (sub_issues_completed >= 0 AND sub_issues_completed <= sub_issues_total);
