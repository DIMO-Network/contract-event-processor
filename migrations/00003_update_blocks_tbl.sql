-- +goose Up
-- +goose StatementBegin
SET search_path TO contract_event_processor, public;

ALTER TABLE blocks
    DROP CONSTRAINT blocks_pkey;

ALTER TABLE blocks
    ADD chain_id bigint;

UPDATE blocks 
SET chain_id = -1
WHERE chain_id IS NULL;

ALTER TABLE blocks
    ALTER COLUMN chain_id SET NOT NULL;

ALTER TABLE blocks
    ALTER COLUMN hash SET NOT NULL;

ALTER TABLE blocks
    ALTER COLUMN number SET NOT NULL;

ALTER TABLE blocks
    ADD PRIMARY KEY (chain_id);
    
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO contract_event_processor, public;

DROP TABLE IF EXISTS blocks;
-- +goose StatementEnd
