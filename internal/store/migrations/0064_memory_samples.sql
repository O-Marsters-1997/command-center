-- +goose Up
SET lock_timeout = '5s';
CREATE TABLE memory_samples (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sampled_at  timestamptz NOT NULL,
    total_bytes bigint NOT NULL CHECK (total_bytes > 0),
    used_bytes  bigint NOT NULL CHECK (used_bytes >= 0)
);
CREATE INDEX memory_samples_sampled_at_idx ON memory_samples (sampled_at);

CREATE TABLE memory_processes (
    sample_id bigint  NOT NULL REFERENCES memory_samples (id) ON DELETE CASCADE,
    run_id    bigint  NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    pid       integer NOT NULL,
    name      text    NOT NULL,
    args      text    NOT NULL,
    rss_bytes bigint  NOT NULL CHECK (rss_bytes >= 0),
    PRIMARY KEY (sample_id, pid)
);
CREATE INDEX memory_processes_run_id_idx ON memory_processes (run_id);

-- +goose Down
SET lock_timeout = '5s';
DROP TABLE memory_processes;
DROP TABLE memory_samples;
