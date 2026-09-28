-- +goose Up
ALTER TABLE tickets
    ADD COLUMN first_push_ci    boolean,
    ADD COLUMN hand_churn_lines bigint;
