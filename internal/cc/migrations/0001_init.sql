-- +goose Up
CREATE TABLE meta (
    key   text PRIMARY KEY,
    value text NOT NULL
);

CREATE TABLE tickets (
    url        text PRIMARY KEY,
    repo       text NOT NULL,
    branch     text NOT NULL,
    blocked_by jsonb NOT NULL DEFAULT '[]'::jsonb,
    source     text NOT NULL DEFAULT '',
    title      text NOT NULL DEFAULT '',
    body       text NOT NULL DEFAULT '',
    status     text NOT NULL DEFAULT '',
    group_key  text NOT NULL DEFAULT '',
    synced_at  text NOT NULL DEFAULT ''
);

CREATE TABLE launches (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at text NOT NULL,
    state      text NOT NULL CHECK (state IN ('active', 'done', 'cancelled'))
);

-- prompt_hash binds consent to content (§4b): a launch authorises a prompt, not a ticket.
CREATE TABLE launch_members (
    launch_id   bigint NOT NULL REFERENCES launches (id),
    ticket_id   text   NOT NULL REFERENCES tickets (url) ON DELETE CASCADE,
    prompt_hash text   NOT NULL,
    PRIMARY KEY (launch_id, ticket_id)
);

CREATE TABLE runs (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id       text NOT NULL REFERENCES tickets (url) ON DELETE CASCADE,
    kind            text NOT NULL,
    pgid            bigint,
    proc_started_at text,
    baseline_sha    text,
    prompt_hash     text,
    log_path        text,
    outcome         text,
    exit_code       bigint,
    ended_at        text
);

CREATE TABLE pushes (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id        text NOT NULL REFERENCES tickets (url) ON DELETE CASCADE,
    pushed_tip       text NOT NULL,
    base_branch      text NOT NULL,
    base_sha_at_push text NOT NULL,
    pushed_at        text NOT NULL
);

-- ticket_id is nullable: a launch event belongs to a whole launch, not to one ticket.
CREATE TABLE events (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at        text NOT NULL,
    ticket_id text REFERENCES tickets (url) ON DELETE CASCADE,
    kind      text NOT NULL,
    detail    text
);

CREATE TABLE intents (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at          text NOT NULL,
    ticket_id   text NOT NULL,
    verb        text NOT NULL,
    payload     text,
    consumed_at text
);
