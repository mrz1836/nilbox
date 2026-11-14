-- 0001_init: the local schema.
--
-- Conventions:
--   - Timestamps are DATETIME columns so the driver reads them back as times. The
--     application writes them in UTC, which keeps their text form sortable.
--   - Booleans are BOOLEAN columns, kept to 0 or 1 by a CHECK, so code generated from
--     this schema reads them as bools. A NULL in a boolean that allows one means
--     "inherit".
--   - GitHub logins and repository names are case-insensitive, so those columns use
--     NOCASE collation.
--   - Credentials are never stored here; the app borrows them at run time.
--
-- The tables are grouped by the one package that owns them (tableOwners in
-- internal/repoguard/datalayer_test.go), the only package whose queries read or write
-- them, and a table comes before the tables that reference it.

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
-- assign_left_at and review_left_at are set when the user takes themselves off an item's
-- assignees, or off a pull request's requested reviewers, from the inbox: each holds the
-- item's updated_at on GitHub as GitHub answered the edit. GitHub's search index can trail
-- an edit for a while, so a refresh whose search still lists the item there, with no
-- activity since (item_updated_at not after the stamp), keeps it out of that list; once
-- the item moves past the stamp, the search is believed again and the stamp is cleared.
-- Both are NULL otherwise.
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
-- checks_awaiting counts the head commit's GitHub Actions runs held until a maintainer
-- approves them, as a fork's runs are: check suites that ended ACTION_REQUIRED with a
-- workflow run. GitHub's rollup leaves them out, so checks_state can read SUCCESS while
-- they wait.
CREATE TABLE item_details (
    item_id              INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    labels               TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(labels) AND json_type(labels) = 'array'),
    comments             INTEGER  NOT NULL DEFAULT 0 CHECK (comments >= 0),
    additions            INTEGER  NOT NULL DEFAULT 0 CHECK (additions >= 0),
    deletions            INTEGER  NOT NULL DEFAULT 0 CHECK (deletions >= 0),
    changed_files        INTEGER  NOT NULL DEFAULT 0 CHECK (changed_files >= 0),
    commits              INTEGER  NOT NULL DEFAULT 0 CHECK (commits >= 0),
    mergeable            TEXT     NOT NULL DEFAULT '',
    checks_state         TEXT     NOT NULL DEFAULT '',
    checks_passed        INTEGER  NOT NULL DEFAULT 0 CHECK (checks_passed >= 0),
    checks_failed        INTEGER  NOT NULL DEFAULT 0 CHECK (checks_failed >= 0),
    checks_pending       INTEGER  NOT NULL DEFAULT 0 CHECK (checks_pending >= 0),
    checks_skipped       INTEGER  NOT NULL DEFAULT 0 CHECK (checks_skipped >= 0),
    review_decision      TEXT     NOT NULL DEFAULT '',
    reviewers            INTEGER  NOT NULL DEFAULT 0 CHECK (reviewers >= 0),
    reviewed             INTEGER  NOT NULL DEFAULT 0 CHECK (reviewed >= 0 AND reviewed <= reviewers),
    approvals            INTEGER  NOT NULL DEFAULT 0 CHECK (approvals >= 0),
    changes_requested    INTEGER  NOT NULL DEFAULT 0 CHECK (changes_requested >= 0),
    fetched_at           DATETIME NOT NULL,
    head_oid             TEXT     NOT NULL DEFAULT '',
    projects             TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(projects) AND json_type(projects) = 'array'),
    sub_issues_total     INTEGER  NOT NULL DEFAULT 0 CHECK (sub_issues_total >= 0),
    sub_issues_completed INTEGER  NOT NULL DEFAULT 0
        CHECK (sub_issues_completed >= 0 AND sub_issues_completed <= sub_issues_total),
    review_users         TEXT     NOT NULL DEFAULT '[]'
        CHECK (json_valid(review_users) AND json_type(review_users) = 'array'),
    review_teams         TEXT     NOT NULL DEFAULT '[]'
        CHECK (json_valid(review_teams) AND json_type(review_teams) = 'array'),
    auto_merge_method    TEXT     NOT NULL DEFAULT '',
    auto_merge_at        DATETIME,
    checks_awaiting      INTEGER  NOT NULL DEFAULT 0 CHECK (checks_awaiting >= 0)
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
CREATE TABLE item_local_state (
    item_id         INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    dismissed_at    DATETIME,
    updated_at      DATETIME NOT NULL,
    snoozed_until   DATETIME CHECK (snoozed_until IS NULL OR dismissed_at IS NULL),
    snooze_baseline TEXT     CHECK (snooze_baseline IS NULL OR (json_valid(snooze_baseline) AND json_type(snooze_baseline) = 'object')),
    wake_reasons    TEXT     CHECK (wake_reasons IS NULL OR (json_valid(wake_reasons) AND json_type(wake_reasons) = 'object')),
    pinned_at       DATETIME
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

-- == prefs ===============================================================================

-- User preferences as key/value pairs.
CREATE TABLE prefs (
    key        TEXT     NOT NULL PRIMARY KEY,
    value      TEXT     NOT NULL,
    updated_at DATETIME NOT NULL
);

-- == profiles ============================================================================

-- Coding-agent launch profiles, for every agent. agent is the agent's ID ('claude' or
-- 'codex'); a name is unique per agent, so each agent has its own 'default' profile.
-- add_dirs and extra_args are JSON arrays of strings; env is a JSON object of string
-- values.
CREATE TABLE agent_profiles (
    id              INTEGER  PRIMARY KEY,
    agent           TEXT     NOT NULL CHECK (agent <> ''),
    name            TEXT     NOT NULL,
    config_dir      TEXT     NOT NULL DEFAULT '',
    model           TEXT     NOT NULL DEFAULT '',
    effort          TEXT     NOT NULL DEFAULT '',
    permission_mode TEXT     NOT NULL DEFAULT '',
    plan_mode       BOOLEAN  NOT NULL DEFAULT 0 CHECK (plan_mode IN (0, 1)),
    add_dirs        TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(add_dirs) AND json_type(add_dirs) = 'array'),
    extra_args      TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(extra_args) AND json_type(extra_args) = 'array'),
    env             TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(env) AND json_type(env) = 'object'),
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    UNIQUE (agent, name)
);

