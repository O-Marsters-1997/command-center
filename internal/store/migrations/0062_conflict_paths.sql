-- +goose Up
SET lock_timeout = '5s';
CREATE TABLE conflict_paths (
    event_id  bigint  NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    path      text    NOT NULL,
    generated boolean NOT NULL,
    PRIMARY KEY (event_id, path)
);

-- +goose Down
SET lock_timeout = '5s';
DROP TABLE conflict_paths;
