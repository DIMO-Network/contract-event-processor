-- +goose Up
-- +goose StatementBegin
SET search_path TO contract_event_processor, public;

ALTER TABLE blocks
    DROP CONSTRAINT blocks_pkey;

ALTER TABLE blocks
    ADD COLUMN chain_id bigint;

UPDATE blocks 
SET chain_id = 137
WHERE chain_id IS NULL;

ALTER TABLE blocks
    ALTER COLUMN chain_id SET NOT NULL;

ALTER TABLE blocks
    ALTER COLUMN hash SET NOT NULL;

ALTER TABLE blocks
    ALTER COLUMN number SET NOT NULL;

DELETE FROM contract_event_processor.blocks WHERE hash NOT IN
(SELECT hash FROM contract_event_processor.blocks ORDER BY number DESC LIMIT 1);

ALTER TABLE blocks
    ADD CONSTRAINT blocks_pkey PRIMARY KEY (chain_id);
    
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SET search_path TO contract_event_processor, public;

DROP TABLE IF EXISTS blocks;
-- TODO(elffjs): Write a proper down migration.
-- +goose StatementEnd
