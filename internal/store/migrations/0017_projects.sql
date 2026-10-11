-- +goose Up
CREATE TABLE projects (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text NOT NULL,
    hue        smallint NOT NULL,
    created_at timestamptz NOT NULL,
    CHECK (hue BETWEEN 1 AND 3),
    CHECK (name ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$')
);
CREATE UNIQUE INDEX projects_name_key ON projects (lower(name));
ALTER TABLE repos ADD COLUMN project_id bigint REFERENCES projects (id) ON DELETE SET NULL;
