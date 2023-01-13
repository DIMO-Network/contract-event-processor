-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';

SET search_path TO contract_event_processor;

CREATE TABLE IF NOT EXISTS blocks (
    hash bytea PRIMARY KEY,
    number bigint NOT NULL,
    processed_at timestamp default (now() at time zone 'utc')
);

ALTER TABLE blocks
	ADD CONSTRAINT VALIDBLOCKNUM CHECK (number > 0 AND length(hash) > 0);

CREATE UNIQUE INDEX idx_number
on blocks (number);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';

SET search_path TO contract_event_processor;

DROP TABLE IF EXISTS blocks;

-- +goose StatementEnd
