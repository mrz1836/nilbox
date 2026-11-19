-- 0101_inbox: the inbox, the user's GitHub notifications, and the app's own.
--
-- The conventions are 0100_settings's.

-- == inbox ===============================================================================

-- The signed-in GitHub account.
CREATE TABLE accounts (
    id         INTEGER  PRIMARY KEY,
    login      TEXT     NOT NULL COLLATE NOCASE UNIQUE,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Repositories seen in the inbox, with the matched local clone and what GitHub says about
-- each one. Pinned and muted repositories and orgs are the user's settings, kept in the
-- prefs (repos.pinned and repos.muted), since an org, or a clone the inbox has never seen,
-- has no row here.
--
-- Every refresh rewrites GitHub's side from the inbox query, which carries it on each item
-- at no extra cost. fetched_at is NULL until a refresh has described the repository.
-- visibility is PUBLIC, PRIVATE, or INTERNAL; archived_at and pushed_at are NULL when
-- GitHub has none; parent is the "owner/name" a fork was made from; viewer_permission is
-- the viewer's access (ADMIN, MAINTAIN, WRITE, TRIAGE, or READ) and viewer_merge_method the
-- merge method GitHub offers the viewer first; language_color is "#rrggbb" or ''. An
-- archived repository's items are never listed.
CREATE TABLE repos (
    id                     INTEGER  PRIMARY KEY,
    owner                  TEXT     NOT NULL COLLATE NOCASE,
    name                   TEXT     NOT NULL COLLATE NOCASE,
    local_path             TEXT     NOT NULL DEFAULT '',
    created_at             DATETIME NOT NULL,
    updated_at             DATETIME NOT NULL,
    node_id                TEXT     NOT NULL DEFAULT '',
    description            TEXT     NOT NULL DEFAULT '',
    visibility             TEXT     NOT NULL DEFAULT '',
    is_archived            BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_archived IN (0, 1)),
    archived_at            DATETIME,
    is_disabled            BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_disabled IN (0, 1)),
    is_locked              BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_locked IN (0, 1)),
    lock_reason            TEXT     NOT NULL DEFAULT '',
    is_fork                BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_fork IN (0, 1)),
    parent                 TEXT     NOT NULL DEFAULT '',
    default_branch         TEXT     NOT NULL DEFAULT '',
    viewer_permission      TEXT     NOT NULL DEFAULT '',
    viewer_merge_method    TEXT     NOT NULL DEFAULT '',
    merge_commit_allowed   BOOLEAN  NOT NULL DEFAULT 0 CHECK (merge_commit_allowed IN (0, 1)),
    squash_merge_allowed   BOOLEAN  NOT NULL DEFAULT 0 CHECK (squash_merge_allowed IN (0, 1)),
    rebase_merge_allowed   BOOLEAN  NOT NULL DEFAULT 0 CHECK (rebase_merge_allowed IN (0, 1)),
    auto_merge_allowed     BOOLEAN  NOT NULL DEFAULT 0 CHECK (auto_merge_allowed IN (0, 1)),
    delete_branch_on_merge BOOLEAN  NOT NULL DEFAULT 0 CHECK (delete_branch_on_merge IN (0, 1)),
    allow_update_branch    BOOLEAN  NOT NULL DEFAULT 0 CHECK (allow_update_branch IN (0, 1)),
    language               TEXT     NOT NULL DEFAULT '',
    language_color         TEXT     NOT NULL DEFAULT '',
    pushed_at              DATETIME,
    fetched_at             DATETIME,
    UNIQUE (owner, name)
);

