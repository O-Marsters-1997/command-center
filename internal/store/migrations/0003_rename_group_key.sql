-- +goose Up
ALTER TABLE tickets RENAME COLUMN group_key TO feature;
