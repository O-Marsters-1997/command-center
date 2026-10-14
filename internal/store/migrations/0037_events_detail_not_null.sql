-- +goose Up
SET LOCAL lock_timeout = '5s';
ALTER TABLE events ALTER COLUMN detail SET DEFAULT '';
ALTER TABLE events ADD CONSTRAINT events_detail_not_null CHECK (detail IS NOT NULL) NOT VALID;
ALTER TABLE events VALIDATE CONSTRAINT events_detail_not_null;
ALTER TABLE events ALTER COLUMN detail SET NOT NULL;
ALTER TABLE events DROP CONSTRAINT events_detail_not_null;