-- The unified pull request and issue cache. The reason_* flags record which inbox lists
-- returned the item on the latest refresh; author_association and is_cross_repository
-- feed the outside-contributor trust rule.
--
-- author_type is the GraphQL type GitHub reports for the item's author: 'User' for a
-- person, 'Bot' for a GitHub App or bot (Dependabot, Renovate, GitHub Actions), and
-- others such as 'Mannequin'. It is '' until a refresh has read it. The trust rule only
-- treats an author as a bot when GitHub says so here, so a person whose login resembles
-- a bot's can never borrow its trust.
--
-- The inbox's cards show each author's picture. A person's is downloaded by login, but the
-- avatar host answers a bot's login with a generated pattern rather than its app's picture,
-- so the inbox's search asks GitHub for a bot author's picture URL too, at no extra point.
-- author_avatar_url is that URL, on the avatar host, as GitHub gave it, and '' for a person,
-- for any author that is not a bot, and until a read has asked for it. The inbox's refresh
-- and an item's own read write it; a routine's watched read leaves it as stored.
--
-- assign_left_at and review_left_at are set when the user takes themselves off an item's
-- assignees, or off a pull request's requested reviewers, from the inbox: each holds the
-- item's updated_at on GitHub as GitHub answered the edit. GitHub's search index can trail
-- an edit for a while, so a refresh whose search still lists the item there, with no
-- activity since (item_updated_at not after the stamp), keeps it out of that list; once
-- the item moves past the stamp, the search is believed again and the stamp is cleared.
-- Both are NULL otherwise.
--
-- state is the item's state as the last read that carries it said: OPEN, CLOSED, or MERGED
-- (a pull request's only), '' before a read said; closed_at is when it was merged or
-- closed, NULL while it is open or when the read didn't say. The inbox query's searches,
-- an item's own read, the checks follower's, a routine's search, and the states other
-- services read all write them.
--
-- review_requested_at and assigned_at are when the inbox first saw the item in Review
-- requested and in Assigned, while it is there; each is NULL once the item leaves that
-- list, so the next arrival stamps it again.
--
-- Submitting a review takes the user's review request off a pull request, so one its author
-- pushed to without asking again would leave the inbox for good. The inbox's fourth search
-- reads the open pull requests the user reviewed and didn't open, each with the user's own
-- standing verdict (the latest review that approved or requested changes) and its head
-- commit. reason_reviewed is the raw match of that search, which lists nothing by itself,
-- and reason_pushed one listed in Review requested because the user's verdict requested
-- changes at a commit other than its head, both written by the refresh's upsert. A review
-- the user leaves from the inbox takes reason_pushed off as it takes
-- reason_review_requested off, stamped, until the pull request moves.
--
-- viewer_review_state is the user's verdict, APPROVED or CHANGES_REQUESTED, and
-- viewer_review_commit the commit it was left on, '' with none; the card says a pull request
-- was pushed to since from them, whichever list it is in. A read of one pull request (a
-- card's refresh, the read after an agent session stops working, the review sheet's) reads
-- the verdict too, so a review an agent left, or one left in a browser, reaches the card
-- before the next whole refresh, and the inbox tells it once as the user's review.
-- viewer_review_at is when GitHub says the verdict was submitted, NULL with none. It moves
-- with viewer_review_state and viewer_review_commit, and is cleared with them, so a read can
-- tell the verdict it holds from a newer one, and an older read never writes over a newer
-- verdict. viewer_review_seen_at is when the newest verdict the inbox has seen of the
-- user's on the pull request was submitted, NULL before any. It only ever rises, and nothing
-- clears it, so a review is told at most once, whichever read sees it first.
--
-- The inbox's cards show a dot on an item with activity on GitHub since the user last
-- opened it. seen_activity_at is the activity the user last saw, compared with
-- item_updated_at: the item is unread while it is NULL (never seen) or item_updated_at is
-- later. Only the window's marks move it (the inbox's MarkSeen, to the later of the item's
-- activity and the time of the mark, and MarkUnseen, to NULL), and a new row's insert sets
-- it: a whole refresh stores NULL, except on an account's first load, which stores the
-- item's activity, as a routine's search always does. No read rewrites it.
CREATE TABLE items (
    id                      INTEGER  PRIMARY KEY,
    account_id              INTEGER  NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    repo_id                 INTEGER  NOT NULL REFERENCES repos (id) ON DELETE CASCADE,
    kind                    TEXT     NOT NULL CHECK (kind IN ('pr', 'issue')),
    number                  INTEGER  NOT NULL CHECK (number > 0),
    node_id                 TEXT     NOT NULL DEFAULT '',
    title                   TEXT     NOT NULL DEFAULT '',
    url                     TEXT     NOT NULL DEFAULT '',
    author_login            TEXT     NOT NULL DEFAULT '',
    author_association      TEXT     NOT NULL DEFAULT '',
    is_cross_repository     BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_cross_repository IN (0, 1)),
    is_draft                BOOLEAN  NOT NULL DEFAULT 0 CHECK (is_draft IN (0, 1)),
    head_ref                TEXT     NOT NULL DEFAULT '',
    item_created_at         DATETIME NOT NULL,
    item_updated_at         DATETIME NOT NULL,
    reason_review_requested BOOLEAN  NOT NULL DEFAULT 0 CHECK (reason_review_requested IN (0, 1)),
    reason_authored         BOOLEAN  NOT NULL DEFAULT 0 CHECK (reason_authored IN (0, 1)),
    reason_assigned         BOOLEAN  NOT NULL DEFAULT 0 CHECK (reason_assigned IN (0, 1)),
    fetched_at              DATETIME NOT NULL,
    author_type             TEXT     NOT NULL DEFAULT '',
    assign_left_at          DATETIME,
    review_left_at          DATETIME,
    state                   TEXT     NOT NULL DEFAULT '',
    closed_at               DATETIME,
    review_requested_at     DATETIME,
    assigned_at             DATETIME,
    reason_reviewed         BOOLEAN  NOT NULL DEFAULT 0 CHECK (reason_reviewed IN (0, 1)),
    reason_pushed           BOOLEAN  NOT NULL DEFAULT 0 CHECK (reason_pushed IN (0, 1)),
    viewer_review_state     TEXT     NOT NULL DEFAULT ''
        CHECK (viewer_review_state IN ('', 'APPROVED', 'CHANGES_REQUESTED')),
    viewer_review_commit    TEXT     NOT NULL DEFAULT '',
    viewer_review_at        DATETIME,
    viewer_review_seen_at   DATETIME,
    author_avatar_url       TEXT     NOT NULL DEFAULT '',
    seen_activity_at        DATETIME,
    UNIQUE (account_id, repo_id, kind, number)
);

