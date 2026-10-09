CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
    session_id   TEXT PRIMARY KEY,
    workspace    TEXT NOT NULL,
    system       TEXT,
    created_at   REAL NOT NULL,
    run_count    INTEGER NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'idle',
    todos        TEXT NOT NULL DEFAULT '[]',
    owner        TEXT NOT NULL DEFAULT 'anonymous',
    lease_owner  TEXT,
    lease_until  REAL NOT NULL DEFAULT 0,
    pending_steering TEXT NOT NULL DEFAULT '[]',
    workspace_bound INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
    session_id TEXT NOT NULL,
    ordinal    INTEGER NOT NULL,
    epoch      INTEGER NOT NULL DEFAULT 1,
    payload    TEXT NOT NULL,
    PRIMARY KEY (session_id, ordinal)
);
CREATE TABLE IF NOT EXISTS actions (
    action_id       TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL,
    message_id      TEXT NOT NULL,
    tool_use_id     TEXT NOT NULL,
    tool_name       TEXT NOT NULL,
    input_hash      TEXT NOT NULL,
    status          TEXT NOT NULL,
    result          TEXT,
    workflow_run_id TEXT,
    created_at      REAL NOT NULL,
    completed_at    REAL
);
CREATE TABLE IF NOT EXISTS events (
    session_id TEXT NOT NULL,
    ordinal    INTEGER NOT NULL,
    payload    TEXT NOT NULL,
    PRIMARY KEY (session_id, ordinal)
);
CREATE TABLE IF NOT EXISTS approvals (
    approval_id   TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL,
    tool_use_id   TEXT,
    tool_name     TEXT NOT NULL,
    rule          TEXT NOT NULL,
    message       TEXT NOT NULL,
    input_preview TEXT NOT NULL,
    status        TEXT NOT NULL,
    created_at    REAL NOT NULL,
    resolved_at   REAL,
    kind          TEXT NOT NULL DEFAULT 'approval',
    answer        TEXT
);