-- == prompts =============================================================================

-- The prompt library: agent prompts and review replies.
--
-- purpose is 'agent' for a prompt a launch fills in for a coding agent: a Go text/template
-- filled in for one pull request or issue. It is 'reply' for a review reply: text the
-- review sheet fills in as the body of a review, which has no launch settings.
-- reply_action names the review a reply is written for ('approve', 'request_changes', or
-- 'comment'), and is '' for an agent prompt.
--
-- name is the prompt's full title and nickname the short label the menus show; agent
-- prompts and replies share one nickname space, and nicknames are unique, ignoring case.
-- applies_to is 'pr', 'issue', or 'any'. starter_key names the starter a row was seeded
-- from ('' for a prompt the user wrote), so the app can restore the original text; the app
-- seeds each starter once, so a deleted starter stays deleted.
--
-- The rest are an agent prompt's launch settings, used whenever the prompt is chosen:
-- effort is '' to inherit, and plan_mode and worktree are NULL to inherit. models pins a
-- model per agent, a JSON object of agent to model, and agent_args extra flags per agent,
-- a JSON object of agent to a JSON array of arguments added after the profile's and the
-- launch defaults'. An agent with no entry inherits.
CREATE TABLE prompts (
    id           INTEGER  PRIMARY KEY,
    name         TEXT     NOT NULL,
    nickname     TEXT     NOT NULL COLLATE NOCASE UNIQUE,
    applies_to   TEXT     NOT NULL CHECK (applies_to IN ('pr', 'issue', 'any')),
    body         TEXT     NOT NULL,
    effort       TEXT     NOT NULL DEFAULT '',
    plan_mode    BOOLEAN  CHECK (plan_mode IN (0, 1)),
    worktree     BOOLEAN  CHECK (worktree IN (0, 1)),
    starter_key  TEXT     NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    models       TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(models) AND json_type(models) = 'object'),
    agent_args   TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(agent_args) AND json_type(agent_args) = 'object'),
    purpose      TEXT     NOT NULL DEFAULT 'agent' CHECK (purpose IN ('agent', 'reply')),
    reply_action TEXT     NOT NULL DEFAULT ''
        CHECK (reply_action IN ('', 'approve', 'request_changes', 'comment')
            AND (reply_action = '') = (purpose = 'agent'))
);

