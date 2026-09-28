-- +goose Up
CREATE TABLE utilization_readings (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at          timestamptz NOT NULL,
    "window"    text NOT NULL,
    utilization double precision NOT NULL,
    resets_at   timestamptz NOT NULL,
    UNIQUE (at, "window")
);
