-- +goose Up
-- +goose StatementBegin
REVOKE CREATE ON schema public FROM public; -- public schema isolation
CREATE SCHEMA IF NOT EXISTS contract_event_processor;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP SCHEMA contract_event_processor CASCADE;
GRANT CREATE, USAGE ON schema public TO public;
-- +goose StatementEnd
