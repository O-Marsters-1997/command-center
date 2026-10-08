-- +goose Up
CREATE TABLE repos (
    name             text PRIMARY KEY,
    remote           text NOT NULL,
    state            text NOT NULL,
    refusal_kind     text,
    refusal          text,
    settings_source  text,
    settings_read_at timestamptz,
    tracked_at       timestamptz NOT NULL,
    CHECK (state IN ('cloning', 'ready', 'refused')),
    CHECK ((state = 'refused') = (refusal IS NOT NULL))
);
