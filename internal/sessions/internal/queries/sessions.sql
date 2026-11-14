-- name: InsertSession :exec
-- InsertSession records a session the app just launched.
INSERT INTO agent_sessions (
    id, agent_session_id, resumed_from, agent, profile, config_dir, prompt, prompt_head, terminal,
    item_id, owner, name, kind, number, title, url, dir, clone_path, worktree, pid,
    state, state_detail, last_message, launched_at, linked_at, state_at, ended_at, permission, worktree_action,
    launch_notice
) VALUES (
    sqlc.arg(id), sqlc.arg(agent_session_id), sqlc.arg(resumed_from), sqlc.arg(agent), sqlc.arg(profile),
    sqlc.arg(config_dir), sqlc.arg(prompt), sqlc.arg(prompt_head), sqlc.arg(terminal),
    sqlc.arg(item_id), sqlc.arg(owner), sqlc.arg(name), sqlc.arg(kind), sqlc.arg(number), sqlc.arg(title),
    sqlc.arg(url), sqlc.arg(dir), sqlc.arg(clone_path), sqlc.arg(worktree), 0,
    sqlc.arg(state), '', '', sqlc.arg(launched_at), sqlc.narg(linked_at), sqlc.arg(launched_at), NULL,
    sqlc.arg(permission), sqlc.arg(worktree_action), sqlc.arg(launch_notice)
);

-- name: GetSession :one
-- GetSession returns one session by the app's id.
SELECT id, agent_session_id, resumed_from, agent, profile, config_dir, prompt, prompt_head, terminal,
    item_id, owner, name, kind, number, title, url, dir, clone_path, worktree, pid,
    state, state_detail, last_message, launched_at, linked_at, state_at, ended_at, permission, worktree_action,
    launch_notice
FROM agent_sessions
WHERE id = sqlc.arg(id);

-- name: LiveSessions :many
-- LiveSessions returns every session that has not ended, oldest first.
SELECT id, agent_session_id, resumed_from, agent, profile, config_dir, prompt, prompt_head, terminal,
    item_id, owner, name, kind, number, title, url, dir, clone_path, worktree, pid,
    state, state_detail, last_message, launched_at, linked_at, state_at, ended_at, permission, worktree_action,
    launch_notice
FROM agent_sessions
WHERE state <> 'ended'
ORDER BY launched_at, id;

-- name: RecentSessions :many
-- RecentSessions returns every session that has not ended, and those launched since a
-- time, newest first, up to row_limit.
SELECT id, agent_session_id, resumed_from, agent, profile, config_dir, prompt, prompt_head, terminal,
    item_id, owner, name, kind, number, title, url, dir, clone_path, worktree, pid,
    state, state_detail, last_message, launched_at, linked_at, state_at, ended_at, permission, worktree_action,
    launch_notice
FROM agent_sessions
WHERE state <> 'ended' OR launched_at >= sqlc.arg(since)
ORDER BY launched_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: UpdateSession :execrows
-- UpdateSession stores what the app last observed of a session.
UPDATE agent_sessions SET
    agent_session_id = sqlc.arg(agent_session_id),
    pid = sqlc.arg(pid),
    state = sqlc.arg(state),
    state_detail = sqlc.arg(state_detail),
    last_message = sqlc.arg(last_message),
    linked_at = sqlc.narg(linked_at),
    state_at = sqlc.arg(state_at),
    ended_at = sqlc.narg(ended_at)
WHERE id = sqlc.arg(id);

-- name: PruneEndedBefore :execrows
-- PruneEndedBefore forgets the sessions that ended before a time.
DELETE FROM agent_sessions WHERE state = 'ended' AND ended_at < sqlc.arg(before);

-- name: ForgetEndedSession :execrows
-- ForgetEndedSession forgets one session, only once it has ended.
DELETE FROM agent_sessions WHERE id = sqlc.arg(id) AND state = 'ended';

-- name: PruneItemExcess :execrows
-- PruneItemExcess forgets each item's ended sessions past its newest keep sessions.
DELETE FROM agent_sessions WHERE state = 'ended' AND (
    SELECT count(*) FROM agent_sessions AS newer
    WHERE newer.owner = agent_sessions.owner AND newer.name = agent_sessions.name
        AND newer.number = agent_sessions.number
        AND (newer.launched_at > agent_sessions.launched_at
            OR (newer.launched_at = agent_sessions.launched_at AND newer.id > agent_sessions.id))
) >= CAST(sqlc.arg(keep) AS INTEGER);

-- name: EndedSince :many
-- EndedSince returns the sessions the agent named that ended since a time, the last to
-- end first, up to row_limit.
SELECT id, agent_session_id, resumed_from, agent, profile, config_dir, prompt, prompt_head, terminal,
    item_id, owner, name, kind, number, title, url, dir, clone_path, worktree, pid,
    state, state_detail, last_message, launched_at, linked_at, state_at, ended_at, permission, worktree_action,
    launch_notice
FROM agent_sessions
WHERE state = 'ended' AND agent_session_id <> '' AND ended_at >= sqlc.arg(since)
ORDER BY ended_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: CountLiveConversation :one
-- CountLiveConversation counts the sessions not ended that the agent names
-- agent_session_id: the ones carrying on that conversation.
SELECT count(*) FROM agent_sessions
WHERE state <> 'ended' AND agent_session_id = sqlc.arg(agent_session_id) AND agent_session_id <> '';
