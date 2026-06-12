-- Initial schema for sfworkflowd — the arcade-game repair workflow tracker.
--
-- Conventions:
--  * Times are INTEGER unix epoch seconds (UTC) unless noted otherwise.
--  * Two roles: 'admin' and 'user'. A user can do everything except manage
--    other users; an admin can additionally create/manage users.
--  * The repair-request lifecycle (status values + legal transitions) is
--    intentionally NOT constrained at the schema level here — the workflow
--    state machine is specified later and will live in the Go `repair`
--    package. `status` is a free TEXT column so the state machine can evolve
--    without a schema migration. The request_events table records every
--    transition for an immutable audit trail.

-- ---------- Auth ----------

CREATE TABLE users (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    username        TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash   TEXT    NOT NULL,
    role            TEXT    NOT NULL CHECK (role IN ('admin', 'user')),
    display_name    TEXT    NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    last_login_at   INTEGER
);

CREATE TABLE sessions (
    token       TEXT    PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL
);

CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

-- ---------- Generic node/key-value state ----------
--
-- Small durable key/value store for daemon bookkeeping (e.g. the
-- last-seen version string that drives the post-deploy session wipe).

CREATE TABLE node_state (
    key         TEXT    PRIMARY KEY,
    value       TEXT    NOT NULL,
    updated_at  INTEGER NOT NULL
);

-- ---------- Repair requests ----------
--
-- One row per inbound arcade-game repair request. Text/metadata only for now
-- (attachments are a planned later migration). `status` is the current
-- workflow state; its allowed values and transitions are owned by the Go
-- `repair` package, not enforced here.

CREATE TABLE repair_requests (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,

    -- What's broken.
    game_title          TEXT    NOT NULL,            -- e.g. "Galaga", "Defender"
    cabinet_ref         TEXT    NOT NULL DEFAULT '', -- serial / asset tag / location
    problem_summary     TEXT    NOT NULL,            -- one-line headline
    problem_detail      TEXT    NOT NULL DEFAULT '', -- longer description

    -- Who reported it / owns the machine.
    reporter_name       TEXT    NOT NULL DEFAULT '',
    reporter_contact    TEXT    NOT NULL DEFAULT '', -- phone or email, free-form

    -- Workflow.
    status              TEXT    NOT NULL DEFAULT 'received',
    priority            TEXT    NOT NULL DEFAULT 'normal'
                                CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    assigned_to         INTEGER REFERENCES users(id) ON DELETE SET NULL,

    created_by          INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    -- Set when the request reaches a terminal state; NULL while still open.
    closed_at           INTEGER
);

CREATE INDEX idx_repair_requests_status   ON repair_requests(status);
CREATE INDEX idx_repair_requests_assigned ON repair_requests(assigned_to);
CREATE INDEX idx_repair_requests_updated  ON repair_requests(updated_at);

-- ---------- Request events (immutable audit log) ----------
--
-- Every meaningful change to a repair request appends a row here: creation,
-- status transitions, (re)assignment, priority changes, and free-text notes.
-- Never updated or deleted in normal operation — this is the history tab.

CREATE TABLE request_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id  INTEGER NOT NULL REFERENCES repair_requests(id) ON DELETE CASCADE,
    actor_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
    kind        TEXT    NOT NULL CHECK (kind IN (
                    'created', 'status_change', 'assignment',
                    'priority_change', 'note', 'edited'
                )),
    from_value  TEXT,                       -- prior status/assignee/priority, when relevant
    to_value    TEXT,                       -- new status/assignee/priority, when relevant
    note        TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);

CREATE INDEX idx_request_events_request ON request_events(request_id, id);
