-- +goose Up
ALTER TABLE runs ADD COLUMN question text;