-- == launchdefaults ======================================================================

-- Launch defaults for an org or a repository.
--
-- A row with name '' holds an owner's defaults, which every repository of that owner
-- inherits; a row with a name holds one repository's, which win over its owner's. Rows are
-- keyed by name rather than by repos.id, so an org's defaults need no repository row and
-- outlive the inbox cache.
--
-- Every setting inherits when it is '', NULL, or has no entry: agent is the coding tool
-- ('' is the app's, Claude), pr_prompt_id and issue_prompt_id the default prompt for pull
-- requests and for issues, effort the agent flag, and plan_mode and worktree on or off,
-- each shared by every agent. Each agent has its own profiles, models, and flags, so
-- profiles and models are JSON objects of agent to name, and agent_args a JSON object of
-- agent to a JSON array of arguments added after the profile's (an org's first, then the
-- repository's). A prompt that is a default cannot be deleted: the foreign keys restrict
-- it.
CREATE TABLE launch_defaults (
    id              INTEGER  PRIMARY KEY,
    owner           TEXT     NOT NULL COLLATE NOCASE,
    name            TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    agent           TEXT     NOT NULL DEFAULT '',
    pr_prompt_id    INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    issue_prompt_id INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    effort          TEXT     NOT NULL DEFAULT '',
    plan_mode       BOOLEAN  CHECK (plan_mode IN (0, 1)),
    worktree        BOOLEAN  CHECK (worktree IN (0, 1)),
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    profiles        TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(profiles) AND json_type(profiles) = 'object'),
    models          TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(models) AND json_type(models) = 'object'),
    agent_args      TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(agent_args) AND json_type(agent_args) = 'object'),
    UNIQUE (owner, name)
);

CREATE INDEX launch_defaults_pr_prompt_id ON launch_defaults (pr_prompt_id);
CREATE INDEX launch_defaults_issue_prompt_id ON launch_defaults (issue_prompt_id);

-- == worklog =============================================================================

-- The worktrees the app made for pull requests and issues, one per item per clone, which
-- sessions of every agent share.
--
-- clone_path is the clone the worktree belongs to, as the launcher resolved it; worktree
-- is the item's worktree name (pr-<n> or issue-<n>), and owner, name, kind, and number the
-- item. path is the worktree's directory, and branch the branch it was made on
-- (worktree-pr-<n> or worktree-issue-<n>). A path of '' means the worktree is
-- <clone_path>/.claude/worktrees/<worktree>, where Claude Code made worktrees before the
-- app made them itself. agent is the agent of the latest session, created_at when the
-- worktree was made or first recorded, last_agent_at when the latest session started
-- (NULL before any did), and launches how many sessions started there.
--
-- The worktrees service checks every row against git each time it lists or sweeps: a row
-- whose worktree is gone is removed, and a worktree on an item's branch with no row is
-- recorded.
CREATE TABLE worktree_records (
    id            INTEGER  PRIMARY KEY,
    clone_path    TEXT     NOT NULL CHECK (clone_path <> ''),
    worktree      TEXT     NOT NULL CHECK (worktree <> ''),
    path          TEXT     NOT NULL DEFAULT '',
    branch        TEXT     NOT NULL DEFAULT '',
    owner         TEXT     NOT NULL COLLATE NOCASE CHECK (owner <> ''),
    name          TEXT     NOT NULL COLLATE NOCASE CHECK (name <> ''),
    kind          TEXT     NOT NULL CHECK (kind IN ('pr', 'issue')),
    number        INTEGER  NOT NULL CHECK (number > 0),
    agent         TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    last_agent_at DATETIME,
    launches      INTEGER  NOT NULL DEFAULT 0 CHECK (launches >= 0),
    UNIQUE (clone_path, worktree)
);

