-- +goose Up
ALTER TABLE runs
    ALTER COLUMN ticket_id DROP NOT NULL,
    ADD COLUMN launch_id bigint REFERENCES launches (id),
    ADD CONSTRAINT runs_one_owner CHECK ((ticket_id IS NULL) <> (launch_id IS NULL));