CREATE INDEX items_repo_id ON items (repo_id);
CREATE INDEX items_account_updated ON items (account_id, item_updated_at);

-- The details an inbox card shows about a pull request or issue, replaced whole on every
-- refresh that reads them. labels is a JSON array of {"name", "color"} objects, color as
-- "#rrggbb" or "". The pull request columns stay 0, '', '[]', or NULL for an issue.
-- checks_state is GitHub's overall check result ('' when the head commit has no checks);
-- the checks_* counts add up the check runs and commit statuses by outcome. reviewers is
-- everyone asked to review or who has, the author excluded; reviewed is how many of them
-- have and are not asked again. head_oid is a pull request's head commit SHA; a new one
-- means it was pushed, force-pushes included.
--
-- projects is a JSON array of {"title", "url", "status", "statusColor"} objects, one per
-- open project the issue is in, in GitHub's order: status is the issue's option in the
-- project's Status field ('' when it has none) and statusColor that option's color as
-- "#rrggbb" or ''; the issue's values in the project's other fields, such as Priority or
-- Size, are a "fields" array on each project. It stays '[]' for a pull request, and for an
-- issue read while the token lacks the read:project scope.
--
-- sub_issues_total counts an issue's sub-issues and sub_issues_completed those closed as
-- done, never more than the total. Both stay 0 for a pull request and for an issue without
-- sub-issues.
--
-- review_users and review_teams are who a pull request asks to review: JSON arrays of the
-- logins of the people (and bots) and the slugs of the teams, in GitHub's order. An edit to
-- the review requests rewrites them with GitHub's answer.
--
-- auto_merge_method is the method GitHub merges the pull request with once its
-- requirements are met ('MERGE', 'SQUASH', or 'REBASE'), and '' while auto-merge is off;
-- auto_merge_at is when it was turned on, NULL while it is off.
--
-- base_ref is the branch a pull request merges into, and merge_state GitHub's
-- mergeStateStatus for it (BEHIND, BLOCKED, CLEAN, DIRTY, HAS_HOOKS, UNKNOWN, UNSTABLE);
-- both '' for an issue and before a read brought them.
--
-- GitHub holds a pull request's GitHub Actions runs until a maintainer approves them, as a
-- fork's runs are: check suites that ended ACTION_REQUIRED with a workflow run. Its API
-- takes the approval only for a fork pull request's own run (the pull_request event on a
-- pull request from another repository) or one the Actions bot queued, and refuses any
-- other, as a run a review started in the base repository, which waits for approval on
-- GitHub. checks_awaiting counts the held runs of the head commit the user can approve from
-- the app, and checks_awaiting_on_github the others. GitHub's rollup leaves both out, so
-- checks_state can read SUCCESS while they wait.
--
-- A card counts a held run toward where its pull request stands only when it should: a run
-- GitHub approves only on its site doesn't count unless GitHub reports the merge blocked,
-- and a run of a workflow the user ignores (Settings, checks.ignoredWorkflows) never does.
-- The user can change that list at any time, and the card counts again at once, from what
-- is stored, without asking GitHub, so the details keep each held run: checks_held is a
-- JSON array of the runs the two counts count, each {"workflow": its workflow's name, ""
-- when GitHub gave none, "onGitHub": whether GitHub approves it only on its site}, in
-- GitHub's order.
CREATE TABLE item_details (
    item_id                   INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    labels                    TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(labels) AND json_type(labels) = 'array'),
    comments                  INTEGER  NOT NULL DEFAULT 0 CHECK (comments >= 0),
    additions                 INTEGER  NOT NULL DEFAULT 0 CHECK (additions >= 0),
    deletions                 INTEGER  NOT NULL DEFAULT 0 CHECK (deletions >= 0),
    changed_files             INTEGER  NOT NULL DEFAULT 0 CHECK (changed_files >= 0),
    commits                   INTEGER  NOT NULL DEFAULT 0 CHECK (commits >= 0),
    mergeable                 TEXT     NOT NULL DEFAULT '',
    checks_state              TEXT     NOT NULL DEFAULT '',
    checks_passed             INTEGER  NOT NULL DEFAULT 0 CHECK (checks_passed >= 0),
    checks_failed             INTEGER  NOT NULL DEFAULT 0 CHECK (checks_failed >= 0),
    checks_pending            INTEGER  NOT NULL DEFAULT 0 CHECK (checks_pending >= 0),
    checks_skipped            INTEGER  NOT NULL DEFAULT 0 CHECK (checks_skipped >= 0),
    review_decision           TEXT     NOT NULL DEFAULT '',
    reviewers                 INTEGER  NOT NULL DEFAULT 0 CHECK (reviewers >= 0),
    reviewed                  INTEGER  NOT NULL DEFAULT 0 CHECK (reviewed >= 0 AND reviewed <= reviewers),
    approvals                 INTEGER  NOT NULL DEFAULT 0 CHECK (approvals >= 0),
    changes_requested         INTEGER  NOT NULL DEFAULT 0 CHECK (changes_requested >= 0),
    fetched_at                DATETIME NOT NULL,
    head_oid                  TEXT     NOT NULL DEFAULT '',
    projects                  TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(projects) AND json_type(projects) = 'array'),
    sub_issues_total          INTEGER  NOT NULL DEFAULT 0 CHECK (sub_issues_total >= 0),
    sub_issues_completed      INTEGER  NOT NULL DEFAULT 0
        CHECK (sub_issues_completed >= 0 AND sub_issues_completed <= sub_issues_total),
    review_users              TEXT     NOT NULL DEFAULT '[]'
        CHECK (json_valid(review_users) AND json_type(review_users) = 'array'),
    review_teams              TEXT     NOT NULL DEFAULT '[]'
        CHECK (json_valid(review_teams) AND json_type(review_teams) = 'array'),
    auto_merge_method         TEXT     NOT NULL DEFAULT '',
    auto_merge_at             DATETIME,
    checks_awaiting           INTEGER  NOT NULL DEFAULT 0 CHECK (checks_awaiting >= 0),
    base_ref                  TEXT     NOT NULL DEFAULT '',
    merge_state               TEXT     NOT NULL DEFAULT '',
    checks_awaiting_on_github INTEGER  NOT NULL DEFAULT 0 CHECK (checks_awaiting_on_github >= 0),
    checks_held               TEXT     NOT NULL DEFAULT '[]'
        CHECK (json_valid(checks_held) AND json_type(checks_held) = 'array')
);