-- The worktrees the app cleaned up, newest kept.
--
-- Cleaning up removes a finished worktree, and its own branch, once its pull request merged
-- or its issue closed: by hand from the Worktrees view, or on its own when automatic
-- cleanup is on. Each one is recorded here so the view can show what was removed and when,
-- across restarts, since a toast is easy to miss. automatic tells the sweep from the user.
-- display_path is the worktree's path with ~ for home, as the view shows it; head is the
-- commit it had checked out, which was on GitHub. Only the newest rows are kept.
CREATE TABLE worktree_cleanups (
    id             INTEGER  PRIMARY KEY,
    cleaned_at     DATETIME NOT NULL,
    automatic      BOOLEAN  NOT NULL DEFAULT 0 CHECK (automatic IN (0, 1)),
    repo           TEXT     NOT NULL CHECK (repo <> ''),
    worktree       TEXT     NOT NULL CHECK (worktree <> ''),
    display_path   TEXT     NOT NULL DEFAULT '',
    deleted_branch TEXT     NOT NULL DEFAULT '',
    head           TEXT     NOT NULL DEFAULT '',
    item_kind      TEXT     NOT NULL DEFAULT '' CHECK (item_kind IN ('', 'pr', 'issue')),
    item_owner     TEXT     NOT NULL DEFAULT '',
    item_name      TEXT     NOT NULL DEFAULT '',
    item_number    INTEGER  NOT NULL DEFAULT 0 CHECK (item_number >= 0),
    item_title     TEXT     NOT NULL DEFAULT '',
    item_url       TEXT     NOT NULL DEFAULT '',
    item_state     TEXT     NOT NULL DEFAULT '' CHECK (item_state IN ('', 'open', 'merged', 'closed'))
);

