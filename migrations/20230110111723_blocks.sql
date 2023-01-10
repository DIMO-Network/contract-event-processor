-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';

CREATE SCHEMA IF NOT EXISTS chain_indexer;

SET search_path TO chain_indexer, public;

CREATE TABLE IF NOT EXISTS blocks (
    number NUMERIC(78),
    "hash" TEXT,
    processed BOOLEAN
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';

SET search_path TO chain_indexer, public;
DROP SCHEMA chain_indexer CASCADE;
DROP TABLE IF EXISTS blocks;

-- +goose StatementEnd
