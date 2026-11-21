-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE intents DROP CONSTRAINT intents_verb_check;
ALTER TABLE intents ADD CONSTRAINT intents_verb_check
    CHECK (verb IN ('launch', 'kill', 're-run', 'retry-push', 'remove-worktree', 'cancel',
        'refresh', 'abort', 'resolve', 'follow-up', 'import', 'track', 'edit_ticket', 'raise',
        'adopt-head')) NOT VALID;
ALTER TABLE intents VALIDATE CONSTRAINT intents_verb_check;

-- +goose Down
SET lock_timeout = '5s';
DELETE FROM intents WHERE verb = 'adopt-head';
ALTER TABLE intents DROP CONSTRAINT intents_verb_check;
ALTER TABLE intents ADD CONSTRAINT intents_verb_check
    CHECK (verb IN ('launch', 'kill', 're-run', 'retry-push', 'remove-worktree', 'cancel',
        'refresh', 'abort', 'resolve', 'follow-up', 'import', 'track', 'edit_ticket', 'raise')) NOT VALID;
