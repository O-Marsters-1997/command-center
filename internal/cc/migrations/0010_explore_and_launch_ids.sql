-- +goose Up
ALTER TABLE runs
    ALTER COLUMN ticket_id DROP NOT NULL,
    ADD COLUMN launch_id bigint REFERENCES launches (id) ON DELETE CASCADE,
    ADD CONSTRAINT runs_ticket_xor_launch CHECK ((ticket_id IS NULL) <> (launch_id IS NULL));