CREATE INDEX worktree_cleanups_cleaned_at ON worktree_cleanups (cleaned_at);

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
CREATE TABLE notifications (
    id          INTEGER  PRIMARY KEY,
    kind        TEXT     NOT NULL CHECK (kind <> ''),
    created_at  DATETIME NOT NULL,
    read_at     DATETIME,
    item_id     INTEGER  REFERENCES items (id) ON DELETE SET NULL,
    item_kind   TEXT     NOT NULL DEFAULT '' CHECK (item_kind IN ('', 'pr', 'issue')),
    repo_owner  TEXT     NOT NULL DEFAULT '',
    repo_name   TEXT     NOT NULL DEFAULT '',
    number      INTEGER  NOT NULL DEFAULT 0 CHECK (number >= 0),
    title       TEXT     NOT NULL DEFAULT '',
    url         TEXT     NOT NULL DEFAULT '',
    author      TEXT     NOT NULL DEFAULT '',
    detail      TEXT     NOT NULL DEFAULT '',
    run_id      INTEGER  NOT NULL DEFAULT 0 CHECK (run_id >= 0),
    run_started TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX notifications_created_at ON notifications (created_at);

-- == sessions ============================================================================

-- One coding-agent session started from an item, followed from the moment its terminal
-- opens until the agent quits. id is the app's own UUID, minted before the launch;
-- agent_session_id is the agent's own: the Claude Code session, which the launch passes as
-- --session-id, or the Codex thread, found once the agent starts, and '' until then. A
-- session resumed from another names it in resumed_from and shares its agent_session_id.
-- The state is what the agent last reported (see the sessions package), and last_message
-- is the start of what it last said, kept only on this Mac. pid is the agent's process
-- once known, or 0.
--
-- permission summarizes the permissions the session started with, as the launch worded
-- them ("full access", "plan mode (outside contributor)"). worktree_action is what the
-- launch did to the item's worktree (resume, adopt, attach, or create), or '' for a
-- session in the clone. launch_notice says what the launch worked around, such as a
-- worktree started from the last fetched copy of the default branch, or '' for nothing.
CREATE TABLE agent_sessions (
    id               TEXT     PRIMARY KEY CHECK (id <> ''),
    agent_session_id TEXT     NOT NULL DEFAULT '',
    resumed_from     TEXT     NOT NULL DEFAULT '',
    agent            TEXT     NOT NULL CHECK (agent <> ''),
    profile          TEXT     NOT NULL DEFAULT '',
    config_dir       TEXT     NOT NULL DEFAULT '',
    prompt           TEXT     NOT NULL DEFAULT '',
    prompt_head      TEXT     NOT NULL DEFAULT '',
    terminal         TEXT     NOT NULL DEFAULT '',
    item_id          INTEGER  NOT NULL DEFAULT 0,
    owner            TEXT     NOT NULL COLLATE NOCASE CHECK (owner <> ''),
    name             TEXT     NOT NULL COLLATE NOCASE CHECK (name <> ''),
    kind             TEXT     NOT NULL CHECK (kind IN ('pr', 'issue')),
    number           INTEGER  NOT NULL CHECK (number > 0),
    title            TEXT     NOT NULL DEFAULT '',
    url              TEXT     NOT NULL DEFAULT '',
    dir              TEXT     NOT NULL CHECK (dir <> ''),
    clone_path       TEXT     NOT NULL DEFAULT '',
    worktree         TEXT     NOT NULL DEFAULT '',
    pid              INTEGER  NOT NULL DEFAULT 0 CHECK (pid >= 0),
    state            TEXT     NOT NULL CHECK (state <> ''),
    state_detail     TEXT     NOT NULL DEFAULT '',
    last_message     TEXT     NOT NULL DEFAULT '',
    launched_at      DATETIME NOT NULL,
    linked_at        DATETIME,
    state_at         DATETIME NOT NULL,
    ended_at         DATETIME,
    permission       TEXT     NOT NULL DEFAULT '',
    worktree_action  TEXT     NOT NULL DEFAULT '',
    launch_notice    TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX agent_sessions_item ON agent_sessions (owner, name, number, launched_at);
CREATE INDEX agent_sessions_state ON agent_sessions (state);

-- == review ==============================================================================

-- The review sheet's cache of each pull request it opened, so the sheet opens at once,
-- offline, and asks GitHub only for what changed since.
--
-- One review_diffs row per pull request the sheet read. head_oid is the head commit its
-- files were listed at; total is how many files the pull request changes, complete
-- whether every one was listed, and bytes the size of the patches kept. context is the
-- JSON of what the sheet read beyond the inbox card (the description, the merge state,
-- what the viewer may do, the reviews, and the checks), read at context_at; fetched_at
-- is when the files were listed, and used_at when the sheet last opened it. The review
-- package prunes the rows itself: ones unused for 14 days, the least recently used past
-- 100 rows or 64 MiB of patches, and a pull request's own once it is merged. Items are
-- never deleted, so the foreign key's cascade is only a backstop.
CREATE TABLE review_diffs (
    item_id    INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    head_oid   TEXT     NOT NULL DEFAULT '',
    total      INTEGER  NOT NULL DEFAULT 0 CHECK (total >= 0),
    complete   BOOLEAN  NOT NULL DEFAULT 0 CHECK (complete IN (0, 1)),
    bytes      INTEGER  NOT NULL DEFAULT 0 CHECK (bytes >= 0),
    context    TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(context) AND json_type(context) = 'object'),
    context_at DATETIME,
    fetched_at DATETIME,
    used_at    DATETIME NOT NULL
);

CREATE INDEX review_diffs_used ON review_diffs (used_at);

-- The files of each cached pull request, in GitHub's order (position). patch is the file's
-- diff as GitHub shows it, and patch_state says why there is none: 'no_content' when GitHub
-- shows no diff (a binary file, or one too large), 'cache_cap' when keeping it would pass
-- the 8 MiB the cache keeps per pull request; '' when patch holds it.
CREATE TABLE review_diff_files (
    item_id       INTEGER NOT NULL REFERENCES review_diffs (item_id) ON DELETE CASCADE,
    position      INTEGER NOT NULL CHECK (position >= 0),
    path          TEXT    NOT NULL CHECK (path <> ''),
    previous_path TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT '',
    additions     INTEGER NOT NULL DEFAULT 0 CHECK (additions >= 0),
    deletions     INTEGER NOT NULL DEFAULT 0 CHECK (deletions >= 0),
    blob_sha      TEXT    NOT NULL DEFAULT '',
    patch         TEXT    NOT NULL DEFAULT '',
    patch_state   TEXT    NOT NULL DEFAULT '' CHECK (patch_state IN ('', 'no_content', 'cache_cap')),
    PRIMARY KEY (item_id, position)
);

-- == issues ==============================================================================

-- The issue sheet's cache of each issue it opened, so the sheet opens at once, offline,
-- and asks GitHub only when the issue may have changed.
--
-- One row per issue the sheet read. view is the JSON of what the sheet read beyond the inbox
-- card (the description, the conversation's newest page, the details beside it, and what the
-- viewer may do), its long texts trimmed and marked so one issue stays within 1 MiB; bytes is
-- its length. fetched_at is when it was read, NULL once an action made it out of date; used_at
-- when the sheet last opened it; seen_at GitHub's time of the newest activity in the read the
-- sheet showed when it last opened the issue, which tells the next visit what is new. The
-- issues package prunes the rows itself: ones unused for 14 days, the least recently used past
-- 200 rows or 32 MiB, and an issue's own once it is closed. Items are never deleted, so the
-- foreign key's cascade is only a backstop.
CREATE TABLE issue_views (
    item_id    INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    view       TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(view) AND json_type(view) = 'object'),
    bytes      INTEGER  NOT NULL DEFAULT 0 CHECK (bytes >= 0),
    fetched_at DATETIME,
    used_at    DATETIME NOT NULL,
    seen_at    DATETIME
);

