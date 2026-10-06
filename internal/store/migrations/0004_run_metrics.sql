-- +goose Up
ALTER TABLE runs
    ADD COLUMN tokens_in       bigint,
    ADD COLUMN tokens_out      bigint,
    ADD COLUMN turns           bigint,
    ADD COLUMN duration_ms     bigint,
    ADD COLUMN cost_usd        double precision,
    ADD COLUMN tool_calls      bigint,
    ADD COLUMN tool_failures   bigint,
    ADD COLUMN model           text,
    ADD COLUMN metrics_settled boolean;
