-- +goose Up
-- +goose StatementBegin
SET search_path TO contract_event_processor, public;

CREATE TABLE IF NOT EXISTS blocks (
    number bigint PRIMARY KEY
        CONSTRAINT blocks_number_check CHECK (number > 0),
    hash bytea
        CONSTRAINT blocks_hash_check CHECK (length(hash) = 32),
    processed_at timestamptz default current_timestamp
);

ALTER TABLE blocks
	ADD CONSTRAINT VALIDBLOCKNUM CHECK (number > 0 AND length(hash) > 0);

CREATE UNIQUE INDEX idx_number
on blocks (number);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO contract_event_processor, public;

DROP TABLE IF EXISTS blocks;

-- +goose StatementEnd