CREATE INDEX issue_views_used ON issue_views (used_at);

-- == avatars =============================================================================

-- The profile pictures of the people the sheets show, by login, so each is downloaded once
-- rather than on every look, and shows offline.
--
-- One row per login, lower-cased, as GitHub compares logins. image is the picture's bytes,
-- of the raster type mime names, and bytes its length; fetched_at is when it was
-- downloaded, and used_at when a sheet last showed it, moved at most once a day. A picture
-- downloaded more than seven days ago is downloaded again the next time it is shown. The
-- avatars package prunes the rows itself: ones unused for 30 days, and the least recently
-- used past 2,000 rows or 16 MiB.
CREATE TABLE avatars (
    login      TEXT     PRIMARY KEY CHECK (login <> '' AND login = lower(login)),
    mime       TEXT     NOT NULL CHECK (mime <> ''),
    image      BLOB     NOT NULL,
    bytes      INTEGER  NOT NULL CHECK (bytes >= 0),
    fetched_at DATETIME NOT NULL,
    used_at    DATETIME NOT NULL
);

CREATE INDEX avatars_used ON avatars (used_at);

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

-- == integrations ========================================================================

-- The tools the app installed or updated from their GitHub releases, one row per install
-- or update. A copy counts as installed by the app while its path and sha256 match the
-- newest row for that path. archive is the release asset it came from, and replaced the
-- version it replaced, or ''.
CREATE TABLE tool_installs (
    id           INTEGER  PRIMARY KEY,
    tool         TEXT     NOT NULL CHECK (tool <> ''),
    version      TEXT     NOT NULL CHECK (version <> ''),
    path         TEXT     NOT NULL CHECK (path <> ''),
    sha256       TEXT     NOT NULL CHECK (length(sha256) = 64),
    archive      TEXT     NOT NULL DEFAULT '',
    replaced     TEXT     NOT NULL DEFAULT '',
    installed_at DATETIME NOT NULL
);

