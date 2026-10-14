-- +goose Up
SET LOCAL lock_timeout = '5s';
UPDATE events SET detail = '' WHERE detail IS NULL;
