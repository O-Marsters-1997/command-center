-- +goose Up
CREATE TABLE utilization_intervals (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "window"            text NOT NULL,
    start_at            timestamptz NOT NULL,
    end_at              timestamptz NOT NULL,
    utilization_start   double precision NOT NULL,
    utilization_end     double precision NOT NULL,
    weight_usd          double precision NOT NULL,
    UNIQUE ("window", start_at)
);
