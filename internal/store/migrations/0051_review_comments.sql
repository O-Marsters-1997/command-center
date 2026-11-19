-- +goose Up
SET lock_timeout = '5s';
CREATE TABLE review_comments (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id  text NOT NULL REFERENCES tickets (url) ON DELETE CASCADE,
    tip        text NOT NULL,
    path       text NOT NULL,
    line       int  NOT NULL CHECK (line > 0),
    side       text NOT NULL CHECK (side IN ('old', 'new')),
    hunk       text NOT NULL,
    body       text NOT NULL CHECK (body <> ''),
    created_at timestamptz NOT NULL
);
CREATE INDEX review_comments_ticket_idx ON review_comments (ticket_id, id);

-- +goose Down
SET lock_timeout = '5s';
DROP TABLE review_comments;