CREATE INDEX tool_installs_path ON tool_installs (path, installed_at);

-- == scriptrun ===========================================================================

-- Every run from the Scripts tab, in the app or in the user's terminal, one row per run.
--
-- kind (release, hooks, checks, script, update, gitPull) and status (running, succeeded,
-- failed, stopped, interrupted, orphaned) are checked in Go, so later kinds need no
-- rebuild. dir is the absolute folder, shown with ~; argv is the JSON argv as started, and
-- command its display form. runs_in is app or terminal. pid and pgid are the process and
-- its group once known, or 0, and proc_started_at is the process's own start time, which
-- tells a live run from a reused pid. result is JSON per kind: a release writes its intent
-- (the expected version, the branch, and the branch the clone was on) at the start, so a
-- run cut short can still be verified. seen_at clears the tab's red dot.
CREATE TABLE script_runs (
    id              INTEGER  PRIMARY KEY,
    kind            TEXT     NOT NULL CHECK (kind <> ''),
    title           TEXT     NOT NULL DEFAULT '',
    source          TEXT     NOT NULL DEFAULT '',
    repo_owner      TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    repo_name       TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    dir             TEXT     NOT NULL CHECK (dir <> ''),
    argv            TEXT     NOT NULL DEFAULT '[]',
    command         TEXT     NOT NULL DEFAULT '',
    runs_in         TEXT     NOT NULL DEFAULT 'app',
    status          TEXT     NOT NULL CHECK (status <> ''),
    exit_code       INTEGER,
    pid             INTEGER  NOT NULL DEFAULT 0 CHECK (pid >= 0),
    pgid            INTEGER  NOT NULL DEFAULT 0 CHECK (pgid >= 0),
    proc_started_at DATETIME,
    summary         TEXT     NOT NULL DEFAULT '',
    result          TEXT     NOT NULL DEFAULT '{}',
    log_bytes       INTEGER  NOT NULL DEFAULT 0,
    seen_at         DATETIME,
    started_at      DATETIME NOT NULL,
    finished_at     DATETIME
);

CREATE INDEX script_runs_started ON script_runs (started_at);
CREATE INDEX script_runs_status ON script_runs (status);

-- == scripts =============================================================================

-- One repository command the user trusted: the sha256 of its effective form when they
-- did, and that form's display text, to show what changed. source is where the command
-- comes from (t3, tasks, or magex) and name its name there.
CREATE TABLE script_trust (
    repo_owner TEXT     NOT NULL COLLATE NOCASE CHECK (repo_owner <> ''),
    repo_name  TEXT     NOT NULL COLLATE NOCASE CHECK (repo_name <> ''),
    source     TEXT     NOT NULL CHECK (source <> ''),
    name       TEXT     NOT NULL CHECK (name <> ''),
    sha256     TEXT     NOT NULL CHECK (length(sha256) = 64),
    command    TEXT     NOT NULL DEFAULT '',
    granted_at DATETIME NOT NULL,
    PRIMARY KEY (repo_owner, repo_name, source, name)
);

-- The user's own scripts, one row each. scope is all, go, or repos (scope_repos, a JSON
-- list of owner/name); runs_in is '' for the app setting, app, or terminal. Go checks scope
-- and runs_in, so later values need no rebuild.
CREATE TABLE user_scripts (
    id          INTEGER  PRIMARY KEY,
    name        TEXT     NOT NULL CHECK (name <> ''),
    command     TEXT     NOT NULL CHECK (command <> ''),
    icon        TEXT     NOT NULL DEFAULT '',
    scope       TEXT     NOT NULL DEFAULT 'all',
    scope_repos TEXT     NOT NULL DEFAULT '[]',
    runs_in     TEXT     NOT NULL DEFAULT '',
    confirm     INTEGER  NOT NULL DEFAULT 0,
    position    INTEGER  NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);