-- The files checked pull requests change.
--
-- A pull request from outside the repository (and a collaborator's, unless the user says
-- otherwise) has its changed files listed whenever its head commit moves, so the inbox can
-- point out the sensitive ones: workflows, agent configuration, build scripts, and the
-- like. Only paths are kept, never content. files is a JSON array of {"path", "previous",
-- "change"} objects: the path after the change, a renamed or copied file's path before it
-- ('' otherwise), and GitHub's word for the change. head_oid is the head commit the list
-- belongs to; total is how many files GitHub says the pull request changes, which can be
-- more than it lists (3,000 at most), so complete says whether files holds them all. A
-- check that failed keeps its message in error, with no files, and is tried again on the
-- next refresh. The row is replaced on each check and removed when the item leaves the
-- inbox.
CREATE TABLE item_files (
    item_id    INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    head_oid   TEXT     NOT NULL DEFAULT '',
    total      INTEGER  NOT NULL DEFAULT 0 CHECK (total >= 0),
    files      TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(files) AND json_type(files) = 'array'),
    complete   BOOLEAN  NOT NULL DEFAULT 0 CHECK (complete IN (0, 1)),
    error      TEXT     NOT NULL DEFAULT '',
    checked_at DATETIME NOT NULL
);

-- Local-only triage state for an item: snooze, dismiss, and pin. The state stays on this
-- machine; nothing is written to GitHub.
--
-- An item can be dismissed (hidden from every list until it is restored) or snoozed
-- (hidden until snoozed_until passes, then back on its own), never both. A row whose
-- snooze has passed is kept, so the inbox can say the item is back from a snooze;
-- restoring an item deletes its row, unless the item is pinned.
--
-- snooze_baseline is a JSON object of the item's activity when it was snoozed (its head,
-- commit and comment counts, and reviews), which each refresh compares with the item's
-- details under the user's wake rules (the snooze.* prefs). wake_reasons is a JSON object
-- of the new activity that ended a snooze early; it is NULL while a snooze runs and when
-- one ran its course.
--
-- A pinned item stays at the top of every list it is in, the most recently pinned first,
-- until it is unpinned. A pin is kept apart from a snooze or a dismissal: snoozing or
-- dismissing a pinned item sets it aside as usual, and it comes back still pinned, so
-- undoing either leaves the pin as it was.
--
-- set_aside_by is the Autopilot routine that last set the item's dismissal or snooze, or
-- brought it back from one (a routine's Restore), and pinned_by the routine that pinned it;
-- each is NULL when the user did, or nothing did. They are two because a pin is independent
-- of a snooze or a dismissal on the same row. Every triage path of the user's own clears the
-- one it changes, so the window never says a routine did what the user did last. A routine
-- deleted for good leaves no name behind.
CREATE TABLE item_local_state (
    item_id         INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    dismissed_at    DATETIME,
    updated_at      DATETIME NOT NULL,
    snoozed_until   DATETIME CHECK (snoozed_until IS NULL OR dismissed_at IS NULL),
    snooze_baseline TEXT     CHECK (snooze_baseline IS NULL OR (json_valid(snooze_baseline) AND json_type(snooze_baseline) = 'object')),
    wake_reasons    TEXT     CHECK (wake_reasons IS NULL OR (json_valid(wake_reasons) AND json_type(wake_reasons) = 'object')),
    pinned_at       DATETIME,
    set_aside_by    INTEGER  REFERENCES autopilot_routines (id) ON DELETE SET NULL,
    pinned_by       INTEGER  REFERENCES autopilot_routines (id) ON DELETE SET NULL
);

-- GitHub API rate-limit readings, one row per resource per refresh.
CREATE TABLE rate_snapshots (
    id          INTEGER  PRIMARY KEY,
    account_id  INTEGER  NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    resource    TEXT     NOT NULL,
    limit_total INTEGER  NOT NULL,
    remaining   INTEGER  NOT NULL,
    used        INTEGER  NOT NULL,
    cost        INTEGER  NOT NULL DEFAULT 0,
    reset_at    DATETIME NOT NULL,
    taken_at    DATETIME NOT NULL
);

CREATE INDEX rate_snapshots_resource_taken_at ON rate_snapshots (resource, taken_at);
CREATE INDEX rate_snapshots_account_id ON rate_snapshots (account_id);

-- One row per routine search and the item it returned. watch_key is the search's query, as
-- the routines' builder writes it in one canonical order, so identical searches share it.
-- follow says the checks follower reads the item while its checks run, as it reads a
-- listed pull request, since a routine on it acts on its checks. seen_at is when the
-- search last returned the item. stale is set when the search failed, or returned only its
-- 100 most recently updated items and this one wasn't among them: the row is kept, so the
-- item doesn't look as if it left the watch, until the search returns without it. A row
-- whose search no routine has any more is deleted.
CREATE TABLE item_watches (
    watch_key TEXT     NOT NULL CHECK (watch_key <> ''),
    item_id   INTEGER  NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    follow    BOOLEAN  NOT NULL DEFAULT 0 CHECK (follow IN (0, 1)),
    seen_at   DATETIME NOT NULL,
    stale     BOOLEAN  NOT NULL DEFAULT 0 CHECK (stale IN (0, 1)),
    PRIMARY KEY (watch_key, item_id)
);

CREATE INDEX item_watches_item_id ON item_watches (item_id);

-- A pull request's checks on one head commit, as read: checks is a JSON array of objects,
-- one per check run or commit status, with its name, workflow ('' for an app's own run and
-- for a status), app, status, conclusion, outcome (passed, failed, pending, or skipped),
-- URL, whether it is required (null when not read), and when it started and completed
-- (null while it runs). A name may repeat, once per event that ran it. complete says checks
-- holds every check of the commit; settled_at is the newest completion, once every check
-- has finished, NULL while one runs. The row is replaced on each read, and goes with its
-- item.
CREATE TABLE item_checks (
    item_id    INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    head_oid   TEXT     NOT NULL DEFAULT '',
    checks     TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(checks) AND json_type(checks) = 'array'),
    complete   BOOLEAN  NOT NULL DEFAULT 0 CHECK (complete IN (0, 1)),
    settled_at DATETIME,
    fetched_at DATETIME NOT NULL
);

