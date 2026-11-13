-- 0018_gh_threads: the user's GitHub notifications, which GitHub's REST API calls threads, as
-- the web inbox lists them (read and unread, not done), mirrored per account, so the window
-- counts them and tags the cards they are about without asking GitHub.
--
-- One row per account and thread, as the last pass that listed it read it: reason, unread,
-- updated_at, last_read_at (NULL when never read), subject_type, and subject_title in
-- GitHub's words. repo_owner and repo_name are the thread's repository, '' when GitHub named
-- none that could be one; item_kind and number the pull request or issue its subject is, ''
-- and 0 for any other subject (a release, a discussion, a check suite, a commit).
-- latest_comment_url is the subject's latest comment's API URL as GitHub gave it ('' for
-- none), kept for the check of who acted on a thread after its item was finished, and never
-- shown or opened.
--
-- subject_state is the item's state as the last state check read it ('' until one did;
-- OPEN, CLOSED, or MERGED), subject_closed_at when it was merged or closed, and
-- state_checked_at when it was checked, stamped too when GitHub couldn't find it.
-- after_moment is who acted on the thread after its item was finished ('', bot, or person),
-- and after_moment_checked_at when that was checked. A listing keeps these as they were:
-- only their checks write them. seen_at is when a pass last listed the thread.
--
-- The ghthreads package deletes an account's rows a walk of every page didn't list (done on
-- GitHub, or past the newest 1,000), and the rows of other accounts unseen for 30 days.
CREATE TABLE gh_threads (
    account_login           TEXT     NOT NULL COLLATE NOCASE CHECK (account_login <> ''),
    thread_id               INTEGER  NOT NULL CHECK (thread_id > 0),
    reason                  TEXT     NOT NULL DEFAULT '',
    unread                  BOOLEAN  NOT NULL DEFAULT 0 CHECK (unread IN (0, 1)),
    updated_at              DATETIME NOT NULL,
    last_read_at            DATETIME,
    subject_type            TEXT     NOT NULL DEFAULT '',
    subject_title           TEXT     NOT NULL DEFAULT '',
    repo_owner              TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    repo_name               TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    item_kind               TEXT     NOT NULL DEFAULT '' CHECK (item_kind IN ('', 'pr', 'issue')),
    number                  INTEGER  NOT NULL DEFAULT 0 CHECK (number >= 0),
    subject_state           TEXT     NOT NULL DEFAULT '' CHECK (subject_state IN ('', 'OPEN', 'CLOSED', 'MERGED')),
    subject_closed_at       DATETIME,
    state_checked_at        DATETIME,
    after_moment            TEXT     NOT NULL DEFAULT '' CHECK (after_moment IN ('', 'bot', 'person')),
    after_moment_checked_at DATETIME,
    latest_comment_url      TEXT     NOT NULL DEFAULT '',
    seen_at                 DATETIME NOT NULL,
    PRIMARY KEY (account_login, thread_id),
    CHECK ((item_kind = '') = (number = 0)),
    CHECK (item_kind = '' OR (repo_owner <> '' AND repo_name <> ''))
);

CREATE INDEX gh_threads_item ON gh_threads (account_login, repo_owner, repo_name, number);
