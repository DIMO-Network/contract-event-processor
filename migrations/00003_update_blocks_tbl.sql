-- +goose Up
-- +goose StatementBegin
SET search_path TO contract_event_processor, public;

ALTER TABLE blocks
    DROP CONSTRAINT blocks_pkey;

ALTER TABLE blocks
    ADD chain_id bigint PRIMARY KEY NOT NULL;

ALTER TABLE blocks
    ALTER COLUMN hash SET NOT NULL;

ALTER TABLE blocks
    ALTER COLUMN number SET NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO contract_event_processor, public;

DROP TABLE IF EXISTS blocks;
-- +goose StatementEnd