-- What an agent run's commits changed on an item: one row per item and head commit the
-- run's commits changed files at, the head the run started from, whose worktree now holds
-- its commits, and the run's own last commit, which a push makes the pull request's head.
-- files lists the paths git named, up to the fence's cap (complete is 0 past it), as a JSON
-- array of strings. A launch on the item at either head reads them as part of its file
-- check, whoever opened the item, so an agent file a run wrote holds the next click there,
-- as an outside contributor's would. Two runs at one head add up their paths. run_id is
-- the run that noted them last, 0 for none. The rows go with their item.
CREATE TABLE item_agent_changes (
    item_id  INTEGER  NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    head_oid TEXT     NOT NULL CHECK (head_oid <> '' AND head_oid = lower(head_oid)),
    files    TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(files) AND json_type(files) = 'array'),
    total    INTEGER  NOT NULL DEFAULT 0 CHECK (total >= 0),
    complete BOOLEAN  NOT NULL DEFAULT 1 CHECK (complete IN (0, 1)),
    run_id   INTEGER  NOT NULL DEFAULT 0 CHECK (run_id >= 0),
    noted_at DATETIME NOT NULL,
    PRIMARY KEY (item_id, head_oid)
);

-- == ghthreads ===========================================================================

-- The user's GitHub notifications, which GitHub's REST API calls threads, as the web inbox
-- lists them (read and unread, not done), mirrored per account, so the window counts them
-- and tags the cards they are about without asking GitHub.
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

