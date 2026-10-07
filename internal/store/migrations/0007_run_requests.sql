-- +goose Up
CREATE TABLE run_requests (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id                bigint NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    request_id            text NOT NULL,
    thread                text NOT NULL,
    tool                  text NOT NULL DEFAULT '',
    input_tokens          bigint NOT NULL,
    cache_creation_tokens bigint NOT NULL,
    cache_read_tokens     bigint NOT NULL,
    output_tokens         bigint NOT NULL
);

CREATE INDEX run_requests_run_id_idx ON run_requests (run_id);
