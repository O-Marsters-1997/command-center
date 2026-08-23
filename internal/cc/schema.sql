-- Schema version 1. Every table Phases 1-6 need is created here: there is no migration code,
-- and OpenStore refuses a version mismatch, so a later phase adding DDL would brick every
-- existing developer DB. Later phases add rows, not tables.

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    ticket_url TEXT PRIMARY KEY,
    repo       TEXT NOT NULL,
    branch     TEXT NOT NULL,
    blocked_by TEXT NOT NULL DEFAULT '[]',
    seams      TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS launches (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL,
    state      TEXT NOT NULL CHECK (state IN ('active', 'done', 'cancelled'))
);

-- prompt_hash binds consent to content (§4b): a launch authorises a prompt, not a task.
CREATE TABLE IF NOT EXISTS launch_members (
    launch_id   INTEGER NOT NULL REFERENCES launches (id),
    task_id     TEXT    NOT NULL REFERENCES tasks (ticket_url),
    prompt_hash TEXT    NOT NULL,
    PRIMARY KEY (launch_id, task_id)
);

CREATE TABLE IF NOT EXISTS runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id         TEXT NOT NULL REFERENCES tasks (ticket_url),
    kind            TEXT NOT NULL,
    pgid            INTEGER,
    proc_started_at TEXT,
    baseline_sha    TEXT,
    prompt_hash     TEXT,
    log_path        TEXT,
    outcome         TEXT,
    exit_code       INTEGER,
    ended_at        TEXT
);

CREATE TABLE IF NOT EXISTS pushes (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id          TEXT NOT NULL REFERENCES tasks (ticket_url),
    pushed_tip       TEXT NOT NULL,
    base_branch      TEXT NOT NULL,
    base_sha_at_push TEXT NOT NULL,
    pushed_at        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    at      TEXT NOT NULL,
    task_id TEXT,
    kind    TEXT NOT NULL,
    detail  TEXT
);

CREATE TABLE IF NOT EXISTS intents (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    at          TEXT NOT NULL,
    task_id     TEXT NOT NULL,
    verb        TEXT NOT NULL,
    payload     TEXT,
    consumed_at TEXT
);