-- The moments the user finished an item in the app, which clear its notification threads
-- by the rules: a merge (merged), a close (pr_closed or issue_closed), a review that
-- approves or requests changes (reviewed), a dismissal (dismissed), or taking themselves
-- off it (unassigned), one row per account, item, and trigger, the latest kept. at is the
-- moment itself (the click, or the review's own time), and created_at when the app heard
-- of it, which the Undo hold runs from. A moment from GitHub's own state (a merge or a
-- close elsewhere) is never stored here: it is read from gh_threads. trigger is checked
-- in Go, so a later trigger needs no rebuild. The ghthreads package drops a moment 30 days
-- after it was heard of.
CREATE TABLE gh_thread_moments (
    account_login TEXT     NOT NULL COLLATE NOCASE CHECK (account_login <> ''),
    repo_owner    TEXT     NOT NULL COLLATE NOCASE CHECK (repo_owner <> ''),
    repo_name     TEXT     NOT NULL COLLATE NOCASE CHECK (repo_name <> ''),
    item_kind     TEXT     NOT NULL CHECK (item_kind IN ('pr', 'issue')),
    number        INTEGER  NOT NULL CHECK (number > 0),
    trigger       TEXT     NOT NULL CHECK (trigger <> '' AND trigger <> 'by_hand'),
    at            DATETIME NOT NULL,
    created_at    DATETIME NOT NULL,
    PRIMARY KEY (account_login, repo_owner, repo_name, item_kind, number, trigger)
);

