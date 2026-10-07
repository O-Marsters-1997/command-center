-- +goose Up
ALTER TABLE tickets
    DROP COLUMN first_push_ci,
    DROP COLUMN hand_churn_lines;
