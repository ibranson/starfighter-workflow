-- Initial schema for sfworkflowd — the arcade-game repair workflow tracker.
--
-- Conventions:
--  * Times are INTEGER unix epoch seconds (UTC) unless noted otherwise.
--  * Two roles: 'admin' and 'user'. A user can do everything except manage
--    other users; an admin can additionally create/manage users.
--  * The repair-request lifecycle (status values + legal transitions) lives in
--    the Go `repair` package, NOT here. `status` is a free TEXT column so the
--    state machine can evolve without a schema migration.
--  * This is intentionally a "current state" model, not an audit log: the row
--    itself (status, owner, timestamps) is the single source of truth. There
--    is no event-history table — its purpose is to answer "what state is this
--    in and what can I do next" for any authenticated user at any time.

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

-- ---------- Machines (accumulator registry) ----------
--
-- The set of machines we know about, keyed by name (case-insensitive unique).
-- This is an ACCUMULATOR, not a curated fleet list: reporting a fault for a
-- name that doesn't exist yet adds it here (find-or-create). The in-app
-- reporting form offers existing names as type-ahead suggestions but accepts
-- free text, so a human reuses an existing machine or coins a new one. Nobody
-- maintains it explicitly; admin curation (rename/merge/retire dupes, mark
-- deployed vs warehoused) can come later.

CREATE TABLE machines (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,  -- NULL for anonymous/harvested
    created_at  INTEGER NOT NULL
);

-- ---------- Repair requests ----------
--
-- One row per repair request, about exactly one machine. Text/metadata only
-- for now (attachments are a planned later migration). `status` is the current
-- workflow state; its allowed values and transitions are owned by the Go
-- `repair` package, not enforced here.

CREATE TABLE repair_requests (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Which machine. FK into the accumulator; RESTRICT so a machine that has
    -- repair history can't be deleted out from under its requests.
    machine_id          INTEGER NOT NULL REFERENCES machines(id) ON DELETE RESTRICT,
    problem_summary     TEXT    NOT NULL,            -- one-line headline
    problem_detail      TEXT    NOT NULL DEFAULT '', -- longer description

    -- Workflow.
    status              TEXT    NOT NULL DEFAULT 'received',
    priority            TEXT    NOT NULL DEFAULT 'normal'
                                CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    -- Owner. NULL while 'received' (unclaimed). Set when a user claims the
    -- request (received -> in_repair) and changed only by another user
    -- pulling ownership to themselves (pull-only; never delegated out).
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
CREATE INDEX idx_repair_requests_machine  ON repair_requests(machine_id);