-- The ledger of clearing: one row per clear the app decided on, written before anything is
-- sent, so every clear sent has a row, with the rule that chose it.
--
-- account_login and thread_id name the thread; repo_owner, repo_name, item_kind, number,
-- title, and reason are copied from it, so a row reads after its thread is gone. trigger
-- is the moment that chose it (by_hand for the user's own click), checked in Go, and action
-- what clearing does: done (DELETE) or read (PATCH). thread_updated_at is the thread's
-- updated_at the clear was decided on, which the thread must still have when it is sent.
--
-- state is queued until the clear settles: sent (GitHub took it, or no longer had the
-- thread), kept (the user undid it), failed (three attempts failed), or skipped (no
-- longer cleared when it was to be sent, or clearing turned off; skip_reason says why).
-- queued_at is when it was decided, send_after when it may go (the Undo hold's end, or at
-- once), sent_at when GitHub took it, and settled_at when it left the queue. attempts
-- counts the sends that failed, and error is the last failure in the window's words.
--
-- At most one row per account and thread is queued. The ghthreads package drops a row 30
-- days after it settled.
CREATE TABLE gh_thread_clears (
    id                INTEGER  PRIMARY KEY,
    account_login     TEXT     NOT NULL COLLATE NOCASE CHECK (account_login <> ''),
    thread_id         INTEGER  NOT NULL CHECK (thread_id > 0),
    repo_owner        TEXT     NOT NULL COLLATE NOCASE CHECK (repo_owner <> ''),
    repo_name         TEXT     NOT NULL COLLATE NOCASE CHECK (repo_name <> ''),
    item_kind         TEXT     NOT NULL CHECK (item_kind IN ('pr', 'issue')),
    number            INTEGER  NOT NULL CHECK (number > 0),
    title             TEXT     NOT NULL DEFAULT '',
    reason            TEXT     NOT NULL DEFAULT '',
    trigger           TEXT     NOT NULL CHECK (trigger <> ''),
    action            TEXT     NOT NULL CHECK (action IN ('done', 'read')),
    state             TEXT     NOT NULL CHECK (state IN ('queued', 'sent', 'kept', 'failed', 'skipped')),
    thread_updated_at DATETIME NOT NULL,
    queued_at         DATETIME NOT NULL,
    send_after        DATETIME NOT NULL,
    sent_at           DATETIME,
    settled_at        DATETIME,
    attempts          INTEGER  NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    error             TEXT     NOT NULL DEFAULT '',
    skip_reason       TEXT     NOT NULL DEFAULT '',
    CHECK ((state = 'queued') = (settled_at IS NULL)),
    CHECK ((state = 'sent') = (sent_at IS NOT NULL))
);

CREATE INDEX gh_thread_clears_due ON gh_thread_clears (state, send_after);

CREATE UNIQUE INDEX gh_thread_clears_queued ON gh_thread_clears (account_login, thread_id) WHERE state = 'queued';

CREATE INDEX gh_thread_clears_account ON gh_thread_clears (account_login, thread_id);

CREATE INDEX gh_thread_clears_settled ON gh_thread_clears (settled_at);

-- == notify ==============================================================================

-- What the app told the user, newest kept.
--
-- A refresh that finds items newly needing the user records one row per item: a pull
-- request that newly asks for the user's review (review_requested), an issue or pull
-- request newly assigned to them (assigned), or a snoozed item new activity brought back
-- early (woke, with what woke it in detail, such as "approved, 2 new commits"). Each row
-- is kept whether or not a banner was shown, so the window's notification history lists
-- every one. The item's repository, number, title, and URL are copied in, so a row still
-- reads after its item leaves the inbox; item_id links back to it while it is stored.
-- read_at is set once the user has seen the history. Only the newest rows are kept.
--
-- run_id is the script_runs id of a run that failed (run_failed), or 0 for a notification
-- about no run. It is no reference: the run history forgets its oldest runs on its own, and
-- hands an id out again once the history is cleared. run_started is when that run started,
-- as RFC 3339, or '' for none: the window opens the run only while the run with that id
-- started then, so an entry never opens a later run that took its id.
--
-- heading heads the entry and its banner in place of its kind's title, as an Autopilot
-- alert is headed by its routine's name; '' for the kind's own title.
--
-- autopilot_routine, autopilot_match, and autopilot_run are what an Autopilot notification
-- opens in the Autopilot tab: its routine, and the match (its card in Activity) or the
-- agent run (the run view) it names, each 0 for none. autopilot_created is when the most
-- specific of them was created (the run's, else the match's, else the routine's), as RFC
-- 3339, or '': the window opens it only while the one with that ID was created then, since
-- Clear history hands an ID out again.
--
-- once_key tells a notice from every other, so the log keeps it once while it holds it: an
-- Autopilot step's notice is keyed by its match, when the match was made, the step, and
-- the try, and a problem by its routine, its kind, and when it began, so a step run again
-- after a quit, or a problem told again after a crash, adds no second entry and no second
-- banner. '' for every other notification, which is never refused.
CREATE TABLE notifications (
    id                INTEGER  PRIMARY KEY,
    kind              TEXT     NOT NULL CHECK (kind <> ''),
    created_at        DATETIME NOT NULL,
    read_at           DATETIME,
    item_id           INTEGER  REFERENCES items (id) ON DELETE SET NULL,
    item_kind         TEXT     NOT NULL DEFAULT '' CHECK (item_kind IN ('', 'pr', 'issue')),
    repo_owner        TEXT     NOT NULL DEFAULT '',
    repo_name         TEXT     NOT NULL DEFAULT '',
    number            INTEGER  NOT NULL DEFAULT 0 CHECK (number >= 0),
    title             TEXT     NOT NULL DEFAULT '',
    url               TEXT     NOT NULL DEFAULT '',
    author            TEXT     NOT NULL DEFAULT '',
    detail            TEXT     NOT NULL DEFAULT '',
    run_id            INTEGER  NOT NULL DEFAULT 0 CHECK (run_id >= 0),
    run_started       TEXT     NOT NULL DEFAULT '',
    heading           TEXT     NOT NULL DEFAULT '',
    autopilot_routine INTEGER  NOT NULL DEFAULT 0 CHECK (autopilot_routine >= 0),
    autopilot_match   INTEGER  NOT NULL DEFAULT 0 CHECK (autopilot_match >= 0),
    autopilot_run     INTEGER  NOT NULL DEFAULT 0 CHECK (autopilot_run >= 0),
    autopilot_created TEXT     NOT NULL DEFAULT '',
    once_key          TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX notifications_created_at ON notifications (created_at);

CREATE UNIQUE INDEX notifications_once_key ON notifications (once_key) WHERE once_key <> '';
